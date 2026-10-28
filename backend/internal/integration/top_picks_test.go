package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type pickList struct {
	Picks []struct {
		Profile struct {
			DisplayName string `json:"display_name"`
		} `json:"profile"`
		Why string `json:"why"`
	} `json:"picks"`
}

func (e *env) topPicks(uid string) pickList {
	e.t.Helper()
	c, b := e.call("GET", "/matching/top-picks", nil, uid)
	if c != 200 {
		e.t.Fatalf("top picks: %d %s", c, b)
	}
	var p pickList
	if err := json.Unmarshal(b, &p); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func TestTopPicksRankByWhatYouShareAndAreExplained(t *testing.T) {
	e := setup(t)
	e.withStripe()
	lat, lng := randomBase()
	me := e.located("Me", lat, lng)
	e.call("PATCH", "/users/me", map[string]any{"tags": []string{"jazz", "hiking", "coffee"}}, me)
	e.putDetails(me, map[string]any{"goal": "long_term", "kids": "want", "smoking": "never"})

	mk := func(name string, dlat float64, tags []string, d map[string]any) string {
		u := e.located(name, lat+dlat, lng)
		e.call("PATCH", "/users/me", map[string]any{"tags": tags}, u)
		if d != nil {
			e.putDetails(u, d)
		}
		return u
	}
	best := mk("Best", 0.012, []string{"jazz", "hiking", "coffee"}, map[string]any{"goal": "long_term", "kids": "want", "smoking": "never"})
	good := mk("Good", 0.010, []string{"jazz", "chess"}, map[string]any{"goal": "long_term"})
	mk("Near But Different", 0.001, []string{"gaming"}, map[string]any{"goal": "casual", "smoking": "often"})
	mk("Far Different", 0.02, []string{"skating"}, nil)
	_ = good

	p := e.topPicks(me)
	if len(p.Picks) != 3 {
		t.Fatalf("a free member gets 3 picks, got %d: %+v", len(p.Picks), p)
	}
	if p.Picks[0].Profile.DisplayName != "Best" || p.Picks[1].Profile.DisplayName != "Good" {
		t.Errorf("order: %+v", p.Picks)
	}
	if !strings.Contains(p.Picks[0].Why, "jazz") && !strings.Contains(p.Picks[0].Why, "long-term") {
		t.Errorf("reason for the best pick: %q", p.Picks[0].Why)
	}
	if !strings.Contains(p.Picks[1].Why, "long-term") && !strings.Contains(p.Picks[1].Why, "jazz") {
		t.Errorf("reason for the second pick: %q", p.Picks[1].Why)
	}

	// Someone you have already swiped on is not offered again.
	e.likeWith(me, best, "pass", "")
	for _, pk := range e.topPicks(me).Picks {
		if pk.Profile.DisplayName == "Best" {
			t.Errorf("a passed person came back as a pick")
		}
	}

	// Gold sees more picks than free members (up to ten).
	gold := e.located("Gold Me", lat, lng+0.001)
	e.subscribe(gold, "gold", "active", 24*time.Hour)
	for i := 0; i < 5; i++ {
		mk("Extra "+string(rune('A'+i)), 0.003+float64(i)*0.001, []string{"jazz"}, nil)
	}
	if n := len(e.topPicks(gold).Picks); n <= 3 || n > 10 {
		t.Errorf("a Gold member got %d picks", n)
	}
}

func TestTopPicksNeedLocationLikeDiscovery(t *testing.T) {
	e := setup(t)
	noLoc := e.user("No Location")
	if c, _ := e.call("GET", "/matching/top-picks", nil, noLoc); c != 403 && c != 422 {
		t.Errorf("without a location: %d", c)
	}
}
