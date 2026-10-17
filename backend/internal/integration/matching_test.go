package integration

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"connectme/internal/wallet"
)

type swipeRes struct {
	Matched bool   `json:"matched"`
	MatchID string `json:"match_id"`
}

func (e *env) swipe(uid, targetRef, action string) (int, swipeRes) {
	c, b := e.call("POST", "/matching/swipe", map[string]string{"target_ref": targetRef, "action": action}, uid)
	var r swipeRes
	_ = json.Unmarshal(b, &r)
	return c, r
}

func TestSwipeMatchFlow(t *testing.T) {
	e := setup(t)
	a, b := e.user("Ann"), e.user("Ben")

	if c, _ := e.swipe(a, e.refOf(a), "like"); c != 422 {
		t.Fatalf("self swipe: %d", c)
	}
	if c, _ := e.swipe(a, e.refOf(e.newUser("user", false, "Unverified")), "like"); c != 404 {
		t.Fatalf("unverified target must be invisible: %d", c)
	}
	if c, _ := e.call("POST", "/matching/swipe", map[string]string{"target_ref": e.refOf(b), "action": "dance"}, a); c != 422 {
		t.Fatalf("bad action: %d", c)
	}

	// One-sided like is not a match; the second like is, and it is idempotent.
	if c, r := e.swipe(a, e.refOf(b), "like"); c != 200 || r.Matched {
		t.Fatalf("first like: %d %+v", c, r)
	}
	c, r := e.swipe(b, e.refOf(a), "like")
	if c != 200 || !r.Matched || r.MatchID == "" {
		t.Fatalf("mutual like: %d %+v", c, r)
	}
	if c, again := e.swipe(b, e.refOf(a), "like"); c != 200 || again.MatchID != r.MatchID {
		t.Fatalf("repeat swipe must be idempotent: %d %+v", c, again)
	}
	if c, _ := e.swipe(b, e.refOf(a), "pass"); c != 409 {
		t.Fatalf("changing a swipe: %d", c)
	}

	// Listing: open match shows the full counterpart.
	c2, body := e.call("GET", "/matching/matches", nil, a)
	if c2 != 200 || !strings.Contains(string(body), "Ben") || !strings.Contains(string(body), r.MatchID) {
		t.Fatalf("matches: %d %s", c2, body)
	}
	// Unmatch removes it for both and blocks further reveal.
	if c, _ := e.call("DELETE", "/matching/matches/"+r.MatchID, nil, a); c != 204 {
		t.Fatalf("unmatch: %d", c)
	}
	if _, body := e.call("GET", "/matching/matches", nil, b); strings.Contains(string(body), r.MatchID) {
		t.Fatal("unmatched pair still listed")
	}
	if c, _ := e.call("POST", "/matching/matches/"+r.MatchID+"/reveal", nil, b); c != 404 {
		t.Fatalf("reveal after unmatch: %d", c)
	}
	// A stranger cannot touch someone else's match.
	if c, _ := e.call("DELETE", "/matching/matches/"+r.MatchID, nil, e.user("Eve")); c != 404 {
		t.Fatalf("foreign unmatch: %d", c)
	}
}

func TestSimultaneousLikesCreateExactlyOneMatch(t *testing.T) {
	e := setup(t)
	for i := 0; i < 10; i++ {
		a, b := e.user("A"), e.user("B")
		ra, rb := e.refOf(a), e.refOf(b)
		var wg sync.WaitGroup
		results := make([]swipeRes, 2)
		wg.Add(2)
		go func() { defer wg.Done(); _, results[0] = e.swipe(a, rb, "like") }()
		go func() { defer wg.Done(); _, results[1] = e.swipe(b, ra, "like") }()
		wg.Wait()
		if !results[0].Matched && !results[1].Matched {
			t.Fatalf("pair %d: both likes landed but neither side saw a match", i)
		}
		u1, u2 := a, b
		if u2 < u1 {
			u1, u2 = u2, u1
		}
		if n := e.count(`SELECT COUNT(*) FROM matches WHERE user1_id=$1 AND user2_id=$2`, u1, u2); n != 1 {
			t.Fatalf("pair %d: %d match rows", i, n)
		}
	}
}

