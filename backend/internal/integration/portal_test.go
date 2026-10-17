package integration

import (
	"connectme/internal/portal"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (e *env) emailOf(uid string) string {
	e.t.Helper()
	var s string
	if err := e.db.QueryRow(context.Background(), `SELECT email FROM users WHERE id=$1`, uid).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *env) addManager(partner, uid string) (int, string) {
	e.t.Helper()
	admin := e.newUser("admin", true, "Linker")
	c, b := e.callAs("POST", "/partners/"+partner+"/managers", map[string]string{"email": e.emailOf(uid)}, admin, "admin")
	return c, string(b)
}

func TestPartnerManagersAreScopedToTheirOwnPartner(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	bps := 1000
	cafe := e.makePartner("restaurant", "", lat, lng, &bps)
	spa := e.makePartner("activity", "", lat, lng, &bps)
	manager, rival, plain := e.user("Cafe Owner"), e.user("Spa Owner"), e.user("Plain Member")
	unverified := e.newUser("user", false, "Unverified Owner")
	admin := e.newUser("admin", true, "Overseer")

	// Only admins link managers, and only verified members qualify.
	if c, _ := e.call("POST", "/partners/"+cafe+"/managers", map[string]string{"email": e.emailOf(manager)}, manager); c != 403 {
		t.Errorf("member linked themselves: %d", c)
	}
	if c, _ := e.addManager(cafe, unverified); c != 422 {
		t.Errorf("unverified manager: %d", c)
	}
	if c, _ := e.callAs("POST", "/partners/"+cafe+"/managers", map[string]string{"email": "nobody@example.com"}, admin, "admin"); c != 422 {
		t.Errorf("unknown email: %d", c)
	}
	c, body := e.addManager(cafe, manager)
	if c != 201 || strings.Contains(body, "@") {
		t.Fatalf("add manager: %d %s", c, body)
	}
	e.addManager(spa, rival)
	_, ml := e.callAs("GET", "/partners/"+cafe+"/managers", nil, admin, "admin")
	if !strings.Contains(string(ml), e.refOf(manager)) || strings.Contains(string(ml), "@") {
		t.Errorf("manager list: %s", ml)
	}

	// Each manager sees only their own partner.
	_, mine := e.call("GET", "/partners/manage/partners", nil, manager)
	if !strings.Contains(string(mine), cafe) || strings.Contains(string(mine), spa) {
		t.Errorf("managed partners: %s", mine)
	}
	if _, none := e.call("GET", "/partners/manage/partners", nil, plain); strings.Contains(string(none), cafe) {
		t.Errorf("a plain member manages a partner: %s", none)
	}
	for _, path := range []string{"/events", "/bookings", "/report"} {
		if c, _ := e.call("GET", "/partners/manage/partners/"+spa+path, nil, manager); c != 404 {
			t.Errorf("manager read the other partner's %s: %d", path, c)
		}
		if c, _ := e.call("GET", "/partners/manage/partners/"+cafe+path, nil, plain); c != 404 {
			t.Errorf("plain member read %s: %d", path, c)
		}
	}

	// Events: create, list with takings, cancel; never another partner's.
	ev := func(partner, title string, hours int) (int, string) {
		c, b := e.call("POST", "/partners/manage/partners/"+partner+"/events", map[string]any{
			"kind": "hangout", "title": title, "description": "Open to all",
			"starts_at": time.Now().Add(time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339),
			"ends_at":   time.Now().Add(time.Duration(hours+2) * time.Hour).UTC().Format(time.RFC3339), "price_coins": 10}, manager)
		return c, string(b)
	}
	if c, _ := ev(spa, "Not mine", 48); c != 404 {
		t.Errorf("created an event for another partner: %d", c)
	}
	c, b := ev(cafe, "Tasting night", 48)
	if c != 201 {
		t.Fatalf("create event: %d %s", c, b)
	}
	eventID := between(b, `"id":"`, `"`)
	fan := e.user("Event Fan")
	e.locate(fan, lat, lng)
	e.grantCoins(fan, 50)
	if c, _ := e.call("POST", "/partners/events/"+eventID+"/rsvp", nil, fan); c != 200 {
		t.Fatalf("rsvp: %d", c)
	}
	_, list := e.call("GET", "/partners/manage/partners/"+cafe+"/events", nil, manager)
	if !strings.Contains(string(list), `"going":1`) || !strings.Contains(string(list), `"coins_earned":10`) || strings.Contains(string(list), e.refOf(fan)) {
		t.Errorf("event list: %s", list)
	}
	if c, _ := e.call("DELETE", "/partners/manage/events/"+eventID, nil, rival); c != 404 {
		t.Errorf("rival cancelled my event: %d", c)
	}
	if c, _ := e.call("DELETE", "/partners/manage/events/"+eventID, nil, manager); c != 204 {
		t.Fatalf("cancel: %d", c)
	}
	if e.balance(fan) != 50 {
		t.Errorf("tickets not refunded on cancel: %d", e.balance(fan))
	}
	// A partner cannot schedule more than 20 upcoming events.
	for i := 0; i < 20; i++ {
		if c, b := ev(cafe, "Weekly "+strings.Repeat("x", i%3+3), 24+i); c != 201 {
			t.Fatalf("event %d: %d %s", i, c, b)
		}
	}
	if c, _ := ev(cafe, "Twenty-first", 100); c != 422 {
		t.Errorf("21st upcoming event: %d", c)
	}

	// Removing a manager (or banning them) ends their access.
	if c, _ := e.callAs("DELETE", "/partners/"+cafe+"/managers/"+e.refOf(manager), nil, admin, "admin"); c != 204 {
		t.Fatalf("remove: %d", c)
	}
	if c, _ := e.call("GET", "/partners/manage/partners/"+cafe+"/events", nil, manager); c != 404 {
		t.Errorf("removed manager still has access: %d", c)
	}
	e.addManager(cafe, manager)
	e.exec(`UPDATE users SET is_banned=true WHERE id=$1`, manager)
	if c, _ := e.call("GET", "/partners/manage/partners/"+cafe+"/bookings", nil, manager); c != 404 && c != 401 && c != 403 {
		t.Errorf("banned manager: %d", c)
	}
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	j := strings.Index(s, b)
	if j < 0 {
		return ""
	}
	return s[:j]
}

func TestPartnerManagersSeeGuestsWithoutIdentity(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	bps := 1500
	venue := e.makePartner("restaurant", "", lat, lng, &bps)
	other := e.makePartner("restaurant", "", lat, lng, &bps)
	manager := e.user("Maitre D")
	e.addManager(venue, manager)
	openGuest, shyGuest := e.user("Olivia Open"), e.user("Sam Shy")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shyGuest)

	open1 := e.confirmedRequest(venue, openGuest)
	shy1 := e.confirmedRequest(venue, shyGuest)
	foreign := e.confirmedRequest(other, e.user("Elsewhere"))

	_, b := e.call("GET", "/partners/manage/partners/"+venue+"/bookings", nil, manager)
	s := string(b)
	if !strings.Contains(s, `"guest":"Olivia Open"`) || strings.Contains(s, "Sam Shy") || !strings.Contains(s, `"guest":"Guest `+strings.ToUpper(shy1[:4])+`"`) {
		t.Errorf("guest labels: %s", s)
	}
	for _, leak := range []string{"user_ref", e.refOf(openGuest), e.refOf(shyGuest), "@example.com"} {
		if strings.Contains(s, leak) {
			t.Errorf("manager view leaks %q", leak)
		}
	}
	if strings.Contains(s, foreign) {
		t.Error("manager sees another partner's booking")
	}

	// Decide: confirm with a price, complete; wrong partner and plain member get nothing.
	decide := func(who, id, action string, amount *int) (int, string) {
		body := map[string]any{"action": action}
		if amount != nil {
			body["amount_cents"] = *amount
		}
		c, r := e.call("POST", "/partners/manage/bookings/"+id+"/decision", body, who)
		return c, string(r)
	}
	amt := 12000
	if c, _ := decide(openGuest, open1, "confirm", &amt); c != 404 {
		t.Errorf("guest decided their own booking: %d", c)
	}
	if c, _ := decide(manager, foreign, "confirm", &amt); c != 404 {
		t.Errorf("manager decided another partner's booking: %d", c)
	}
	c, r := decide(manager, open1, "confirm", &amt)
	if c != 200 || !strings.Contains(r, `"status":"confirmed"`) || strings.Contains(r, "user_ref") {
		t.Fatalf("confirm: %d %s", c, r)
	}
	if c, _ := decide(manager, open1, "complete", nil); c != 200 {
		t.Errorf("complete: %d", c)
	}
	if c, _ := decide(manager, shy1, "decline", nil); c != 200 {
		t.Errorf("decline: %d", c)
	}
	_, rep := e.call("GET", "/partners/manage/partners/"+venue+"/report", nil, manager)
	for _, want := range []string{`"bookings_completed":1`, `"commission_cents":1800`, `"booked_cents":12000`} {
		if !strings.Contains(string(rep), want) {
			t.Errorf("report lacks %s: %s", want, rep)
		}
	}
	_, onlyDeclined := e.call("GET", "/partners/manage/partners/"+venue+"/bookings?status=declined", nil, manager)
	if !strings.Contains(string(onlyDeclined), shy1) || strings.Contains(string(onlyDeclined), open1) {
		t.Errorf("status filter: %s", onlyDeclined)
	}
	// The member's own view still hides the guest label and has no manager data.
	_, mine := e.call("GET", "/partners/bookings/mine", nil, openGuest)
	if strings.Contains(string(mine), `"guest"`) {
		t.Errorf("member view carries a guest label: %s", mine)
	}
}

