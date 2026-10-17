package integration

import (
	"strings"
	"testing"
)

// guestAt creates a verified user and checks them in at the hotel.
func (e *env) guestAt(hotelID string, lat, lng float64, name string) string {
	e.t.Helper()
	u := e.user(name)
	e.locate(u, lat, lng)
	if c, b := e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u); c != 201 {
		e.t.Fatalf("check in %s: %d %s", name, c, b)
	}
	return u
}

func TestHotelMoodsFilterTheFeedAndNeverLeakIdentity(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	hotelID := e.hotel(lat, lng)
	biz := e.guestAt(hotelID, lat, lng, "Bea Business")
	lonely := e.guestAt(hotelID, lat+0.0002, lng, "Lou Lonely")
	guide := e.guestAt(hotelID, lat, lng+0.0002, "Gus Guide")
	plain := e.guestAt(hotelID, lat+0.0002, lng+0.0002, "Pat Plain")
	outsider := e.user("Out Sider")

	set := func(u, mood string) int {
		c, _ := e.call("PUT", "/geo/checkin/mood", map[string]string{"mood": mood}, u)
		return c
	}
	if c := set(biz, "business"); c != 204 {
		t.Fatalf("set mood: %d", c)
	}
	set(lonely, "company")
	set(guide, "guide")
	if c := set(guide, "party"); c != 422 {
		t.Errorf("bad mood: %d", c)
	}
	if c := set(outsider, "guide"); c != 403 {
		t.Errorf("mood without a stay: %d", c)
	}

	feed := func(viewer, q string) string {
		c, b := e.call("GET", "/geo/hotels/"+hotelID+"/users"+q, nil, viewer)
		if c != 200 {
			t.Fatalf("feed %q: %d %s", q, c, b)
		}
		return string(b)
	}
	all := feed(plain, "")
	for _, m := range []string{`"travel_mood":"business"`, `"travel_mood":"company"`, `"travel_mood":"guide"`} {
		if !strings.Contains(all, m) {
			t.Errorf("feed lacks %s: %s", m, all)
		}
	}
	only := feed(plain, "?mood=guide")
	if strings.Count(only, `"ref"`) != 1 || !strings.Contains(only, `"travel_mood":"guide"`) {
		t.Errorf("guide filter: %s", only)
	}
	for _, name := range []string{"Bea", "Lou", "Gus", "Pat"} {
		if strings.Contains(all, name) {
			t.Errorf("hotel feed leaked a name %q", name)
		}
	}
	if c, _ := e.call("GET", "/geo/hotels/"+hotelID+"/users?mood=party", nil, plain); c != 422 {
		t.Errorf("bad filter: %d", c)
	}

	// Counts, for guests only.
	c, b := e.call("GET", "/geo/hotels/"+hotelID+"/moods", nil, plain)
	if c != 200 || !strings.Contains(string(b), `"business":1`) || !strings.Contains(string(b), `"guide":1`) {
		t.Errorf("moods: %d %s", c, b)
	}
	if c, _ := e.call("GET", "/geo/hotels/"+hotelID+"/moods", nil, outsider); c != 403 {
		t.Errorf("outsider saw moods: %d", c)
	}

	// Clearing a mood removes it; the current stay reports mine.
	set(biz, "")
	if strings.Contains(feed(plain, ""), `"travel_mood":"business"`) {
		t.Error("cleared mood still shown")
	}
	_, b = e.call("GET", "/geo/checkin", nil, guide)
	if !strings.Contains(string(b), `"mood":"guide"`) {
		t.Errorf("current stay: %s", b)
	}
}