func TestIncognitoMatchRevealFlow(t *testing.T) {
	e := setup(t)
	a, b := e.user("Ann Hidden"), e.user("Ben Hidden")
	e.call("POST", "/users/me/mode", map[string]string{"mode": "incognito"}, a)
	e.call("POST", "/users/me/mode", map[string]string{"mode": "incognito"}, b)

	e.swipe(a, e.refOf(b), "like")
	_, r := e.swipe(b, e.refOf(a), "like")
	if !r.Matched {
		t.Fatal("expected match")
	}
	if n := e.count(`SELECT COUNT(*) FROM matches WHERE id=$1 AND mode='incognito'`, r.MatchID); n != 1 {
		t.Fatal("incognito pair must create an incognito match")
	}
	if _, body := e.call("GET", "/matching/matches", nil, a); strings.Contains(string(body), "Ben Hidden") {
		t.Fatalf("name leaked before reveal: %s", body)
	}
	var rev struct{ Mutual bool }
	_, body := e.call("POST", "/matching/matches/"+r.MatchID+"/reveal", nil, a)
	decodeInto(t, body, &rev)
	if rev.Mutual {
		t.Fatal("one-sided reveal must not be mutual")
	}
	if _, body := e.call("GET", "/matching/matches", nil, b); strings.Contains(string(body), "Ann Hidden") {
		t.Fatalf("one-sided reveal unmasked: %s", body)
	}
	_, body = e.call("POST", "/matching/matches/"+r.MatchID+"/reveal", nil, b)
	decodeInto(t, body, &rev)
	if !rev.Mutual {
		t.Fatal("both revealed: want mutual")
	}
	if _, body := e.call("GET", "/matching/matches", nil, a); !strings.Contains(string(body), "Ben Hidden") {
		t.Fatalf("mutual reveal must unmask: %s", body)
	}
}

func TestCoinsSuperLikeBoostAndSwipeLimit(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	a, b := e.user("Ann"), e.user("Ben")

	if c, _ := e.call("POST", "/matching/superlike", map[string]string{"target_ref": e.refOf(b)}, a); c != 402 {
		t.Fatalf("no coins: %d", c)
	}
	if n := e.count(`SELECT COUNT(*) FROM swipes WHERE actor_id=$1`, a); n != 0 {
		t.Fatal("failed super like must not record a swipe")
	}
	if _, err := wallet.Grant(ctx, e.db, a, 30, "test", ""); err != nil {
		t.Fatal(err)
	}
	if c, _ := e.call("POST", "/matching/superlike", map[string]string{"target_ref": e.refOf(b)}, a); c != 200 {
		t.Fatalf("super like: %d", c)
	}
	if bal, _ := wallet.Balance(ctx, e.db, a); bal != 25 {
		t.Fatalf("balance after super like: %d want 25", bal)
	}
	if c, _ := e.call("POST", "/matching/boost", nil, a); c != 200 {
		t.Fatalf("boost: %d", c)
	}
	if bal, _ := wallet.Balance(ctx, e.db, a); bal != 5 {
		t.Fatalf("balance after boost: %d want 5", bal)
	}
	if c, _ := e.call("POST", "/matching/boost", nil, a); c != 402 {
		t.Fatalf("second boost with 5 coins: %d", c)
	}

	// Swipe 51 in 15 minutes -> 429 (spec F1.22).
	s := e.user("Swiper")
	target := e.refOf(e.user("Target"))
	for i := 1; i <= 50; i++ {
		if c, _ := e.call("POST", "/matching/swipe", map[string]string{"target_ref": target, "action": "pass"}, s); c == 429 {
			t.Fatalf("request %d limited too early", i)
		}
	}
	if c, _ := e.call("POST", "/matching/swipe", map[string]string{"target_ref": target, "action": "pass"}, s); c != 429 {
		t.Fatalf("51st swipe: %d want 429", c)
	}
}

func TestBoostedUsersSortFirst(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	viewer, near, boosted := e.user("Viewer"), e.user("Near"), e.user("Boosted")
	e.locate(viewer, lat, lng)
	e.locate(near, lat+0.002, lng)
	e.locate(boosted, lat+0.05, lng) // farther away
	if _, err := wallet.Grant(context.Background(), e.db, boosted, 20, "test", ""); err != nil {
		t.Fatal(err)
	}
	e.call("POST", "/matching/boost", nil, boosted)

	_, body := e.call("GET", "/users/nearby?radius_km=25", nil, viewer)
	s := string(body)
	if i, j := strings.Index(s, "Boosted"), strings.Index(s, "Near"); i < 0 || j < 0 || i > j {
		t.Fatalf("boosted user should come first: %s", s)
	}
}
