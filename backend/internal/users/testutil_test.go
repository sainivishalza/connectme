package users

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"connectme/internal/csam"
	"connectme/migrations"
	"connectme/pkg/database"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// randomBase picks a random point so test data left in the shared database by
// earlier runs can never fall inside a later run's search radius.
func randomBase() (lat, lng float64) {
	return -55 + rand.Float64()*100, -170 + rand.Float64()*340
}

var testSecret = bytes.Repeat([]byte("k"), 40)

type rejectAll struct{}

func (rejectAll) Scan(context.Context, []byte, csam.ScanMeta) (csam.Verdict, error) {
	return csam.Verdict{Match: true, Reference: "test-hit"}, nil
}

type allowAll struct{}

func (allowAll) Scan(context.Context, []byte, csam.ScanMeta) (csam.Verdict, error) {
	return csam.Verdict{}, nil
}

type env struct {
	t    *testing.T
	db   *pgxpool.Pool
	root http.Handler
	svc  *Service
}

func setup(t *testing.T, scan MediaScanner) *env {
	t.Helper()
	pgURL, redisURL := os.Getenv("TEST_POSTGRES_URL"), os.Getenv("TEST_REDIS_URL")
	if pgURL == "" || redisURL == "" {
		t.Skip("TEST_POSTGRES_URL / TEST_REDIS_URL not set")
	}
	ctx := context.Background()
	db, err := database.NewPostgres(ctx, pgURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := database.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	opt, _ := redis.ParseURL(redisURL)
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { rdb.Close() })

	svc := NewService(db, LocalStore{Dir: t.TempDir()}, scan)
	svc.SetDevBypass(true) // these tests are about users, not about the AI service
	r := chi.NewRouter()
	r.Mount("/users", Routes(svc, rdb, testSecret))
	r.Mount("/media", MediaRoutes(svc, rdb, testSecret))
	return &env{t: t, db: db, root: r, svc: svc}
}

func (e *env) user(verified bool, name, bio string, tags ...string) string {
	e.t.Helper()
	ctx := context.Background()
	var id string
	err := e.db.QueryRow(ctx, `INSERT INTO users (email, password_hash, date_of_birth, is_verified)
		VALUES ($1,'x','1995-06-15',$2) RETURNING id::text`,
		fmt.Sprintf("u%d@example.com", time.Now().UnixNano()), verified).Scan(&id)
	if err != nil {
		e.t.Fatal(err)
	}
	if tags == nil {
		tags = []string{}
	}
	if _, err := e.db.Exec(ctx, `INSERT INTO profiles (user_id, display_name, bio, intro, tags, gender)
		VALUES ($1,$2,$3,'hello there',$4,'woman')`, id, name, bio, tags); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) locate(id string, lat, lng float64) {
	e.t.Helper()
	if err := e.svc.SetLocation(context.Background(), id, lat, lng); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) do(method, path string, body any, uid string) (int, []byte, http.Header) {
	var rd *bytes.Reader
	if b, ok := body.([]byte); ok {
		rd = bytes.NewReader(b)
	} else {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req := httptest.NewRequest(method, path, rd)
	tok, _ := middleware.SignAccessToken(testSecret, uid, "user", time.Minute)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.RemoteAddr = fmt.Sprintf("10.9.%d.%d:1", time.Now().UnixNano()%250, time.Now().UnixNano()/7%250)
	rec := httptest.NewRecorder()
	e.root.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes(), rec.Header()
}

func (e *env) upload(uid string, data []byte) (int, []byte) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("photo", "p.jpg")
	fw.Write(data)
	mw.Close()
	req := httptest.NewRequest("POST", "/users/me/photos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	tok, _ := middleware.SignAccessToken(testSecret, uid, "user", time.Minute)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.RemoteAddr = fmt.Sprintf("10.8.%d.%d:1", time.Now().UnixNano()%250, time.Now().UnixNano()/7%250)
	rec := httptest.NewRecorder()
	e.root.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func jpegBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y += 8 {
		for x := 0; x < w; x += 8 {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 99, 255})
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

// withEXIF inserts an APP1/EXIF segment carrying a marker string right after SOI.
func withEXIF(jpg []byte, marker string) []byte {
	payload := append([]byte("Exif\x00\x00"), marker...)
	n := len(payload) + 2
	seg := append([]byte{0xFF, 0xE1, byte(n >> 8), byte(n)}, payload...)
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func refOf(t *testing.T, body []byte) string {
	t.Helper()
	var p struct{ Ref string }
	if err := json.Unmarshal(body, &p); err != nil || p.Ref == "" {
		t.Fatalf("no ref in %s", body)
	}
	return p.Ref
}
