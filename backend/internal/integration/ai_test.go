package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"connectme/internal/server"
)

const aiKey = "ai-test-key"

// stubAI is a fake AI service whose behaviour tests can steer.
type stubAI struct {
	mu       sync.Mutex
	calls    map[string]int
	texts    []map[string]string
	scoreIn  []map[string]any
	badKey   int
	failing  bool
	imageAct string // allow | review | block
	reasons  []map[string]any
	replyIn  []string // bodies sent to /wingman/reply
	srv      *httptest.Server
}

func (s *stubAI) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[path]
}

func (e *env) withAI() *stubAI {
	e.t.Helper()
	st := &stubAI{calls: map[string]int{}, imageAct: "allow", reasons: []map[string]any{
		{"code": "shared_interests", "text": "You both like hiking", "weight": 0.4},
		{"code": "similar_age", "text": "Only 2 years apart", "weight": 0.2},
	}}
	st.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		st.calls[r.URL.Path]++
		if r.Header.Get("X-Internal-Key") != aiKey {
			st.badKey++
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if st.failing {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/moderate":
			var in map[string]string
			_ = json.Unmarshal(body, &in)
			st.texts = append(st.texts, in)
			act := "allow"
			switch {
			case strings.Contains(in["text"], "BADWORD"):
				act = "block"
			case strings.Contains(in["text"], "REVIEWME"):
				act = "review"
			}
			json.NewEncoder(w).Encode(map[string]any{"safe": act == "allow", "action": act, "reason": "This content breaks our community rules.", "categories": []string{"test"}})
		case "/moderate/image":
			json.NewEncoder(w).Encode(map[string]any{"safe": st.imageAct == "allow", "action": st.imageAct, "reason": "This image is not allowed.", "categories": []string{"test"}})
		case "/wingman/reply":
			st.replyIn = append(st.replyIn, string(body))
			json.NewEncoder(w).Encode(map[string]any{"replies": []string{"Sounds great, count me in!", "Which night works for everyone?", "I will bring the snacks, promise."}, "source": "llm"})
		case "/score":
			var in map[string]any
			_ = json.Unmarshal(body, &in)
			st.scoreIn = append(st.scoreIn, in)
			json.NewEncoder(w).Encode(map[string]any{"score": 87, "reasons": st.reasons, "components": map[string]float64{}})
		default:
			http.NotFound(w, r)
		}
	}))
	e.t.Cleanup(st.srv.Close)
	cfg := e.deps.Cfg
	cfg.AIServiceURL, cfg.AIServiceKey = st.srv.URL, aiKey
	e.deps.Cfg = cfg
	e.h = server.New(e.deps).Handler
	return st
}

func TestTextModerationPolicy(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	u := e.user("Mod User")
	mod := e.newUser("moderator", true, "Mod")

	// block: refused with the generic message, nothing saved.
	c, b := e.call("PATCH", "/users/me", map[string]string{"bio": "this has a BADWORD in it"}, u)
	if c != 422 || !strings.Contains(string(b), "community rules") {
		t.Fatalf("blocked bio: %d %s", c, b)
	}
	_, me := e.call("GET", "/users/me", nil, u)
	if strings.Contains(string(me), "BADWORD") {
		t.Fatalf("blocked text was stored: %s", me)
	}

	// review: saved, and queued for staff with no reporter.
	if c, b := e.call("PATCH", "/users/me", map[string]string{"bio": "REVIEWME please"}, u); c != 200 {
		t.Fatalf("review bio must be accepted: %d %s", c, b)
	}
	if n := e.count(`SELECT COUNT(*) FROM reports WHERE reported_id=$1 AND reporter_id IS NULL AND reason='auto_review'`, u); n != 1 {
		t.Fatalf("flag not queued: %d", n)
	}
	_, q := e.callAs("GET", "/moderation/reports", nil, mod, "moderator")
	if !strings.Contains(string(q), "REVIEWME please") || !strings.Contains(string(q), "bio") {
		t.Fatalf("moderators must see the flagged text and where it was: %s", q)
	}

	// each surface reports its own kind so rules can differ.
	e.call("PATCH", "/users/me", map[string]string{"display_name": "Sam", "intro": "hi there", "mood": "chill"}, u)
	kinds := map[string]bool{}
	st.mu.Lock()
	for _, in := range st.texts {
		kinds[in["kind"]] = true
	}
	st.mu.Unlock()
	for _, k := range []string{"bio", "name", "intro", "mood"} {
		if !kinds[k] {
			t.Errorf("no moderation call for kind %q", k)
		}
	}

	// unrelated, harmless edits do not call the service.
	before := st.count("/moderate")
	e.call("PATCH", "/users/me", map[string]any{"tags": []string{"hiking"}}, u)
	if st.count("/moderate") != before {
		t.Fatal("tags-only update must not trigger text moderation")
	}
}

