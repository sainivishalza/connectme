package middleware

import (
	"errors"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var secret = []byte(strings.Repeat("s", 40))

func ok(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-User", UserID(r.Context()))
	w.WriteHeader(http.StatusOK)
}

func do(h http.Handler, hdr map[string]string, method string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuth(t *testing.T) {
	h := Auth(secret)(http.HandlerFunc(ok))
	good, _ := SignAccessToken(secret, "user-1", "user", time.Minute)
	expired, _ := SignAccessToken(secret, "user-1", "user", -time.Minute)
	wrongKey, _ := SignAccessToken([]byte(strings.Repeat("x", 40)), "user-1", "user", time.Minute)
	none := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{Type: "access"})
	noneStr, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)

	cases := []struct {
		name, header string
		want         int
	}{
		{"valid", "Bearer " + good, 200},
		{"missing", "", 401},
		{"expired", "Bearer " + expired, 401},
		{"wrong key", "Bearer " + wrongKey, 401},
		{"alg none", "Bearer " + noneStr, 401},
		{"not bearer", "Basic " + good, 401},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(h, map[string]string{"Authorization": c.header}, "GET")
			if rec.Code != c.want {
				t.Fatalf("got %d want %d", rec.Code, c.want)
			}
			if c.want == 200 && rec.Header().Get("X-User") != "user-1" {
				t.Fatal("user id not in context")
			}
		})
	}
}

func TestCORS(t *testing.T) {
	h := CORS([]string{"https://app.example.com"})(http.HandlerFunc(ok))
	rec := do(h, map[string]string{"Origin": "https://app.example.com"}, "GET")
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" ||
		rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("allowed origin not echoed")
	}
	rec = do(h, map[string]string{"Origin": "https://evil.example.com"}, "GET")
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("disallowed origin must get no CORS header")
	}
	rec = do(h, map[string]string{"Origin": "https://evil.example.com"}, "OPTIONS")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("preflight from bad origin: got %d", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := do(SecurityHeaders(http.HandlerFunc(ok)), nil, "GET")
	for _, k := range []string{"X-Content-Type-Options", "X-Frame-Options",
		"Strict-Transport-Security", "Content-Security-Policy"} {
		if rec.Header().Get(k) == "" {
			t.Errorf("missing %s", k)
		}
	}
}

func TestSanitizeText(t *testing.T) {
	if got := SanitizeText("  hi\x00 there\x07  ", 100); got != "hi there" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizeText("abcdef", 3); got != "abc" {
		t.Fatalf("got %q", got)
	}
}

// When the session-revocation lookup (Redis) is down we cannot tell whether a
// token was revoked. Plain member reads keep working so an outage is not a
// total outage; everything that changes data, and every staff token, is refused.
func TestSessionRevocationOutage(t *testing.T) {
	SetSessionCutoff(func(context.Context, string) (time.Time, error) { return time.Time{}, errors.New("redis down") })
	t.Cleanup(func() {
		SetSessionCutoff(func(context.Context, string) (time.Time, error) { return time.Time{}, nil })
	})
	h := Auth(secret)(http.HandlerFunc(ok))
	member, _ := SignAccessToken(secret, "user-1", "user", time.Minute)
	moderator, _ := SignAccessToken(secret, "mod-1", "moderator", time.Minute)
	admin, _ := SignAccessToken(secret, "adm-1", "admin", time.Minute)

	cases := []struct {
		name, token, method string
		want                int
	}{
		{"member read", member, "GET", 200},
		{"member head", member, "HEAD", 200},
		{"member post", member, "POST", 503},
		{"member put", member, "PUT", 503},
		{"member patch", member, "PATCH", 503},
		{"member delete", member, "DELETE", 503},
		{"moderator read", moderator, "GET", 503},
		{"admin read", admin, "GET", 503},
		{"admin post", admin, "POST", 503},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(h, map[string]string{"Authorization": "Bearer " + c.token}, c.method)
			if rec.Code != c.want {
				t.Fatalf("%s: got %d want %d", c.name, rec.Code, c.want)
			}
		})
	}
}

// Access tokens are for this API only: a token minted for another audience (or
// for none) is refused even when the signature, issuer and expiry are right.
func TestAccessTokenAudience(t *testing.T) {
	h := Auth(secret)(http.HandlerFunc(ok))
	mint := func(aud []string) string {
		c := Claims{Role: "user", Type: "access", RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "connectme", Subject: "user-1", Audience: aud,
			IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}}
		s, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(secret)
		return s
	}
	own, _ := SignAccessToken(secret, "user-1", "user", time.Minute)
	for name, tc := range map[string]struct {
		token string
		want  int
	}{
		"issued by SignAccessToken": {own, 200},
		"no audience":               {mint(nil), 401},
		"another audience":          {mint([]string{"someone-else"}), 401},
		"the right audience":        {mint([]string{"connectme-api"}), 200},
	} {
		t.Run(name, func(t *testing.T) {
			if rec := do(h, map[string]string{"Authorization": "Bearer " + tc.token}, "GET"); rec.Code != tc.want {
				t.Fatalf("got %d want %d", rec.Code, tc.want)
			}
		})
	}
}
