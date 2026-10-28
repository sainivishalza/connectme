package integration

import (
	"net/http"
	"testing"
)

// The static audit reads source lines, which a wrapped registration or a
// helper can hide from. This one asks the running router: every real route
// must pass through a rate limiter, which announces itself with an
// X-RateLimit-Limit header (or has already answered 429).
func TestAuditEveryLiveRouteIsRateLimitedAtRuntime(t *testing.T) {
	e := setup(t)
	// Routes with a deliberate, documented reason not to carry a per-caller limiter.
	exempt := map[string]string{
		"GET /healthz":           "liveness probe",
		"GET /readyz":            "readiness probe",
		"POST /payments/webhook": "authenticated by Stripe signature; Stripe controls the rate",
	}
	admin := e.newUser("admin", true, "RuntimeAudit")
	// A fresh token per route (admin passes every permission gate, so we reach the
	// limiter): the walk includes sign-out-everywhere and password routes, which
	// void the tokens issued before them (decision 119).

	checked := 0
	for _, r := range routesOf(t, e.h) {
		key := r.Method + " " + r.Path
		if _, ok := exempt[key]; ok {
			continue
		}
		rec := e.fire(r.Method, concrete(r.Path), []byte("{}"), e.tokenFor(admin, "admin"), map[string]string{"Content-Type": "application/json"})
		if rec.Header().Get("X-RateLimit-Limit") == "" && rec.Code != http.StatusTooManyRequests {
			t.Errorf("%s answered %d without passing a rate limiter", key, rec.Code)
		}
		checked++
	}
	if checked < 80 {
		t.Fatalf("only %d routes checked; the walk is hollow", checked)
	}
	t.Logf("%d routes verified to pass a limiter (%d exempt)", checked, len(exempt))
}

func TestRateLimitHeaderAdvertisesTheLimit(t *testing.T) {
	e := setup(t)
	rec := e.fire("POST", "/auth/login", []byte(`{"email":"a@b.co","password":"x"}`), "", nil)
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "20" {
		t.Errorf("login advertises limit %q, want 20", got)
	}
}
