package integration

import (
	"strings"
	"testing"
	"time"
)

func TestRewindUndoesOnlyARecentPassAndIsAGoldPerk(t *testing.T) {
	e := setup(t)
	e.withStripe() // lets the test give a member a subscription
	lat, lng := randomBase()
	free, gold := e.located("Free Member", lat, lng), e.located("Gold Member", lat+0.001, lng)
	a, b := e.located("Person A", lat+0.004, lng), e.located("Person B", lat+0.005, lng)
	e.subscribe(gold, "gold", "active", 24*time.Hour)

	swipe := func(u, target, action string) int {
		c, _ := e.call("POST", "/matching/swipe", map[string]string{"target_ref": e.refOf(target), "action": action}, u)
		return c
	}
	seen := func(u string) string { return strings.Join(e.nearbyNames(u, "radius_km=25"), ",") }

	// Free members cannot rewind.
	swipe(free, a, "pass")
	if c, b := e.call("POST", "/matching/rewind", nil, free); c != 402 || !strings.Contains(string(b), "Gold") {
		t.Errorf("free rewind: %d %s", c, b)
	}

	// Nothing to undo yet.
	if c, _ := e.call("POST", "/matching/rewind", nil, gold); c != 422 {
		t.Errorf("rewind with no swipes: %d", c)
	}
	// A pass can be undone, and the person comes back.
	swipe(gold, a, "pass")
	if strings.Contains(seen(gold), "Person A") {
		t.Fatalf("passed person still in discovery: %s", seen(gold))
	}
	if c, b := e.call("POST", "/matching/rewind", nil, gold); c != 204 {
		t.Fatalf("rewind: %d %s", c, b)
	}
	if !strings.Contains(seen(gold), "Person A") {
		t.Errorf("rewound person did not return: %s", seen(gold))
	}
	// Rewinding twice does not reach further back.
	if c, _ := e.call("POST", "/matching/rewind", nil, gold); c != 422 {
		t.Errorf("second rewind: %d", c)
	}
	// A like is never undone.
	swipe(gold, b, "like")
	if c, _ := e.call("POST", "/matching/rewind", nil, gold); c != 422 {
		t.Errorf("rewinding a like: %d", c)
	}
	if strings.Contains(seen(gold), "Person B") {
		t.Errorf("a liked person came back: %s", seen(gold))
	}
	// An old pass is out of reach.
	swipe(gold, a, "pass")
	e.exec(`UPDATE swipes SET created_at = NOW() - INTERVAL '11 minutes' WHERE actor_id=$1::uuid AND target_id=$2::uuid`, gold, a)
	if c, _ := e.call("POST", "/matching/rewind", nil, gold); c != 422 {
		t.Errorf("rewinding an old pass: %d", c)
	}
}
