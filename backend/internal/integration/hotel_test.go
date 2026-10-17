package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectme/internal/auth"
	"connectme/internal/geo"
	"connectme/internal/users"
)

type failingPurger struct{ calls int }

func (f *failingPurger) PurgeCheckin(context.Context, string) error {
	f.calls++
	return errors.New("mongo unavailable")
}

// square returns a closed ~220 m ring centred on lat,lng ([lng, lat] pairs).
func square(lat, lng float64) [][2]float64 {
	d := 0.001
	return [][2]float64{{lng - d, lat - d}, {lng + d, lat - d}, {lng + d, lat + d}, {lng - d, lat + d}, {lng - d, lat - d}}
}

func (e *env) hotel(lat, lng float64) string {
	e.t.Helper()
	admin := e.newUser("admin", true, "Admin")
	body := map[string]any{"name": "Grand Test Hotel", "address": "1 Test St", "polygon": square(lat, lng)}
	if c, _ := e.callAs("POST", "/geo/hotels", body, e.user("Regular"), "user"); c != 403 {
		e.t.Fatalf("non-admin creating a hotel: %d", c)
	}
	c, b := e.callAs("POST", "/geo/hotels", body, admin, "admin")
	if c != 201 {
		e.t.Fatalf("create hotel: %d %s", c, b)
	}
	var h struct{ ID string }
	decodeInto(e.t, b, &h)
	return h.ID
}

func TestHotelRejectsBadFences(t *testing.T) {
	e := setup(t)
	admin := e.newUser("admin", true, "Admin")
	lat, lng := randomBase()
	huge := [][2]float64{{lng, lat}, {lng + 1, lat}, {lng + 1, lat + 1}, {lng, lat + 1}, {lng, lat}}            // ~12000 km²
	bow := [][2]float64{{lng, lat}, {lng + .002, lat + .002}, {lng + .002, lat}, {lng, lat + .002}, {lng, lat}} // self-intersecting
	open := [][2]float64{{lng, lat}, {lng + .001, lat}, {lng + .001, lat + .001}, {lng, lat + .001}}
	for name, poly := range map[string][][2]float64{"huge": huge, "bowtie": bow, "unclosed": open} {
		if c, _ := e.callAs("POST", "/geo/hotels", map[string]any{"name": "X", "polygon": poly}, admin, "admin"); c != 422 {
			t.Errorf("%s fence accepted: %d", name, c)
		}
	}
}