func TestBoardGroupAndPhotoModeration(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	lat, lng := randomBase()
	hotelID := e.hotel(lat, lng)
	u := e.user("Poster")
	e.locate(u, lat, lng)
	e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u)

	if c, _ := e.call("POST", "/geo/hotels/"+hotelID+"/board", map[string]string{"message": "BADWORD dinner?"}, u); c != 422 {
		t.Fatalf("blocked board post: %d", c)
	}
	if c, _ := e.call("POST", "/geo/hotels/"+hotelID+"/board", map[string]string{"message": "Anyone want dinner?"}, u); c != 201 {
		t.Fatalf("clean board post: %d", c)
	}

	g := e.located("Grouper", lat, lng)
	if c, _ := e.call("POST", "/groups", map[string]any{"name": "BADWORD crew", "type": "double", "activity": "dinner"}, g); c != 422 {
		t.Fatalf("blocked group name: %d", c)
	}
	if c, _ := e.call("POST", "/groups", map[string]any{"name": "Nice crew", "type": "double", "activity": "dinner", "description": "BADWORD"}, g); c != 422 {
		t.Fatalf("blocked group description: %d", c)
	}
	if c, _ := e.call("POST", "/groups", map[string]any{"name": "Nice crew", "type": "double", "activity": "dinner"}, g); c != 201 {
		t.Fatalf("clean group: %d", c)
	}

	// images: block refuses and stores nothing; review stores and queues.
	p := e.user("Photographer")
	st.mu.Lock()
	st.imageAct = "block"
	st.mu.Unlock()
	if c := uploadPhoto(e, p, jpegBytesFor(t)); c != 422 {
		t.Fatalf("blocked photo: %d", c)
	}
	if n := e.count(`SELECT jsonb_array_length(photos) FROM profiles WHERE user_id=$1`, p); n != 0 {
		t.Fatalf("blocked photo was stored: %d", n)
	}
	st.mu.Lock()
	st.imageAct = "review"
	st.mu.Unlock()
	if c := uploadPhoto(e, p, jpegBytesFor(t)); c != 201 {
		t.Fatalf("review photo must be accepted: %d", c)
	}
	if n := e.count(`SELECT COUNT(*) FROM reports WHERE reported_id=$1 AND reason='auto_review' AND details LIKE 'photo:%'`, p); n != 1 {
		t.Fatalf("photo flag not queued: %d", n)
	}
}

func TestAIOutageFailsOpenAndBreakerStopsCalls(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	u := e.user("Resilient")
	st.mu.Lock()
	st.failing = true
	st.mu.Unlock()

	for i := 0; i < 12; i++ {
		if c, b := e.call("PATCH", "/users/me", map[string]string{"bio": "BADWORD but the service is down"}, u); c != 200 {
			t.Fatalf("outage must not block users (attempt %d): %d %s", i, c, b)
		}
	}
	// The breaker opened after 5 failures: the remaining calls never left the server.
	if n := st.count("/moderate"); n > 6 {
		t.Fatalf("breaker did not open: %d calls", n)
	}
}

func TestWrongKeyIsTreatedAsOutageNotAsVerdict(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	cfg := e.deps.Cfg
	cfg.AIServiceKey = "wrong"
	e.deps.Cfg = cfg
	e.h = server.New(e.deps).Handler
	u := e.user("Keyed")
	if c, _ := e.call("PATCH", "/users/me", map[string]string{"bio": "hello"}, u); c != 200 {
		t.Fatalf("misconfigured key must fail open: %d", c)
	}
	if st.badKey == 0 {
		t.Fatal("service should have been called (and rejected the key)")
	}
}

