package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func (e *env) nearbyNames(uid, query string) []string {
	e.t.Helper()
	c, b := e.call("GET", "/users/nearby?"+query, nil, uid)
	if c != 200 {
		e.t.Fatalf("nearby: %d %s", c, b)
	}
	var r struct {
		Results []struct {
			DisplayName string `json:"display_name"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, x := range r.Results {
		out = append(out, x.DisplayName)
	}
	return out
}

// The search starts with small circles and widens; the answer must be the
// same people in the same order as one big circle would give, a boosted person
// far away must still come first, and a far person must be found when the
// near circles are empty.
func TestNearbyRingsGiveTheSameAnswerAsOneBigCircle(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	me := e.located("Viewer", lat, lng)
	e.located("Close", lat+0.005, lng)          // ~0.6 km
	e.located("Mid", lat+0.05, lng)             // ~5.5 km
	e.located("Far", lat+0.15, lng)             // ~17 km
	e.located("TooFar", lat+0.6, lng)           // ~67 km, outside 25
	boost := e.located("Boosted", lat+0.2, lng) // ~22 km

	got := e.nearbyNames(me, "radius_km=25")
	want := []string{"Close", "Mid", "Far", "Boosted"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("rings: got %v want %v", got, want)
	}
	// Paging reaches the same people.
	if first, second := e.nearbyNames(me, "radius_km=25&limit=2"), e.nearbyNames(me, "radius_km=25&limit=2&offset=2"); strings.Join(append(first, second...), ",") != strings.Join(want, ",") {
		t.Errorf("paging: %v then %v", first, second)
	}
	// Only the far ring holds anyone for this viewer.
	loner := e.located("Loner", lat+0.5, lng)
	if got := e.nearbyNames(loner, "radius_km=100"); len(got) == 0 {
		t.Errorf("a person only found by the widest circle was missed")
	}

	// A boost jumps the queue, however far away.
	e.exec(`UPDATE profiles SET boosted_until = NOW() + INTERVAL '30 minutes' WHERE user_id = $1::uuid`, boost)
	got = e.nearbyNames(me, "radius_km=25")
	if len(got) != 4 || got[0] != "Boosted" {
		t.Errorf("boosted person not first: %v", got)
	}
	// Hidden people stay hidden whichever ring finds them.
	e.exec(`UPDATE profiles SET invisible = TRUE WHERE display_name = 'Close' AND user_id <> $1::uuid`, me)
	for _, n := range e.nearbyNames(me, "radius_km=25") {
		if n == "Close" {
			t.Errorf("an invisible person was returned")
		}
	}
}