func TestHotelLifecycleAndAutoDelete(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	lat, lng := randomBase()
	hotelID := e.hotel(lat, lng)

	u1, u2, u3 := e.user("Una Hotel"), e.user("Dos Hotel"), e.user("Tres Outside")
	e.locate(u1, lat, lng)
	e.locate(u2, lat+0.0003, lng+0.0003)
	e.locate(u3, lat+0.02, lng) // ~2 km away

	// Geofence and freshness are enforced server-side.
	if c, _ := e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u3); c != 422 {
		t.Fatalf("outside fence: %d", c)
	}
	e.exec(`UPDATE profiles SET location_updated_at = NOW() - INTERVAL '1 hour' WHERE user_id=$1`, u1)
	if c, _ := e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u1); c != 422 {
		t.Fatalf("stale location: %d", c)
	}
	e.locate(u1, lat, lng)

	oldRef := e.refOf(u1)
	if c, b := e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u1); c != 201 {
		t.Fatalf("check-in: %d %s", c, b)
	}
	if c, _ := e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u1); c != 409 {
		t.Fatalf("double check-in: %d", c)
	}
	if e.refOf(u1) == oldRef {
		t.Fatal("ref must rotate on check-in")
	}
	if n := e.count(`SELECT COUNT(*) FROM profiles WHERE user_id=$1 AND mode='hotel' AND prev_mode='open'`, u1); n != 1 {
		t.Fatal("check-in must force hotel mode and remember the previous mode")
	}
	e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u2)

	// Feed is anonymous, hotel-scoped and members-only.
	c, body := e.call("GET", "/geo/hotels/"+hotelID+"/users", nil, u1)
	s := string(body)
	if c != 200 || strings.Contains(s, "Dos Hotel") || !strings.Contains(s, `"hotel_name":"Grand Test Hotel"`) ||
		!strings.Contains(s, `"anonymous":true`) || strings.Contains(s, `"distance"`) {
		t.Fatalf("feed: %d %s", c, s)
	}
	if c, _ := e.call("GET", "/geo/hotels/"+hotelID+"/users", nil, u3); c != 403 {
		t.Fatalf("non-member feed: %d", c)
	}
	// Hotel guests never appear in ordinary nearby search.
	nearby := e.user("Passerby")
	e.locate(nearby, lat+0.0005, lng)
	if _, body := e.call("GET", "/users/nearby?radius_km=5", nil, nearby); strings.Contains(string(body), e.refOf(u1)) ||
		strings.Contains(string(body), e.refOf(u2)) {
		t.Fatalf("hotel guests leaked into nearby: %s", body)
	}

	// Board: sanitized, members-only.
	c, body = e.call("POST", "/geo/hotels/"+hotelID+"/board", map[string]string{"message": "<b>Anyone want dinner?</b><script>x()</script>"}, u1)
	if c != 201 || strings.Contains(string(body), "<") {
		t.Fatalf("post: %d %s", c, body)
	}
	if _, body := e.call("GET", "/geo/hotels/"+hotelID+"/board", nil, u2); !strings.Contains(string(body), "Anyone want dinner?") {
		t.Fatalf("board: %s", body)
	}
	if c, _ := e.call("GET", "/geo/hotels/"+hotelID+"/board", nil, u3); c != 403 {
		t.Fatalf("non-member board: %d", c)
	}

	// Drinks: anonymous, once per pair.
	ref2 := e.refOf(u2)
	if c, _ := e.call("POST", "/geo/hotels/"+hotelID+"/drink", map[string]string{"to_ref": ref2}, u1); c != 204 {
		t.Fatalf("drink: %d", c)
	}
	if c, _ := e.call("POST", "/geo/hotels/"+hotelID+"/drink", map[string]string{"to_ref": ref2}, u1); c != 409 {
		t.Fatalf("second drink: %d", c)
	}
	if _, body := e.call("GET", "/geo/hotels/"+hotelID+"/drinks", nil, u2); !strings.Contains(string(body), e.refOf(u1)) {
		t.Fatalf("drinks: %s", body)
	}

	// Match inside the hotel is a hotel match bound to the stay.
	e.call("POST", "/matching/swipe", map[string]string{"target_ref": ref2, "action": "like"}, u1)
	_, mb := e.call("POST", "/matching/swipe", map[string]string{"target_ref": e.refOf(u1), "action": "like"}, u2)
	var m swipeRes
	decodeInto(t, mb, &m)
	if !m.Matched {
		t.Fatal("expected a hotel match")
	}
	if n := e.count(`SELECT COUNT(*) FROM matches WHERE id=$1 AND mode='hotel' AND checkin_id IS NOT NULL`, m.MatchID); n != 1 {
		t.Fatal("hotel match must be bound to a check-in")
	}
	if n := e.count(`SELECT COUNT(*) FROM swipes WHERE checkin_id IS NOT NULL AND actor_id IN ($1,$2)`, u1, u2); n != 2 {
		t.Fatalf("in-hotel swipes must be bound to the stay, got %d", n)
	}
	e.call("POST", "/matching/matches/"+m.MatchID+"/reveal", nil, u1)

	// Checkout: mode restored, ref rotated, data kept for 24h only.
	refBefore := e.refOf(u1)
	if c, _ := e.call("POST", "/geo/checkout", nil, u1); c != 204 {
		t.Fatalf("checkout: %d", c)
	}
	if e.refOf(u1) == refBefore {
		t.Fatal("ref must rotate on checkout")
	}
	if n := e.count(`SELECT COUNT(*) FROM profiles WHERE user_id=$1 AND mode='open' AND prev_mode IS NULL`, u1); n != 1 {
		t.Fatal("checkout must restore the previous mode")
	}
	if n := e.count(`SELECT COUNT(*) FROM hotel_checkins WHERE user_id=$1 AND auto_delete_at BETWEEN NOW() + INTERVAL '23 hours' AND NOW() + INTERVAL '25 hours'`, u1); n != 1 {
		t.Fatal("auto_delete_at must be checkout + 24h")
	}

	svc := server_geo(e)
	if res, err := svc.RunOnce(ctx); err != nil || e.count(`SELECT COUNT(*) FROM matches WHERE id=$1`, m.MatchID) != 1 {
		t.Fatalf("nothing may be purged before 24h: %+v %v", res, err)
	}

	// An unavailable external store must keep the data and retry later.
	e.exec(`UPDATE hotel_checkins SET auto_delete_at = NOW() - INTERVAL '1 minute' WHERE user_id=$1`, u1)
	fp := &failingPurger{}
	failing := geo.NewService(e.db, users.NewService(e.db, nil, auth.StubCSAMScanner{}), fp)
	if res, err := failing.RunOnce(ctx); err != nil || res.Purged != 0 || fp.calls == 0 {
		t.Fatalf("failed purge: %+v %v calls=%d", res, err, fp.calls)
	}
	if e.count(`SELECT COUNT(*) FROM matches WHERE id=$1`, m.MatchID) != 1 {
		t.Fatal("data must survive a failed external purge")
	}

	// After 24h everything bound to the stay is gone.
	res, err := svc.RunOnce(ctx)
	if err != nil || res.Purged < 1 {
		t.Fatalf("purge: %+v %v", res, err)
	}
	for name, q := range map[string]string{
		"check-in":    fmt.Sprintf(`SELECT COUNT(*) FROM hotel_checkins WHERE user_id='%s'`, u1),
		"match":       fmt.Sprintf(`SELECT COUNT(*) FROM matches WHERE id='%s'`, m.MatchID),
		"reveal":      fmt.Sprintf(`SELECT COUNT(*) FROM reveal_consents WHERE match_id='%s'`, m.MatchID),
		"board posts": fmt.Sprintf(`SELECT COUNT(*) FROM hotel_board_posts WHERE user_id='%s'`, u1),
		"drinks":      fmt.Sprintf(`SELECT COUNT(*) FROM hotel_drinks WHERE from_id='%s'`, u1),
		"swipes":      fmt.Sprintf(`SELECT COUNT(*) FROM swipes WHERE actor_id='%s' AND target_id='%s'`, u1, u2),
	} {
		if n := e.count(q); n != 0 {
			t.Errorf("%s survived the purge (%d rows)", name, n)
		}
	}
	// u2's own stay and global profile are untouched.
	if e.count(`SELECT COUNT(*) FROM hotel_checkins WHERE user_id=$1 AND checked_out_at IS NULL`, u2) != 1 ||
		e.count(`SELECT COUNT(*) FROM profiles WHERE user_id=$1`, u2) != 1 {
		t.Fatal("purge must not touch the other guest's stay or profile")
	}

	// Max stay: automatic checkout, mode restored.
	e.exec(`UPDATE hotel_checkins SET checked_in_at = NOW() - INTERVAL '73 hours' WHERE user_id=$1`, u2)
	if res, err := svc.RunOnce(ctx); err != nil || res.AutoCheckedOut < 1 {
		t.Fatalf("max stay: %+v %v", res, err)
	}
	if e.count(`SELECT COUNT(*) FROM profiles WHERE user_id=$1 AND mode<>'hotel'`, u2) != 1 {
		t.Fatal("max-stay checkout must restore mode")
	}
}

