package integration

import (
	"bytes"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
)

const sampleUUID = "11111111-2222-3333-4444-555555555555"

type route struct{ Method, Path string }

// publicRoutes are the ONLY endpoints reachable without a session. Anything
// new must be added here deliberately, which makes every addition visible in review.
var publicRoutes = map[string]bool{
	"GET /healthz": true, "POST /moderation/appeals": true, "GET /readyz": true,
	"POST /auth/register": true, "POST /auth/login": true, "POST /auth/refresh": true, "POST /auth/logout": true,
	"POST /auth/forgot-password": true, "POST /auth/reset-password": true, "POST /auth/verify-email": true,
	"POST /payments/webhook":   true, // protected by the Stripe signature instead
	"POST /digest/unsubscribe": true, // one-tap unsubscribe from the email; the signed token is the proof
	"POST /safety/confirm":     true, // the trusted contact opens an emailed link; the unguessable token is the proof
	"GET /investors/public":    true, // the ownership board is public by design: names and percentages only
	"GET /videos/file/{id}":    true, // protected by a short-lived HMAC ticket instead (players cannot send headers)
}

// staffRoutes require moderator/admin permissions.
var staffRoutes = map[string]bool{
	"GET /moderation/reports": true, "GET /moderation/analytics": true, "GET /moderation/members": true, "POST /moderation/reports/{id}/resolve": true, "POST /geo/hotels": true, "POST /forums/boards": true, 
	"GET /moderation/stats": true, "GET /moderation/users/{ref}": true,
	"GET /moderation/users/{ref}/photos/{id}": true, "GET /moderation/audit": true,
	"POST /moderation/users/{ref}/ban": true, "POST /moderation/users/{ref}/unban": true,
	"POST /moderation/posts/{id}/remove": true, "POST /moderation/videos/{id}/remove": true, "POST /sponsors": true, "GET /idcheck/queue": true, "GET /idcheck/{id}/image/{which}": true, "POST /idcheck/{id}/decision": true, "GET /sponsors": true, "GET /investors/admin/pledges": true, "GET /moderation/rooms": true, "GET /moderation/appeals": true, "POST /moderation/appeals/{id}/decide": true, "POST /investors/admin/pledges/{id}/{decision}": true, "POST /sponsors/{id}/pause": true, "POST /sponsors/{id}/resume": true, "POST /moderation/users/{ref}/revoke-sessions": true, "POST /moderation/videos/{id}/approve": true, "GET /moderation/queue": true, "GET /moderation/videos/{id}/ticket": true, "POST /moderation/forum/{kind}/{id}/remove": true,
}

func routesOf(t *testing.T, h http.Handler) []route {
	t.Helper()
	rt, ok := h.(chi.Routes)
	if !ok {
		t.Fatalf("handler is %T, want a chi router", h)
	}
	var out []route
	err := chi.Walk(rt, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasSuffix(path, "/*") {
			return nil
		}
		out = append(out, route{method, path})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path+out[i].Method < out[j].Path+out[j].Method })
	return out
}

var paramRE = regexp.MustCompile(`\{[^}]+\}`)