func TestMatchGetsAScoreWithoutLeakingAgeInIncognito(t *testing.T) {
	e := setup(t)
	st := e.withAI()

	// Open match: all reasons kept.
	a, b := e.user("Scorer A"), e.user("Scorer B")
	e.call("PATCH", "/users/me", map[string]any{"tags": []string{"hiking", "coffee"}}, a)
	e.call("PATCH", "/users/me", map[string]any{"tags": []string{"hiking", "jazz"}}, b)
	e.swipe(a, e.refOf(b), "like")
	_, r := e.swipe(b, e.refOf(a), "like")
	if !r.Matched {
		t.Fatal("expected a match")
	}
	waitFor(t, func() bool {
		return e.count(`SELECT COUNT(*) FROM matches WHERE id=$1 AND compat_score=87`, r.MatchID) == 1
	})
	_, body := e.call("GET", "/matching/matches", nil, a)
	if !strings.Contains(string(body), `"compat_score":87`) || !strings.Contains(string(body), "You both like hiking") ||
		!strings.Contains(string(body), "Only 2 years apart") {
		t.Fatalf("open match: %s", body)
	}

	// What the AI service received: tags, age, hours, counts. No names, refs or coordinates.
	st.mu.Lock()
	raw, _ := json.Marshal(st.scoreIn[0])
	st.mu.Unlock()
	for _, leak := range []string{"Scorer", "display_name", "lat", "lng", "email", e.refOf(a), a, b} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("scoring request leaks %q: %s", leak, raw)
		}
	}

	// Incognito match: the age reason is dropped before it is stored or shown.
	c, d := e.user("Anon C"), e.user("Anon D")
	e.call("POST", "/users/me/mode", map[string]string{"mode": "incognito"}, c)
	e.call("POST", "/users/me/mode", map[string]string{"mode": "incognito"}, d)
	e.swipe(c, e.refOf(d), "like")
	_, r2 := e.swipe(d, e.refOf(c), "like")
	waitFor(t, func() bool {
		return e.count(`SELECT COUNT(*) FROM matches WHERE id=$1 AND compat_score IS NOT NULL`, r2.MatchID) == 1
	})
	_, body = e.call("GET", "/matching/matches", nil, c)
	if strings.Contains(string(body), "years apart") {
		t.Fatalf("age-derived reason leaked in an incognito match: %s", body)
	}
	if !strings.Contains(string(body), "You both like hiking") {
		t.Fatalf("interest reason should remain: %s", body)
	}

	// Rescore: participants only.
	if c, _ := e.call("POST", "/matching/matches/"+r.MatchID+"/rescore", nil, a); c != 200 {
		t.Fatalf("rescore: %d", c)
	}
	if c, _ := e.call("POST", "/matching/matches/"+r.MatchID+"/rescore", nil, e.user("Stranger")); c != 404 {
		t.Fatalf("stranger rescore: %d", c)
	}
}

func TestScoringOutageNeverBreaksMatching(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	st.mu.Lock()
	st.failing = true
	st.mu.Unlock()
	a, b := e.user("A"), e.user("B")
	e.swipe(a, e.refOf(b), "like")
	if _, r := e.swipe(b, e.refOf(a), "like"); !r.Matched {
		t.Fatal("matching must work with the AI service down")
	}
	_, body := e.call("GET", "/matching/matches", nil, a)
	if strings.Contains(string(body), "compat_score") {
		t.Fatalf("no score expected: %s", body)
	}
}

// TestRealAIService exercises the actual Python service over HTTP to verify
// the JSON contract end to end. Run with TEST_AI_URL and TEST_AI_KEY set.
func TestRealAIService(t *testing.T) {
	url, key := os.Getenv("TEST_AI_URL"), os.Getenv("TEST_AI_KEY")
	if url == "" {
		t.Skip("TEST_AI_URL not set")
	}
	e := setup(t)
	cfg := e.deps.Cfg
	cfg.AIServiceURL, cfg.AIServiceKey = url, key
	e.deps.Cfg = cfg
	e.h = server.New(e.deps).Handler

	u := e.user("Real AI")
	for text, want := range map[string]int{"I'm 16 and bored": 422, "Love hiking and strong coffee": 200, "send me money": 422} {
		if c, b := e.call("PATCH", "/users/me", map[string]string{"bio": text}, u); c != want {
			t.Errorf("bio %q: got %d want %d (%s)", text, c, want, b)
		}
	}

	a, b := e.user("Real A"), e.user("Real B")
	e.call("PATCH", "/users/me", map[string]any{"tags": []string{"hiking", "coffee", "jazz"}}, a)
	e.call("PATCH", "/users/me", map[string]any{"tags": []string{"hiking", "coffee", "jazz"}}, b)
	e.swipe(a, e.refOf(b), "like")
	_, r := e.swipe(b, e.refOf(a), "like")
	waitFor(t, func() bool {
		return e.count(`SELECT COUNT(*) FROM matches WHERE id=$1 AND compat_score >= 60`, r.MatchID) == 1
	})
	_, body := e.call("GET", "/matching/matches", nil, a)
	if !strings.Contains(string(body), "You both like") {
		t.Fatalf("real scorer reasons missing: %s", body)
	}
	_ = context.Background()
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