func TestLeavingTheFenceChecksYouOut(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	hotelID := e.hotel(lat, lng)
	u := e.user("Leaver")
	e.locate(u, lat, lng)
	if c, _ := e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u); c != 201 {
		t.Fatalf("check-in: %d", c)
	}
	// Move 3 km away; pretend the stay began 10 minutes ago.
	e.exec(`UPDATE hotel_checkins SET checked_in_at = NOW() - INTERVAL '10 minutes' WHERE user_id=$1`, u)
	e.locate(u, lat+0.03, lng)

	res, err := server_geo(e).RunOnce(context.Background())
	if err != nil || res.AutoCheckedOut < 1 {
		t.Fatalf("exit detection: %+v %v", res, err)
	}
	if e.count(`SELECT COUNT(*) FROM hotel_checkins WHERE user_id=$1 AND checked_out_at IS NOT NULL`, u) != 1 {
		t.Fatal("user should be checked out")
	}
	if e.count(`SELECT COUNT(*) FROM profiles WHERE user_id=$1 AND mode<>'hotel'`, u) != 1 {
		t.Fatal("mode should be restored")
	}
}

func server_geo(e *env) *geo.Service {
	return geo.NewService(e.db, users.NewService(e.db, nil, auth.StubCSAMScanner{}))
}
