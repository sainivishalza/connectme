package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type pEvent struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Going      int    `json:"going"`
	Joined     bool   `json:"joined"`
	PriceCoins int    `json:"price_coins"`
	GuestsOnly bool   `json:"guests_only"`
	Distance   string `json:"distance"`
}

func (e *env) makePartner(kind, hotelID string, lat, lng float64, bps *int) string {
	e.t.Helper()
	admin := e.newUser("admin", true, "PAdmin")
	body := map[string]any{"kind": kind, "name": "Test " + kind, "description": "A place", "address": "1 Road", "lat": lat, "lng": lng}
	if hotelID != "" {
		body["hotel_id"] = hotelID
	}
	if bps != nil {
		body["commission_bps"] = *bps
	}
	if c, _ := e.call("POST", "/partners/", body, e.user("Plain")); c != 403 {
		e.t.Fatalf("member created a partner: %d", c)
	}
	c, b := e.callAs("POST", "/partners/", body, admin, "admin")
	if c != 201 {
		e.t.Fatalf("create partner: %d %s", c, b)
	}
	var p struct{ ID string }
	decodeInto(e.t, b, &p)
	return p.ID
}

func (e *env) makeEvent(partner, kind string, hours int, capacity *int, price int) (int, pEvent) {
	e.t.Helper()
	admin := e.newUser("admin", true, "EAdmin")
	body := map[string]any{"kind": kind, "title": "Rooftop " + kind, "description": "Come along",
		"starts_at": time.Now().Add(time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339),
		"ends_at":   time.Now().Add(time.Duration(hours+3) * time.Hour).UTC().Format(time.RFC3339), "price_coins": price}
	if capacity != nil {
		body["capacity"] = *capacity
	}
	c, b := e.callAs("POST", "/partners/"+partner+"/events", body, admin, "admin")
	var ev pEvent
	if c == 201 {
		decodeInto(e.t, b, &ev)
	}
	return c, ev
}

func (e *env) eventsFor(uid string) []pEvent {
	e.t.Helper()
	c, b := e.call("GET", "/partners/events", nil, uid)
	if c != 200 {
		e.t.Fatalf("events: %d %s", c, b)
	}
	var r struct {
		Events []pEvent `json:"events"`
	}
	decodeInto(e.t, b, &r)
	return r.Events
}

func (e *env) grantCoins(uid string, n int) {
	e.t.Helper()
	e.exec(`INSERT INTO coin_ledger (user_id, delta, reason) VALUES ($1,$2,'test_grant')`, uid, n)
}

