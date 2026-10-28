package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

func (e *env) clearSponsorCap() {
	ctx := context.Background()
	keys, _ := e.rdb.Client.Keys(ctx, "sponsor:seen:*").Result()
	if len(keys) > 0 {
		e.rdb.Client.Del(ctx, keys...)
	}
}

func (e *env) sponsorFor(uid string) string {
	e.t.Helper()
	e.clearSponsorCap()
	c, b := e.call("GET", "/sponsors/feed", nil, uid)
	if c != 200 {
		e.t.Fatalf("sponsor feed: %d %s", c, b)
	}
	return string(b)
}

func (e *env) newPlacement(lat, lng float64, radius int, headline string, extra map[string]any) (int, string) {
	e.t.Helper()
	admin := e.newUser("admin", true, "Ad Admin")
	body := map[string]any{
		"advertiser": "Casa Verde", "headline": headline, "body": "Two dishes for the price of one.",
		"link_url":  "https://casaverde.example.com/offer",
		"starts_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		"ends_at":   time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339),
	}
	if radius > 0 {
		body["lat"], body["lng"], body["radius_km"] = lat, lng, radius
	}
	for k, v := range extra {
		body[k] = v
	}
	c, b := e.callAs("POST", "/sponsors/", body, admin, "admin")
	return c, string(b)
}

