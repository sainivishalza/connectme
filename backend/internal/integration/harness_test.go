// Package integration exercises the assembled HTTP API (real router, real
// Postgres/PostGIS, real Redis). Tests skip unless TEST_POSTGRES_URL and
// TEST_REDIS_URL are set.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectme/internal/chat"
	"connectme/internal/levels"
	"connectme/internal/push"
	"connectme/internal/server"
	"connectme/internal/users"
	"connectme/migrations"
	"connectme/pkg/config"
	"connectme/pkg/database"
	"connectme/pkg/middleware"

	"github.com/jackc/pgx/v5/pgxpool"
)

const secret = "integration-test-secret-integration-test"

type env struct {
	t    *testing.T
	db   *pgxpool.Pool
	rdb  *database.Redis
	deps server.Deps
	h    http.Handler
	chat *chat.Service // nil unless TEST_MONGO_URL is set
	push *push.Stub    // what the server would have sent to phones
}

func setup(t *testing.T) *env {
	t.Helper()
	pgURL, redisURL := os.Getenv("TEST_POSTGRES_URL"), os.Getenv("TEST_REDIS_URL")
	if pgURL == "" || redisURL == "" {
		t.Skip("TEST_POSTGRES_URL / TEST_REDIS_URL not set")
	}
	levels.CacheTTL = 0 // tests read rankings right after changing them
	ctx := context.Background()
	db, err := database.NewPostgres(ctx, pgURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := database.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	rdb, err := database.NewRedis(ctx, redisURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rdb.Close() })

	// A stand-in video scanner: the clip's bytes decide the verdict.
	scanner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch {
		case bytes.Contains(b, []byte("BROKEN")):
			http.Error(w, "boom", http.StatusInternalServerError)
		case bytes.Contains(b, []byte("BLOCKME")):
			_, _ = w.Write([]byte(`{"action":"block","reason":"known abuse hash"}`))
		case bytes.Contains(b, []byte("FLAGME")):
			_, _ = w.Write([]byte(`{"action":"flag","reason":"possible nudity"}`))
		default:
			_, _ = w.Write([]byte(`{"action":"allow"}`))
		}
	}))
	t.Cleanup(scanner.Close)
	cfg := config.Config{Env: "development", JWTSecret: secret, JWTAccessExpiry: 15 * time.Minute,
		JWTRefreshExpiry: time.Hour, MediaDir: t.TempDir(), EnableVideo: true, EnableInvestors: true, InvestorOwnerName: "Vishal Saini", InvestorPaymentNote: "Bank transfer details are sent by the owner.", VideoScanner: "http", VideoScannerURL: scanner.URL, VideoScannerKey: "test-key",
		LiveKitURL: "ws://livekit.test:7880", LiveKitAPIKey: "devkey", LiveKitAPISecret: "livekit-test-secret-at-least-32-bytes!!"}
	if u, sec := os.Getenv("TEST_LIVEKIT_URL"), os.Getenv("TEST_LIVEKIT_SECRET"); u != "" && sec != "" {
		cfg.LiveKitURL, cfg.LiveKitAPISecret = u, sec // a real server, for livekit_real_test.go
	}
	if ep := os.Getenv("TEST_S3_ENDPOINT"); ep != "" {
		// Run the whole suite (photos, chat media, erasure, retention) on an
		// S3-compatible store instead of local disk.
		cfg.S3Endpoint, cfg.S3Bucket = ep, "connectme-it"
		cfg.S3AccessKey, cfg.S3SecretKey = os.Getenv("TEST_S3_ACCESS"), os.Getenv("TEST_S3_SECRET")
		ensureBucket(t, cfg)
	}
	stub := &push.Stub{}
	deps := server.Deps{Cfg: cfg, PG: db, Redis: rdb, Push: stub}
	if mongoURL := os.Getenv("TEST_MONGO_URL"); mongoURL != "" {
		m, err := database.NewMongo(ctx, mongoURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { m.Close(context.Background()) })
		deps.Mongo = m
	}
	return &env{t: t, db: db, rdb: rdb, deps: deps, h: server.New(deps).Handler, chat: server.NewChat(deps), push: stub}
}