// confirmedRequest makes a (still unconfirmed) booking and returns its id.
func (e *env) confirmedRequest(partner, user string) string {
	e.t.Helper()
	c, b := e.call("POST", "/partners/"+partner+"/bookings", map[string]any{
		"party_size": 3, "requested_for": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)}, user)
	if c != 201 {
		e.t.Fatalf("book: %d %s", c, b)
	}
	return between(string(b), `"id":"`, `"`)
}

func TestPartnerPortalServesOnlyKnownAssetsWithStrictCSPAndNoInnerHTML(t *testing.T) {
	h := portal.Handler()
	for path, want := range map[string]int{
		"/partner/": 200, "/partner": 200, "/partner/app.js": 200, "/partner/style.css": 200,
		"/partner/../go.mod": 404, "/partner/secret": 404, "/partner/web/": 404,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("GET %s: %d, want %d", path, rec.Code, want)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/partner/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "http:") {
		t.Errorf("weak CSP: %q", csp)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/partner/", nil))
	if rec.Code != 405 {
		t.Errorf("POST allowed: %d", rec.Code)
	}
	// Member text must only ever reach the page through textContent.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/partner/app.js", nil))
	js := rec.Body.String()
	for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function"} {
		if strings.Contains(js, bad) {
			t.Errorf("portal script uses %s", bad)
		}
	}
}