func TestSponsorPlacementsAreValidatedAndAdminOnly(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	member := e.user("Plain Member")
	if c, _ := e.call("POST", "/sponsors/", map[string]any{"advertiser": "x"}, member); c != 403 {
		t.Errorf("member created a placement: %d", c)
	}
	if c, _ := e.call("GET", "/sponsors/", nil, member); c != 403 {
		t.Errorf("member listed placements: %d", c)
	}
	for name, extra := range map[string]map[string]any{
		"http link":       {"link_url": "http://casaverde.example.com"},
		"ip link":         {"link_url": "https://203.0.113.7/offer"},
		"localhost":       {"link_url": "https://localhost/offer"},
		"userinfo":        {"link_url": "https://bank.com@evil.example.com/"},
		"internal host":   {"link_url": "https://db.internal/admin"},
		"javascript":      {"link_url": "javascript:alert(1)"},
		"no dot host":     {"link_url": "https://intranet/x"},
		"ended":           {"ends_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)},
		"end before":      {"starts_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339), "ends_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
		"huge radius":     {"radius_km": 500},
		"radius no place": {"lat": nil, "lng": nil},
		"short headline":  {"headline": "ab"},
		"bad coords":      {"lat": 120.0},
	} {
		if c, b := e.newPlacement(lat, lng, 10, "Two for one dinner", extra); c != 422 {
			t.Errorf("%s accepted: %d %s", name, c, b)
		}
	}
	if c, b := e.newPlacement(lat, lng, 10, "Two for one dinner", nil); c != 201 || strings.Contains(b, "created_by") {
		t.Errorf("valid placement: %d %s", c, b)
	}
}

func TestSponsorFeedTargetsRotatesAndRespectsNoAds(t *testing.T) {
	e := setup(t)
	e.withStripe()                                       // lets the test give a member a subscription through the webhook
	e.exec(`UPDATE sponsor_placements SET active=false`) // other runs' leftovers must not interfere
	lat, lng := randomBase()
	near, far, nowhere, silver := e.user("Near"), e.user("Far"), e.user("Nowhere"), e.user("Silver Member")
	e.locate(near, lat+0.01, lng)
	e.locate(far, lat+1.5, lng) // ~165 km away
	e.locate(silver, lat, lng+0.01)
	e.subscribe(silver, "silver", "active", 24*time.Hour)

	c, b := e.newPlacement(lat, lng, 25, "Local dinner offer", nil)
	if c != 201 {
		t.Fatalf("create: %d %s", c, b)
	}
	id := between(b, `"id":"`, `"`)

	got := e.sponsorFor(near)
	if !strings.Contains(got, id) || !strings.Contains(got, "Casa Verde") || !strings.Contains(got, "casaverde.example.com") {
		t.Fatalf("nearby member did not get the placement: %s", got)
	}
	for _, leak := range []string{"impressions", "clicks", "radius", "created_by", "lat", "lng"} {
		if strings.Contains(got, leak) {
			t.Errorf("member payload leaks %q: %s", leak, got)
		}
	}
	// At most one per few minutes per member (checked before anything clears the cap).
	if c, b := e.call("GET", "/sponsors/feed", nil, near); !strings.Contains(string(b), `"placement":null`) {
		t.Errorf("no frequency cap: %d %s", c, b)
	}
	if s := e.sponsorFor(far); strings.Contains(s, id) {
		t.Errorf("a member outside the radius got it: %s", s)
	}
	if s := e.sponsorFor(nowhere); strings.Contains(s, id) {
		t.Errorf("a member with no location got a local placement: %s", s)
	}
	if s := e.sponsorFor(silver); strings.Contains(s, id) || !strings.Contains(s, `"placement":null`) {
		t.Errorf("Silver (no ads) was shown a placement: %s", s)
	}

	// Counted once per showing; totals are visible to admins only.
	admin := e.newUser("admin", true, "Reader")
	_, list := e.callAs("GET", "/sponsors/", nil, admin, "admin")
	if !strings.Contains(string(list), `"impressions":1`) {
		t.Errorf("impressions: %s", list)
	}

	// Pausing stops it; resuming restores it; an impression cap ends it.
	if c, _ := e.callAs("POST", "/sponsors/"+id+"/pause", nil, admin, "admin"); c != 204 {
		t.Fatalf("pause: %d", c)
	}
	if s := e.sponsorFor(near); strings.Contains(s, id) {
		t.Errorf("paused placement served: %s", s)
	}
	e.callAs("POST", "/sponsors/"+id+"/resume", nil, admin, "admin")
	if s := e.sponsorFor(near); !strings.Contains(s, id) {
		t.Errorf("resumed placement not served: %s", s)
	}
	e.exec(`UPDATE sponsor_placements SET max_impressions = impressions WHERE id=$1::uuid`, id)
	if s := e.sponsorFor(near); strings.Contains(s, id) {
		t.Errorf("served past its impression cap: %s", s)
	}
	e.exec(`UPDATE sponsor_placements SET max_impressions = NULL, ends_at = NOW() - INTERVAL '1 minute' WHERE id=$1::uuid`, id)
	if s := e.sponsorFor(near); strings.Contains(s, id) {
		t.Errorf("served after it ended: %s", s)
	}
	if c, _ := e.call("POST", "/sponsors/"+id+"/click", nil, near); c != 404 {
		t.Errorf("click on an ended placement: %d", c)
	}
}

func TestSponsorRotationClicksAndLinks(t *testing.T) {
	e := setup(t)
	e.exec(`UPDATE sponsor_placements SET active=false`)
	lat, lng := randomBase()
	u := e.user("Viewer")
	e.locate(u, lat, lng)
	_, a := e.newPlacement(lat, lng, 50, "First offer here", nil)
	_, b := e.newPlacement(lat, lng, 50, "Second offer here", map[string]any{"link_url": ""})
	ida, idb := between(a, `"id":"`, `"`), between(b, `"id":"`, `"`)

	// The least-shown placement goes first, so they alternate.
	first := e.sponsorFor(u)
	second := e.sponsorFor(u)
	if strings.Contains(first, ida) == strings.Contains(second, ida) || (!strings.Contains(first, ida) && !strings.Contains(first, idb)) {
		t.Errorf("no fair rotation:\n%s\n%s", first, second)
	}
	// Clicking returns the link and counts; a placement without a link cannot be clicked.
	c, body := e.call("POST", "/sponsors/"+ida+"/click", nil, u)
	if c != 200 || !strings.Contains(string(body), "https://casaverde.example.com/offer") {
		t.Fatalf("click: %d %s", c, body)
	}
	if c, _ := e.call("POST", "/sponsors/"+idb+"/click", nil, u); c != 404 {
		t.Errorf("clicking a placement with no link: %d", c)
	}
	if c, _ := e.call("POST", "/sponsors/not-a-uuid/click", nil, u); c != 404 {
		t.Errorf("bad id: %d", c)
	}
	admin := e.newUser("admin", true, "Stats")
	_, list := e.callAs("GET", "/sponsors/", nil, admin, "admin")
	if !strings.Contains(string(list), `"clicks":1`) {
		t.Errorf("click not counted: %s", list)
	}
}
