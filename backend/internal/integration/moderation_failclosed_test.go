package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectme/internal/server"
	"connectme/pkg/middleware"
)

// do is call() that hands back the whole response, for header checks.
func (e *env) do(method, path string, body any, uid string) *httptest.ResponseRecorder {
	j, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(j))
	tok, _ := middleware.SignAccessToken([]byte(secret), uid, "user", time.Minute)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1", rand.Intn(256), rand.Intn(256), rand.Intn(256))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

// When screening cannot run, public text and photos are refused with 503
// moderation_unavailable. Nothing is allowed through unchecked.

func (e *env) wantUnavailable(what string, c int, b []byte) {
	e.t.Helper()
	if c != 503 || !strings.Contains(string(b), "moderation_unavailable") {
		e.t.Fatalf("%s: want 503 moderation_unavailable, got %d %s", what, c, b)
	}
}

func (e *env) breakAI(st *stubAI, failing bool) {
	st.mu.Lock()
	st.failing = failing
	st.mu.Unlock()
}

func TestModerationRefusesWhenTheAIServiceErrors(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	u := e.user("Fail Closed")
	e.breakAI(st, true)

	c, b := e.call("PATCH", "/users/me", map[string]string{"bio": "A perfectly normal bio"}, u)
	e.wantUnavailable("bio while the AI service errors", c, b)
	c, b = e.call("PATCH", "/users/me", map[string]string{"display_name": "Fresh Name"}, u)
	e.wantUnavailable("display name while the AI service errors", c, b)
	if c := uploadPhoto(e, u, jpegBytesFor(t)); c != 503 {
		t.Fatalf("photo while the AI service errors: want 503, got %d", c)
	}
	if n := e.count(`SELECT jsonb_array_length(photos) FROM profiles WHERE user_id=$1`, u); n != 0 {
		t.Fatalf("an unscreened photo was stored: %d", n)
	}
}

func TestModerationRefusesWhenNoCheckerIsConfigured(t *testing.T) {
	e := setup(t)
	cfg := e.deps.Cfg
	cfg.AIServiceURL, cfg.AIServiceKey = "", ""
	cfg.ModerationDevBypass = false
	e.deps.Cfg = cfg
	e.h = server.New(e.deps).Handler
	u := e.user("No Checker")

	c, b := e.call("PATCH", "/users/me", map[string]string{"bio": "Nobody is checking this"}, u)
	e.wantUnavailable("bio without a checker", c, b)
	if c := uploadPhoto(e, u, jpegBytesFor(t)); c != 503 {
		t.Fatalf("photo without a checker: want 503, got %d", c)
	}
}

func TestModerationRefusesWhileTheBreakerIsOpenAndStopsCalling(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	u := e.user("Breaker")
	e.breakAI(st, true)

	for i := 0; i < 12; i++ {
		c, b := e.call("PATCH", "/users/me", map[string]string{"bio": "BADWORD while the service is down"}, u)
		e.wantUnavailable("bio during an outage", c, b)
	}
	// After five failures the breaker opens and the remaining calls never leave the server.
	if n := st.count("/moderate"); n > 6 {
		t.Fatalf("breaker did not open: %d calls", n)
	}
}

func TestSwipeCommentsAreRefusedButPlainSwipesKeepWorking(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	lat, lng := randomBase()
	a, b := e.located("Swiper", lat, lng), e.located("Target", lat+0.002, lng)
	e.breakAI(st, true)

	c, body := e.call("POST", "/matching/swipe", map[string]string{"target_ref": e.refOf(b), "action": "like", "comment": "Your photo made me smile"}, a)
	e.wantUnavailable("swipe with a comment", c, body)
	if c, body := e.call("POST", "/matching/swipe", map[string]string{"target_ref": e.refOf(b), "action": "like"}, a); c != 200 {
		t.Fatalf("a plain swipe must still work: %d %s", c, body)
	}
}

func TestResponseCarriesRetryAfter(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	u := e.user("Retry")
	e.breakAI(st, true)
	rr := e.do("PATCH", "/users/me", map[string]string{"bio": "hello"}, u)
	if rr.Code != 503 || rr.Header().Get("Retry-After") != "5" {
		t.Fatalf("want 503 with Retry-After: 5, got %d %q", rr.Code, rr.Header().Get("Retry-After"))
	}
}

func TestModerationTimesOutAndRefusesWithAConfigurableLimit(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	cfg := e.deps.Cfg
	cfg.AITextTimeoutMS = 100
	e.deps.Cfg = cfg
	e.h = server.New(e.deps).Handler
	u := e.user("Slow")
	st.mu.Lock()
	st.delay = 400 * time.Millisecond
	st.mu.Unlock()

	c, b := e.call("PATCH", "/users/me", map[string]string{"bio": "a bio that takes too long to check"}, u)
	e.wantUnavailable("bio when the AI service is slower than the configured timeout", c, b)

	st.mu.Lock()
	st.delay = 0
	st.mu.Unlock()
	if c, b := e.call("PATCH", "/users/me", map[string]string{"bio": "now it is fast"}, u); c != 200 {
		t.Fatalf("a fast answer within the limit must succeed: %d %s", c, b)
	}
}

// With only the unimplemented provider configured every photo is refused, and
// nothing is stored: the real wiring, not just the unit.
func TestUnimplementedCSAMProviderRefusesEveryPhoto(t *testing.T) {
	e := setup(t)
	e.withAI()
	cfg := e.deps.Cfg
	cfg.CSAMProvider = "photodna"
	e.deps.Cfg = cfg
	e.h = server.New(e.deps).Handler
	u := e.user("Unscanned")

	if c := uploadPhoto(e, u, jpegBytesFor(t)); c != 503 {
		t.Fatalf("photo with an unimplemented CSAM provider: want 503, got %d", c)
	}
	if n := e.count(`SELECT jsonb_array_length(photos) FROM profiles WHERE user_id=$1`, u); n != 0 {
		t.Fatalf("an unscanned photo was stored: %d", n)
	}
}

// An "allow" that the AI service says no model produced is not an allow.
func TestAnImageNoModelLookedAtIsRefused(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	u := e.user("Unmodelled")
	st.mu.Lock()
	st.unscreened = true
	st.mu.Unlock()
	if c := uploadPhoto(e, u, jpegBytesFor(t)); c != 503 {
		t.Fatalf("photo the AI service could not model-screen: want 503, got %d", c)
	}
	if n := e.count(`SELECT jsonb_array_length(photos) FROM profiles WHERE user_id=$1`, u); n != 0 {
		t.Fatalf("an unscreened photo was stored: %d", n)
	}
}
