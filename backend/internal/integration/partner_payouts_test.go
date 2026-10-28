package integration

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// completedPaidBooking makes a confirmed booking, pays it through the webhook
// and completes it, returning the booking id.
func (e *env) completedPaidBooking(partner, user string, cents int64) string {
	e.t.Helper()
	admin := e.newUser("admin", true, "Completer")
	id := e.confirmedBooking(partner, user, int(cents))
	if c := e.bookingPaid(id, user, cents, uniq("pi")); c != 200 {
		e.t.Fatalf("pay: %d", c)
	}
	if c, b := e.callAs("POST", "/partners/bookings/"+id+"/decision", map[string]any{"action": "complete"}, admin, "admin"); c != 200 {
		e.t.Fatalf("complete: %d %s", c, b)
	}
	return id
}

func TestPartnerPayoutsSettleOnceAndNeverTwice(t *testing.T) {
	e := setup(t)
	e.withStripe()
	lat, lng := randomBase()
	bps := 1000 // 10% commission
	partner := e.makePartner("restaurant", "", lat, lng, &bps)
	staff := e.newUser("admin", true, "Treasurer")
	member, manager, stranger := e.user("Diner"), e.user("Manager"), e.user("Nobody")
	if c, b := e.addManager(partner, manager); c != 201 && c != 200 {
		t.Fatalf("manager: %d %s", c, b)
	}
	payouts := "/partners/" + partner + "/payouts"

	// Nothing completed yet: nothing to settle, and only staff may try.
	if c, _ := e.call("POST", payouts, map[string]string{"method": "bank_transfer", "reference": "wire-0"}, member); c != 403 {
		t.Errorf("a member recorded a payout: %d", c)
	}
	if c, b := e.callAs("POST", payouts, map[string]string{"method": "bank_transfer", "reference": "wire-0"}, staff, "admin"); c != 422 || !strings.Contains(string(b), "nothing is owed") {
		t.Errorf("empty settlement: %d %s", c, b)
	}

	a := e.completedPaidBooking(partner, member, 20000)  // owes 18000
	b2 := e.completedPaidBooking(partner, member, 10000) // owes 9000
	_, rep := e.callAs("GET", "/partners/"+partner+"/report", nil, staff, "admin")
	for _, want := range []string{`"owed_to_partner_cents":27000`, `"settled_cents":0`, `"unsettled_cents":27000`} {
		if !strings.Contains(string(rep), want) {
			t.Errorf("report before: want %s in %s", want, rep)
		}
	}

	// Bad input is refused.
	for name, body := range map[string]map[string]string{
		"method":    {"method": "bitcoin", "reference": "wire-1"},
		"reference": {"method": "bank_transfer", "reference": "x"},
		"control":   {"method": "bank_transfer", "reference": "wire\x001"},
		"note":      {"method": "bank_transfer", "reference": "wire-1", "note": strings.Repeat("n", 201)},
	} {
		if c, _ := e.callAs("POST", payouts, body, staff, "admin"); c != 422 {
			t.Errorf("%s accepted: %d", name, c)
		}
	}

	// Two staff members press the button at once: exactly one payout is made.
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], _ = e.callAs("POST", payouts, map[string]string{"method": "bank_transfer", "reference": "wire-" + string(rune('A'+i))}, staff, "admin")
		}(i)
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == 201 {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("concurrent settlements created %d payouts (codes %v)", created, codes)
	}
	var amount, bookings, tagged int
	_ = e.db.QueryRow(context.Background(), `SELECT amount_cents, bookings FROM partner_payouts WHERE partner_id=$1::uuid`, partner).Scan(&amount, &bookings)
	_ = e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM bookings WHERE partner_id=$1::uuid AND payout_id IS NOT NULL`, partner).Scan(&tagged)
	if amount != 27000 || bookings != 2 || tagged != 2 {
		t.Errorf("payout amount=%d bookings=%d tagged=%d", amount, bookings, tagged)
	}

	// Settled money is no longer owed; a later booking is the only thing left.
	if c, _ := e.callAs("POST", payouts, map[string]string{"method": "bank_transfer", "reference": "wire-late"}, staff, "admin"); c != 422 {
		t.Errorf("second settlement with nothing owed: %d", c)
	}
	c3 := e.completedPaidBooking(partner, member, 5000) // owes 4500
	_, rep = e.callAs("GET", "/partners/"+partner+"/report", nil, staff, "admin")
	for _, want := range []string{`"settled_cents":27000`, `"unsettled_cents":4500`} {
		if !strings.Contains(string(rep), want) {
			t.Errorf("report after: want %s in %s", want, rep)
		}
	}
	// A repeated transfer reference is refused even when money is owed.
	var ref string
	_ = e.db.QueryRow(context.Background(), `SELECT reference FROM partner_payouts WHERE partner_id=$1::uuid`, partner).Scan(&ref)
	if c, b := e.callAs("POST", payouts, map[string]string{"method": "bank_transfer", "reference": ref}, staff, "admin"); c != 422 || !strings.Contains(string(b), "already recorded") {
		t.Errorf("duplicate reference: %d %s", c, b)
	}
	if c, b := e.callAs("POST", payouts, map[string]string{"method": "other", "reference": "wire-final", "note": "paid by cheque"}, staff, "admin"); c != 201 || !strings.Contains(string(b), `"amount_cents":4500`) {
		t.Errorf("final settlement: %d %s", c, b)
	}

	// Refunds stop once the money has gone to the partner.
	if c, b := e.callAs("POST", "/payments/bookings/"+a+"/refund", nil, staff, "admin"); c != 422 || !strings.Contains(string(b), "already paid out") {
		t.Errorf("refund after payout: %d %s", c, b)
	}
	_, _ = b2, c3

	// The partner's own manager can read the history; strangers and members cannot.
	if c, b := e.call("GET", "/partners/manage/partners/"+partner+"/payouts", nil, manager); c != 200 || strings.Count(string(b), `"reference"`) != 2 || strings.Contains(string(b), "created_by") {
		t.Errorf("manager view: %d %s", c, b)
	}
	if c, _ := e.call("GET", "/partners/manage/partners/"+partner+"/payouts", nil, stranger); c != 404 {
		t.Errorf("stranger saw payouts: %d", c)
	}
	if c, _ := e.call("GET", payouts, nil, manager); c != 403 {
		t.Errorf("manager used the staff list: %d", c)
	}
	if c, b := e.callAs("GET", payouts, nil, staff, "admin"); c != 200 || strings.Count(string(b), `"reference"`) != 2 {
		t.Errorf("staff list: %d %s", c, b)
	}
	if c, _ := e.callAs("POST", "/partners/00000000-0000-0000-0000-000000000000/payouts", map[string]string{"method": "other", "reference": "ghost-1"}, staff, "admin"); c != 404 {
		t.Errorf("unknown partner: %d", c)
	}
}
