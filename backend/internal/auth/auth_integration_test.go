package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"connectme/migrations"
	"connectme/pkg/database"
	"connectme/pkg/middleware"
	"connectme/pkg/utils"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// captureMailer records the latest raw tokens instead of sending email.
type captureMailer struct {
	mu            sync.Mutex
	verify, reset string
	already       int
}

func (m *captureMailer) SendVerification(_ context.Context, _, t string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verify = t
	return nil
}
func (m *captureMailer) SendPasswordReset(_ context.Context, _, t string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reset = t
	return nil
}
func (m *captureMailer) SendAlreadyRegistered(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.already++
	return nil
}

type env struct {
	h      http.Handler
	mail   *captureMailer
	db     *pgxpool.Pool
	secret []byte
}

func setup(t *testing.T) *env {
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
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { rdb.Close() })

	secret := bytes.Repeat([]byte("k"), 40)
	mail := &captureMailer{}
	svc := NewService(db, rdb, NewTokenStore(rdb, time.Hour), mail, secret, 15*time.Minute)
	ver := &VerificationHandler{DB: db, CSAM: StubCSAMScanner{}, Liveness: StubLiveness{}}
	return &env{h: Routes(svc, rdb, secret, ver), mail: mail, db: db, secret: secret}
}

func (e *env) post(path string, body any, bearer string) (int, map[string]any) {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	// Random client IP from 10.0.0.0/8 so per-IP limits (which persist in
	// Redis across runs) never interfere with the logic under test.
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1", rand.Intn(256), rand.Intn(256), rand.Intn(256))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@example.com", prefix, time.Now().UnixNano())
}

const goodPW = "Correct-Horse-9!"

func TestAuthFlow(t *testing.T) {
	e := setup(t)
	email := uniqueEmail("flow")

	// Under 18 and weak password are rejected.
	if c, _ := e.post("/register", RegisterRequest{Email: uniqueEmail("kid"), Password: goodPW, DateOfBirth: "2015-01-01"}, ""); c != 422 {
		t.Fatalf("minor: got %d want 422", c)
	}
	if c, _ := e.post("/register", RegisterRequest{Email: uniqueEmail("weak"), Password: "password", DateOfBirth: "1990-01-01"}, ""); c != 422 {
		t.Fatalf("weak pw: got %d want 422", c)
	}

	reg := RegisterRequest{Email: email, Password: goodPW, DateOfBirth: "1990-05-05"}
	if c, _ := e.post("/register", reg, ""); c != 201 {
		t.Fatalf("register: got %d", c)
	}
	// Duplicate looks identical to the caller but emails the owner.
	if c, _ := e.post("/register", reg, ""); c != 201 || e.mail.already != 1 {
		t.Fatalf("duplicate: got %d already=%d", c, e.mail.already)
	}

	// Email verification: token works once.
	tok := e.mail.verify
	if c, _ := e.post("/verify-email", VerifyEmailRequest{Token: tok}, ""); c != 204 {
		t.Fatalf("verify: got %d", c)
	}
	if c, _ := e.post("/verify-email", VerifyEmailRequest{Token: tok}, ""); c != 401 {
		t.Fatalf("verify reuse: got %d want 401", c)
	}

	// Login: wrong password and unknown email look the same.
	c1, b1 := e.post("/login", LoginRequest{Email: email, Password: "Wrong-Pass-123!"}, "")
	c2, b2 := e.post("/login", LoginRequest{Email: uniqueEmail("ghost"), Password: goodPW}, "")
	if c1 != 401 || c2 != 401 || fmt.Sprint(b1) != fmt.Sprint(b2) {
		t.Fatalf("login failures differ: %d %v / %d %v", c1, b1, c2, b2)
	}
	c, login := e.post("/login", LoginRequest{Email: email, Password: goodPW}, "")
	if c != 200 || login["email_verified"] != true {
		t.Fatalf("login: %d %v", c, login)
	}
	access, _ := login["access_token"].(string)
	refresh1, _ := login["refresh_token"].(string)
	claims, err := middleware.ParseAccessToken(e.secret, access)
	if err != nil || claims.Role != "user" {
		t.Fatalf("access token: %v %+v", err, claims)
	}
	if d := time.Until(claims.ExpiresAt.Time); d > 15*time.Minute || d < 14*time.Minute {
		t.Fatalf("access TTL %v, want ~15m", d)
	}

	// Rotation: a new pair is issued; replaying the old token revokes the family.
	c, rot := e.post("/refresh", RefreshRequest{RefreshToken: refresh1}, "")
	if c != 200 {
		t.Fatalf("refresh: %d", c)
	}
	refresh2, _ := rot["refresh_token"].(string)
	if refresh2 == refresh1 {
		t.Fatal("refresh token was not rotated")
	}
	if c, _ := e.post("/refresh", RefreshRequest{RefreshToken: refresh1}, ""); c != 401 {
		t.Fatalf("reuse: got %d want 401", c)
	}
	if c, _ := e.post("/refresh", RefreshRequest{RefreshToken: refresh2}, ""); c != 401 {
		t.Fatalf("family should be revoked after reuse: got %d want 401", c)
	}

	// Logout revokes the session.
	_, l2 := e.post("/login", LoginRequest{Email: email, Password: goodPW}, "")
	r3, _ := l2["refresh_token"].(string)
	if c, _ := e.post("/logout", RefreshRequest{RefreshToken: r3}, ""); c != 204 {
		t.Fatalf("logout: %d", c)
	}
	if c, _ := e.post("/refresh", RefreshRequest{RefreshToken: r3}, ""); c != 401 {
		t.Fatalf("refresh after logout: got %d want 401", c)
	}

	// Password reset: always 202, token works once, old sessions die.
	_, l3 := e.post("/login", LoginRequest{Email: email, Password: goodPW}, "")
	r4, _ := l3["refresh_token"].(string)
	if c, _ := e.post("/forgot-password", ForgotPasswordRequest{Email: uniqueEmail("nobody")}, ""); c != 202 {
		t.Fatalf("forgot unknown: %d", c)
	}
	if c, _ := e.post("/forgot-password", ForgotPasswordRequest{Email: email}, ""); c != 202 {
		t.Fatalf("forgot: %d", c)
	}
	newPW := "Another-Strong-Pw-7?"
	if c, _ := e.post("/reset-password", ResetPasswordRequest{Token: e.mail.reset, NewPassword: "weak"}, ""); c != 422 {
		t.Fatalf("reset weak: %d", c)
	}
	if c, _ := e.post("/reset-password", ResetPasswordRequest{Token: e.mail.reset, NewPassword: newPW}, ""); c != 204 {
		t.Fatalf("reset: %d", c)
	}
	if c, _ := e.post("/reset-password", ResetPasswordRequest{Token: e.mail.reset, NewPassword: newPW}, ""); c != 401 {
		t.Fatalf("reset reuse: %d", c)
	}
	if c, _ := e.post("/refresh", RefreshRequest{RefreshToken: r4}, ""); c != 401 {
		t.Fatalf("session survived reset: %d", c)
	}
	if c, _ := e.post("/login", LoginRequest{Email: email, Password: goodPW}, ""); c != 401 {
		t.Fatalf("old password still works: %d", c)
	}
	if c, _ := e.post("/login", LoginRequest{Email: email, Password: newPW}, ""); c != 200 {
		t.Fatalf("new password: %d", c)
	}
}