func concrete(p string) string {
	p = paramRE.ReplaceAllString(p, sampleUUID)
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

func (e *env) fire(method, path string, body []byte, token string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1", rand.Intn(256), rand.Intn(256), rand.Intn(256))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) tokenFor(uid, role string) string {
	t, _ := middleware.SignAccessToken([]byte(secret), uid, role, time.Minute)
	return t
}

// A1 (API2 Broken Authentication / API5 Function-level authz): no route is
// reachable without a valid token unless it is explicitly public.
func TestAuditEveryNonPublicRouteRequiresAuthentication(t *testing.T) {
	e := setup(t)
	routes := routesOf(t, e.h)
	if len(routes) < 80 {
		t.Fatalf("route walk found only %d routes; the audit would be hollow", len(routes))
	}
	checked := 0
	for _, r := range routes {
		key := r.Method + " " + strings.TrimSuffix(r.Path, "/")
		if publicRoutes[r.Method+" "+r.Path] || publicRoutes[key] {
			continue
		}
		p := concrete(r.Path)
		for name, tok := range map[string]string{"no token": "", "garbage token": "garbage.token.value"} {
			rec := e.fire(r.Method, p, []byte("{}"), tok, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s with %s: got %d, want 401", r.Method, r.Path, name, rec.Code)
			}
		}
		checked++
	}
	t.Logf("verified %d protected routes (%d total, %d public)", checked, len(routes), len(publicRoutes))
	if checked < 70 {
		t.Fatalf("only %d protected routes checked", checked)
	}
}

// Public routes must not be a silent hole: new ones fail this test until listed.
func TestAuditPublicRouteAllowlistIsExact(t *testing.T) {
	e := setup(t)
	seen := map[string]bool{}
	for _, r := range routesOf(t, e.h) {
		seen[r.Method+" "+r.Path] = true
	}
	for k := range publicRoutes {
		if !seen[k] {
			t.Errorf("allowlisted public route %q does not exist (stale list)", k)
		}
	}
	// Anything reachable without a token that is NOT allowlisted is a finding.
	for _, r := range routesOf(t, e.h) {
		k := r.Method + " " + r.Path
		if publicRoutes[k] {
			continue
		}
		rec := e.fire(r.Method, concrete(r.Path), []byte("{}"), "", nil)
		if rec.Code != 401 {
			t.Errorf("undeclared public route: %s -> %d", k, rec.Code)
		}
	}
}

// API5: ordinary users never reach staff functions.
func TestAuditStaffRoutesRejectOrdinaryUsers(t *testing.T) {
	e := setup(t)
	u := e.user("Ordinary")
	tok := e.tokenFor(u, "user")
	for _, r := range routesOf(t, e.h) {
		if !isStaffRoute(r) {
			continue
		}
		rec := e.fire(r.Method, concrete(r.Path), []byte(`{"action":"ban"}`), tok, nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as user: got %d, want 403", r.Method, r.Path, rec.Code)
		}
	}
	// A moderator token cannot create hotels (admin-only).
	mod := e.newUser("moderator", true, "Mod")
	rec := e.fire("POST", "/geo/hotels", []byte(`{}`), e.tokenFor(mod, "moderator"), nil)
	if rec.Code != 403 {
		t.Errorf("moderator creating a hotel: %d", rec.Code)
	}
}

// Robustness: hostile input must never produce a 5xx or a panic anywhere.
func TestAuditHostileInputNeverCausesServerErrors(t *testing.T) {
	e := setup(t)
	u := e.user("Fuzzer")
	tok := e.tokenFor(u, "user")
	bodies := [][]byte{
		[]byte(`{`), []byte(`[]`), []byte(`null`), []byte(`"string"`), []byte(`{"__proto__":{"x":1}}`),
		[]byte(`{"a":` + strings.Repeat("[", 400) + strings.Repeat("]", 400) + `}`),
		[]byte(`{"text":"` + strings.Repeat("A", 70000) + `"}`),
		[]byte(`{"target_ref":"' OR 1=1;--","ref":"../../etc/passwd","id":"%00","message":"<script>alert(1)</script>"}`),
		bytes.Repeat([]byte{0xff, 0xfe, 0x00}, 100),
	}
	pathPayloads := []string{"' OR '1'='1", "../../../etc/passwd", "%00", "<script>", sampleUUID, "9999999999999999999999"}
	n := 0
	for _, r := range routesOf(t, e.h) {
		if publicRoutes[r.Method+" "+r.Path] && r.Path != "/auth/login" && r.Path != "/auth/register" {
			continue
		}
		for _, p := range pathPayloads {
			path := paramRE.ReplaceAllString(r.Path, strings.NewReplacer(" ", "%20", "'", "%27", "<", "%3C", ">", "%3E").Replace(p))
			path = strings.TrimSuffix(path, "/")
			if path == "" {
				path = "/"
			}
			for _, b := range bodies {
				rec := e.fire(r.Method, path, b, tok, map[string]string{"Content-Type": "application/json"})
				// A deliberate "not configured / unavailable" 503 is fine; an
				// unhandled 500 (or any other 5xx) is a finding.
				deliberate := rec.Code == http.StatusServiceUnavailable && strings.Contains(rec.Body.String(), "not configured")
				if rec.Code >= 500 && !deliberate {
					t.Fatalf("%s %s with body %.40q -> %d %s", r.Method, path, b, rec.Code, rec.Body.String())
				}
				n++
			}
			if !paramRE.MatchString(r.Path) {
				break // path payloads only matter for parameterised routes
			}
		}
	}
	t.Logf("sent %d hostile requests without a 5xx", n)
}

// API8: security headers are present on every kind of response, errors included.
func TestAuditSecurityHeadersOnEveryResponse(t *testing.T) {
	e := setup(t)
	u := e.user("Headers")
	cases := []struct {
		name, method, path, token string
	}{
		{"200", "GET", "/healthz", ""},
		{"401", "GET", "/users/me", ""},
		{"404 unknown path", "GET", "/definitely/not/here", ""},
		{"405 wrong method", "DELETE", "/healthz", ""},
		{"authed 200", "GET", "/users/me", e.tokenFor(u, "user")},
	}
	for _, c := range cases {
		rec := e.fire(c.method, c.path, nil, c.token, nil)
		h := rec.Header()
		for k, want := range map[string]string{
			"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer",
		} {
			if h.Get(k) != want {
				t.Errorf("%s: %s = %q", c.name, k, h.Get(k))
			}
		}
		for _, k := range []string{"Strict-Transport-Security", "Content-Security-Policy", "Cache-Control"} {
			if h.Get(k) == "" {
				t.Errorf("%s: missing %s", c.name, k)
			}
		}
		if s := h.Get("Server"); s != "" || h.Get("X-Powered-By") != "" {
			t.Errorf("%s: leaks server software (%q)", c.name, s)
		}
	}
}

// API8: CORS only allows configured origins, and never with a wildcard.
func TestAuditCORS(t *testing.T) {
	e := setup(t)
	e.deps.Cfg.AllowedOrigins = []string{"https://app.example.com"}
	e.h = rebuild(e)
	cases := []struct {
		origin string
		allow  bool
	}{
		{"https://app.example.com", true},
		{"https://evil.example.com", false},
		{"https://app.example.com.evil.com", false},
		{"http://app.example.com", false},
		{"null", false},
	}
	for _, c := range cases {
		rec := e.fire("GET", "/healthz", nil, "", map[string]string{"Origin": c.origin})
		got := rec.Header().Get("Access-Control-Allow-Origin")
		if c.allow && got != c.origin || !c.allow && got != "" {
			t.Errorf("origin %q: ACAO=%q (allow=%v)", c.origin, got, c.allow)
		}
		if got == "*" {
			t.Error("wildcard origin with credentials")
		}
		pre := e.fire("OPTIONS", "/users/me", nil, "", map[string]string{"Origin": c.origin, "Access-Control-Request-Method": "GET"})
		if !c.allow && pre.Code != http.StatusForbidden {
			t.Errorf("preflight from %q: %d, want 403", c.origin, pre.Code)
		}
	}
}

// API4: bodies and page sizes are bounded.
func TestAuditResourceLimits(t *testing.T) {
	e := setup(t)
	u := e.user("Limits")
	tok := e.tokenFor(u, "user")

	big := []byte(`{"bio":"` + strings.Repeat("x", 3<<20) + `"}`)
	rec := e.fire("PATCH", "/users/me", big, tok, map[string]string{"Content-Type": "application/json"})
	if rec.Code != 400 && rec.Code != 413 {
		t.Errorf("3 MiB JSON body accepted: %d", rec.Code)
	}
	if rec := e.fire("GET", "/matching/matches?limit=1000000&offset=-5", nil, tok, nil); rec.Code != 200 {
		t.Errorf("oversized page params: %d", rec.Code)
	}
	for _, q := range []string{"limit=100000", "offset=99999999", "radius_km=999999", "age_min=-1"} {
		rec := e.fire("GET", "/users/nearby?"+q, nil, tok, nil)
		if rec.Code != 422 && rec.Code != 403 {
			t.Errorf("nearby?%s: %d, want 422 (or 403 before setup)", q, rec.Code)
		}
	}
}

// API2: the login endpoint is rate limited per client IP.
func TestAuditLoginRateLimited(t *testing.T) {
	e := setup(t)
	ip := fmt.Sprintf("10.77.%d.%d:1", rand.Intn(256), rand.Intn(256))
	var last *httptest.ResponseRecorder
	for i := 0; i < 25; i++ {
		req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"email":"a@b.co","password":"x"}`))
		req.RemoteAddr = ip
		last = httptest.NewRecorder()
		e.h.ServeHTTP(last, req)
	}
	if last.Code != http.StatusTooManyRequests || last.Header().Get("Retry-After") == "" {
		t.Fatalf("21st+ login from one IP: %d (Retry-After=%q)", last.Code, last.Header().Get("Retry-After"))
	}
}

func rebuild(e *env) http.Handler { return serverNew(e) }

// TestWriteRouteInventory regenerates docs/ROUTES.md from the live router.
// Run with UPDATE_ROUTES=1. API9 (improper inventory management): the
// documented surface is generated, never hand-maintained.
func TestWriteRouteInventory(t *testing.T) {
	if os.Getenv("UPDATE_ROUTES") != "1" {
		t.Skip("set UPDATE_ROUTES=1 to regenerate docs/ROUTES.md")
	}
	e := setup(t)
	var b strings.Builder
	b.WriteString("# API routes\n\nGenerated from the running router by `UPDATE_ROUTES=1 go test ./internal/integration -run TestWriteRouteInventory`.\n")
	b.WriteString("Do not edit by hand. **Auth** is `public` only for the allowlist enforced by the audit tests; staff routes need a moderator/admin role.\n\n")
	b.WriteString("| Method | Path | Auth |\n|---|---|---|\n")
	for _, r := range routesOf(t, e.h) {
		auth := "user"
		switch {
		case publicRoutes[r.Method+" "+r.Path]:
			auth = "public"
		case staffRoutes[r.Method+" "+r.Path]:
			auth = "staff"
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", r.Method, r.Path, auth)
	}
	if err := os.WriteFile("../../../docs/ROUTES.md", []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