func (e *env) balance(uid string) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COALESCE(SUM(delta),0) FROM coin_ledger WHERE user_id=$1`, uid).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func hasEvent(es []pEvent, id string) bool {
	for _, x := range es {
		if x.ID == id {
			return true
		}
	}
	return false
}

func TestSponsoredEventsTicketsCapacityAndRefunds(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	partner := e.makePartner("activity", "", lat, lng, nil)
	two := 2
	c, ev := e.makeEvent(partner, "concert", 48, &two, 30)
	if c != 201 {
		t.Fatalf("create event: %d", c)
	}
	// Validation.
	if c, _ := e.makeEvent(partner, "concert", -2, nil, 0); c != 422 {
		t.Errorf("past event: %d", c)
	}
	if c, _ := e.makeEvent(partner, "mixer", 48, nil, 0); c != 422 {
		t.Errorf("mixer without a hotel partner: %d", c)
	}
	if c, _ := e.makeEvent(partner, "party", 48, nil, 0); c != 422 {
		t.Errorf("bad kind: %d", c)
	}

	a, b, d := e.user("Ann"), e.user("Ben"), e.user("Dee")
	far := e.user("Far Away")
	e.locate(a, lat+0.01, lng)
	e.locate(b, lat, lng+0.01)
	e.locate(d, lat+0.01, lng+0.01)
	e.locate(far, lat+1.5, lng) // ~165 km away
	e.grantCoins(a, 100)
	e.grantCoins(b, 20)
	e.grantCoins(d, 100)

	if !hasEvent(e.eventsFor(a), ev.ID) {
		t.Fatal("nearby event not listed")
	}
	if hasEvent(e.eventsFor(far), ev.ID) {
		t.Error("distant event listed")
	}
	if es := e.eventsFor(a); es[0].Distance == "" || !strings.HasPrefix(es[0].Distance, "within") {
		t.Errorf("distance: %q", es[0].Distance)
	}
	if c, _ := e.call("POST", "/partners/events/"+ev.ID+"/rsvp", nil, far); c != 404 {
		t.Errorf("far join: %d", c)
	}

	// Paying is idempotent.
	for i := 0; i < 2; i++ {
		if c, b := e.call("POST", "/partners/events/"+ev.ID+"/rsvp", nil, a); c != 200 {
			t.Fatalf("join %d: %d %s", i, c, b)
		}
	}
	if got := e.balance(a); got != 70 {
		t.Errorf("balance after double join: %d", got)
	}
	if c, _ := e.call("POST", "/partners/events/"+ev.ID+"/rsvp", nil, b); c != 402 {
		t.Errorf("broke member joined: %d", c)
	}
	if got := e.balance(b); got != 20 {
		t.Errorf("failed join charged: %d", got)
	}
	e.grantCoins(b, 20)
	if c, _ := e.call("POST", "/partners/events/"+ev.ID+"/rsvp", nil, b); c != 200 {
		t.Fatalf("second join: %d", c)
	}
	// Full.
	if c, _ := e.call("POST", "/partners/events/"+ev.ID+"/rsvp", nil, d); c != 409 {
		t.Errorf("over capacity: %d", c)
	}
	if got := e.balance(d); got != 100 {
		t.Errorf("full event charged: %d", got)
	}
	// Attendance is a count only.
	for _, x := range e.eventsFor(a) {
		if x.ID == ev.ID && (x.Going != 2 || !x.Joined) {
			t.Errorf("event view: %+v", x)
		}
	}
	_, body := e.call("GET", "/partners/events", nil, a)
	if strings.Contains(string(body), e.refOf(b)) {
		t.Error("event listing exposes attendees")
	}

	// Leaving refunds once; joining again is charged again, leaving refunds again.
	if c, _ := e.call("DELETE", "/partners/events/"+ev.ID+"/rsvp", nil, a); c != 204 {
		t.Fatalf("leave: %d", c)
	}
	if got := e.balance(a); got != 100 {
		t.Errorf("refund: %d", got)
	}
	if c, _ := e.call("DELETE", "/partners/events/"+ev.ID+"/rsvp", nil, a); c != 404 {
		t.Errorf("leave twice: %d", c)
	}
	e.call("POST", "/partners/events/"+ev.ID+"/rsvp", nil, a)
	e.call("DELETE", "/partners/events/"+ev.ID+"/rsvp", nil, a)
	if got := e.balance(a); got != 100 {
		t.Errorf("rejoin and leave: %d", got)
	}
	e.call("POST", "/partners/events/"+ev.ID+"/rsvp", nil, a)

	// Mine lists joined events; staff cancelling refunds everyone.
	_, body = e.call("GET", "/partners/events/mine", nil, b)
	if !strings.Contains(string(body), ev.ID) {
		t.Errorf("mine: %s", body)
	}
	admin := e.newUser("admin", true, "Canceller")
	if c, _ := e.callAs("DELETE", "/partners/events/"+ev.ID, nil, admin, "admin"); c != 204 {
		t.Fatalf("cancel: %d", c)
	}
	if e.balance(a) != 100 || e.balance(b) != 40 {
		t.Errorf("refunds after cancel: a=%d b=%d", e.balance(a), e.balance(b))
	}
	if hasEvent(e.eventsFor(a), ev.ID) {
		t.Error("cancelled event still listed")
	}
	// Past events cannot be joined or refunded: simulate by moving the start.
}

func TestHotelMixersAreForGuestsAndVanishWithTheStay(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	hotelID := e.hotel(lat, lng)
	partner := e.makePartner("hotel", hotelID, lat, lng, nil)
	c, mixer := e.makeEvent(partner, "mixer", 5, nil, 0)
	if c != 201 || !mixer.GuestsOnly {
		t.Fatalf("mixer: %d %+v", c, mixer)
	}
	guest := e.guestAt(hotelID, lat, lng, "Guest One")
	outsider := e.user("Outside")
	e.locate(outsider, lat, lng)

	if !hasEvent(e.eventsFor(guest), mixer.ID) {
		t.Fatal("guest does not see the mixer")
	}
	if hasEvent(e.eventsFor(outsider), mixer.ID) {
		t.Error("non-guest sees the mixer")
	}
	if c, _ := e.call("POST", "/partners/events/"+mixer.ID+"/rsvp", nil, outsider); c != 404 {
		t.Errorf("non-guest joined: %d", c)
	}
	if c, b := e.call("POST", "/partners/events/"+mixer.ID+"/rsvp", nil, guest); c != 200 {
		t.Fatalf("guest join: %d %s", c, b)
	}
	// The partner shows up for guests of its hotel even without a fresh location.
	if _, b := e.call("GET", "/partners/nearby", nil, guest); !strings.Contains(string(b), partner) {
		t.Errorf("guest partner list: %s", b)
	}

	// The RSVP hangs off the stay: deleting the stay deletes it.
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM event_rsvps WHERE event_id=$1::uuid AND checkin_id IS NOT NULL`, mixer.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rsvp bound to stay: %d %v", n, err)
	}
	e.exec(`DELETE FROM hotel_checkins WHERE user_id=$1`, guest)
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM event_rsvps WHERE event_id=$1::uuid`, mixer.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("rsvp survived the stay: %d %v", n, err)
	}
}

func TestBookingsCommissionAndReport(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	bps := 1500
	partner := e.makePartner("restaurant", "", lat, lng, &bps)
	user, other := e.user("Diner"), e.user("Other")
	admin := e.newUser("admin", true, "Booker")
	when := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)

	book := func(u string, party int, at string) (int, string) {
		c, b := e.call("POST", "/partners/"+partner+"/bookings", map[string]any{"party_size": party, "requested_for": at, "note": "Window seat"}, u)
		var r struct{ ID string }
		if c == 201 {
			decodeInto(t, b, &r)
		}
		return c, r.ID
	}
	for name, tc := range map[string]struct {
		party int
		at    string
	}{
		"zero party": {0, when},
		"huge party": {13, when},
		"too soon":   {2, time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)},
		"too far":    {2, time.Now().Add(200 * 24 * time.Hour).UTC().Format(time.RFC3339)},
	} {
		if c, _ := book(user, tc.party, tc.at); c != 422 {
			t.Errorf("%s: %d", name, c)
		}
	}
	c, id := book(user, 4, when)
	if c != 201 {
		t.Fatalf("book: %d", c)
	}
	_, mine := e.call("GET", "/partners/bookings/mine", nil, user)
	if !strings.Contains(string(mine), `"status":"requested"`) || strings.Contains(string(mine), "user_ref") {
		t.Errorf("my bookings: %s", mine)
	}
	if _, o := e.call("GET", "/partners/bookings/mine", nil, other); strings.Contains(string(o), id) {
		t.Error("another member sees my booking")
	}
	// At most three open bookings.
	book(user, 2, when)
	book(user, 2, when)
	if c, _ := book(user, 2, when); c != 422 {
		t.Errorf("fourth open booking: %d", c)
	}

	// Staff: confirm with an amount, complete, commission = 15%.
	decide := func(action string, amount *int) (int, []byte) {
		body := map[string]any{"action": action}
		if amount != nil {
			body["amount_cents"] = *amount
		}
		return e.callAs("POST", "/partners/bookings/"+id+"/decision", body, admin, "admin")
	}
	if c, _ := decide("complete", nil); c != 404 {
		t.Errorf("complete before confirm: %d", c)
	}
	amt := 20000
	if c, b := decide("confirm", &amt); c != 200 || !strings.Contains(string(b), `"status":"confirmed"`) || !strings.Contains(string(b), "user_ref") {
		t.Fatalf("confirm: %d %s", c, b)
	}
	if c, _ := decide("confirm", &amt); c != 404 {
		t.Errorf("double confirm: %d", c)
	}
	if c, b := decide("complete", nil); c != 200 {
		t.Fatalf("complete: %d %s", c, b)
	}
	if c, _ := e.call("DELETE", "/partners/bookings/"+id, nil, user); c != 404 {
		t.Errorf("cancelled a completed booking: %d", c)
	}
	c, b := e.callAs("GET", "/partners/"+partner+"/report", nil, admin, "admin")
	if c != 200 || !strings.Contains(string(b), `"bookings_completed":1`) || !strings.Contains(string(b), `"booked_cents":20000`) || !strings.Contains(string(b), `"commission_cents":3000`) {
		t.Errorf("report: %d %s", c, b)
	}
	// A member cancels their own pending booking; others cannot.
	c, id2 := book(other, 2, when)
	if c != 201 {
		t.Fatalf("other booking: %d", c)
	}
	if c, _ := e.call("DELETE", "/partners/bookings/"+id2, nil, user); c != 404 {
		t.Errorf("cancelled someone else's booking: %d", c)
	}
	if c, _ := e.call("DELETE", "/partners/bookings/"+id2, nil, other); c != 204 {
		t.Errorf("own cancel: %d", c)
	}
	// Staff list is by status.
	_, sl := e.callAs("GET", "/partners/bookings?status=completed", nil, admin, "admin")
	if !strings.Contains(string(sl), id) || strings.Contains(string(sl), id2) {
		t.Errorf("staff list: %s", sl)
	}
	_ = fmt.Sprint
}