// randomBase gives each test its own patch of the planet so leftover rows
// from earlier runs can never fall inside a later run's search radius.
func randomBase() (lat, lng float64) {
	return -55 + rand.Float64()*100, -170 + rand.Float64()*340
}

func (e *env) newUser(role string, verified bool, name string) string {
	e.t.Helper()
	ctx := context.Background()
	var id string
	err := e.db.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, date_of_birth, is_verified, role)
		VALUES ($1,'x','1994-03-02',$2,$3) RETURNING id::text`,
		fmt.Sprintf("u%d-%d@example.com", time.Now().UnixNano(), rand.Intn(1e6)), verified, role).Scan(&id)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, `INSERT INTO profiles (user_id, display_name, intro, gender) VALUES ($1,$2,'hi','woman')`,
		id, name); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) user(name string) string { return e.newUser("user", true, name) }

func (e *env) locate(uid string, lat, lng float64) {
	e.t.Helper()
	if c, b := e.call("PUT", "/users/me/location", map[string]float64{"lat": lat, "lng": lng}, uid); c != 204 {
		e.t.Fatalf("locate: %d %s", c, b)
	}
}

func (e *env) call(method, path string, body any, uid string) (int, []byte) {
	return e.callAs(method, path, body, uid, "user")
}

func (e *env) callAs(method, path string, body any, uid, role string) (int, []byte) {
	var rd *bytes.Reader
	if body == nil {
		rd = bytes.NewReader(nil)
	} else {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req := httptest.NewRequest(method, path, rd)
	tok, _ := middleware.SignAccessToken([]byte(secret), uid, role, time.Minute)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1", rand.Intn(256), rand.Intn(256), rand.Intn(256))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func (e *env) refOf(uid string) string {
	e.t.Helper()
	var ref string
	if err := e.db.QueryRow(context.Background(),
		`SELECT public_ref::text FROM profiles WHERE user_id=$1`, uid).Scan(&ref); err != nil {
		e.t.Fatal(err)
	}
	return ref
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.db.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func decodeInto(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
}

// raw sends a request with an arbitrary body and returns status and bytes.
func (e *env) raw(method, path string, body []byte, uid string) (int, []byte) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	tok, _ := middleware.SignAccessToken([]byte(secret), uid, "user", time.Minute)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1", rand.Intn(256), rand.Intn(256), rand.Intn(256))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// matched creates two verified users who have matched, returning ids and the match id.
func (e *env) matched() (a, b, matchID string) {
	e.t.Helper()
	a, b = e.user("ChatA"), e.user("ChatB")
	e.swipe(a, e.refOf(b), "like")
	_, r := e.swipe(b, e.refOf(a), "like")
	if !r.Matched {
		e.t.Fatal("setup: expected match")
	}
	return a, b, r.MatchID
}

func ensureBucket(t *testing.T, cfg config.Config) {
	t.Helper()
	st, err := users.NewS3Store(users.S3Config{Endpoint: cfg.S3Endpoint, Bucket: cfg.S3Bucket,
		AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureBucket(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// photoStored reports whether a photo's bytes exist in the configured media
// store (local disk, or the S3 bucket when TEST_S3_ENDPOINT is set).
func (e *env) photoStored(id string) bool {
	e.t.Helper()
	c := e.deps.Cfg
	if c.S3Endpoint == "" {
		_, err := os.Stat(filepath.Join(c.MediaDir, id+".jpg"))
		return err == nil
	}
	st, err := users.NewS3Store(users.S3Config{Endpoint: c.S3Endpoint, Bucket: c.S3Bucket,
		AccessKey: c.S3AccessKey, SecretKey: c.S3SecretKey, Prefix: "photos"})
	if err != nil {
		e.t.Fatal(err)
	}
	_, err = st.Get(context.Background(), id)
	return err == nil
}