func TestLoginLockoutAndBan(t *testing.T) {
	e := setup(t)
	email := uniqueEmail("lock")
	e.post("/register", RegisterRequest{Email: email, Password: goodPW, DateOfBirth: "1990-05-05"}, "")

	for i := 0; i < maxLoginFailures; i++ {
		e.post("/login", LoginRequest{Email: email, Password: "Wrong-Pass-123!"}, "")
	}
	// Even the right password is refused while locked.
	if c, _ := e.post("/login", LoginRequest{Email: email, Password: goodPW}, ""); c != 429 {
		t.Fatalf("locked account: got %d want 429", c)
	}

	// A banned user cannot log in.
	ban := uniqueEmail("ban")
	e.post("/register", RegisterRequest{Email: ban, Password: goodPW, DateOfBirth: "1990-05-05"}, "")
	if _, err := e.db.Exec(context.Background(), `UPDATE users SET is_banned=TRUE WHERE email=$1`, ban); err != nil {
		t.Fatal(err)
	}
	if c, _ := e.post("/login", LoginRequest{Email: ban, Password: goodPW}, ""); c != 401 {
		t.Fatalf("banned: got %d want 401", c)
	}
}

func TestRegisterAgeBoundaryInDB(t *testing.T) {
	e := setup(t)
	// Exactly 18 on the conservative (UTC-12) calendar is allowed; a day short is not.
	now := utils.AgeGateReference(time.Now())
	ok := now.AddDate(-18, 0, 0).Format("2006-01-02")
	short := now.AddDate(-18, 0, 1).Format("2006-01-02")
	if c, _ := e.post("/register", RegisterRequest{Email: uniqueEmail("a18"), Password: goodPW, DateOfBirth: ok}, ""); c != 201 {
		t.Fatalf("18 today: got %d", c)
	}
	if c, _ := e.post("/register", RegisterRequest{Email: uniqueEmail("a17"), Password: goodPW, DateOfBirth: short}, ""); c != 422 {
		t.Fatalf("17y364d: got %d", c)
	}
}

func TestRBAC(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	secret := bytes.Repeat([]byte("k"), 40)
	h := middleware.Auth(secret)(RequireRole(RoleModerator)(ok))
	for role, want := range map[string]int{"user": 403, "moderator": 200, "admin": 200} {
		tok, _ := middleware.SignAccessToken(secret, "u1", role, time.Minute)
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: got %d want %d", role, rec.Code, want)
		}
	}
	if Can("moderator", PermManageHotels) || !Can("admin", PermManageHotels) || Can("user", PermBanUser) {
		t.Error("permission matrix wrong")
	}
}
