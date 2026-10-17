package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// confirmedBooking makes a booking and has staff confirm it for the amount.
func (e *env) confirmedBooking(partner, user string, cents int) string {
	e.t.Helper()
	admin := e.newUser("admin", true, "PayAdmin")
	c, b := e.call("POST", "/partners/"+partner+"/bookings", map[string]any{
		"party_size": 2, "requested_for": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)}, user)
	if c != 201 {
		e.t.Fatalf("book: %d %s", c, b)
	}
	var r struct{ ID string }
	decodeInto(e.t, b, &r)
	if c, b := e.callAs("POST", "/partners/bookings/"+r.ID+"/decision", map[string]any{"action": "confirm", "amount_cents": cents}, admin, "admin"); c != 200 {
		e.t.Fatalf("confirm: %d %s", c, b)
	}
	return r.ID
}

func (e *env) bookingPaid(id string, user string, cents int64, intent string) int {
	return e.event(uniq("evt"), "checkout.session.completed", map[string]any{
		"id": uniq("cs_booking"), "mode": "payment", "payment_status": "paid", "payment_intent": intent,
		"amount_total": cents, "currency": "usd",
		"metadata": map[string]string{"user_id": user, "booking_id": id},
	})
}

func (e *env) payStatus(id string) string {
	e.t.Helper()
	var s string
	if err := e.db.QueryRow(context.Background(), `SELECT payment_status FROM bookings WHERE id=$1::uuid`, id).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func TestBookingPaymentsTrustTheDatabaseNotTheWebhook(t *testing.T) {
	e := setup(t)
	st := e.withStripe()
	lat, lng := randomBase()
	bps := 1500
	partner := e.makePartner("restaurant", "", lat, lng, &bps)
	user, other := e.user("Payer"), e.user("Bystander")
	admin := e.newUser("admin", true, "Closer")

	// An unconfirmed booking cannot be paid.
	c, b := e.call("POST", "/partners/"+partner+"/bookings", map[string]any{"party_size": 2, "requested_for": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)}, user)
	var pending struct{ ID string }
	decodeInto(t, b, &pending)
	if c != 201 {
		t.Fatalf("book: %d", c)
	}
	if c, _ := e.call("POST", "/payments/bookings/"+pending.ID+"/checkout", nil, user); c != 422 {
		t.Errorf("paying an unconfirmed booking: %d", c)
	}

	id := e.confirmedBooking(partner, user, 20000)
	if c, _ := e.call("POST", "/payments/bookings/"+id+"/checkout", nil, other); c != 404 {
		t.Errorf("someone else's booking: %d", c)
	}
	c, b = e.call("POST", "/payments/bookings/"+id+"/checkout", nil, user)
	if c != 201 {
		t.Fatalf("checkout: %d %s", c, b)
	}
	f := st.last()
	if f.Get("line_items[0][price_data][unit_amount]") != "20000" || f.Get("mode") != "payment" ||
		f.Get("metadata[booking_id]") != id || f.Get("metadata[user_id]") != user || f.Get("success_url") != "connectme://ok" ||
		!strings.Contains(f.Get("line_items[0][price_data][product_data][name]"), "Booking:") {
		t.Errorf("checkout params: %v", f)
	}

	// Payments that do not match the booking change nothing.
	if c := e.bookingPaid(id, user, 100, uniq("pi")); c != 200 || e.payStatus(id) != "unpaid" {
		t.Errorf("wrong amount accepted: %d %s", c, e.payStatus(id))
	}
	if c := e.bookingPaid(id, other, 20000, uniq("pi")); c != 200 || e.payStatus(id) != "unpaid" {
		t.Errorf("wrong payer accepted: %d %s", c, e.payStatus(id))
	}
	if c := e.bookingPaid("00000000-0000-0000-0000-000000000000", user, 20000, uniq("pi")); c != 200 {
		t.Errorf("unknown booking must still be acknowledged: %d", c)
	}

	intent := uniq("pi_booking")
	if c := e.bookingPaid(id, user, 20000, intent); c != 200 || e.payStatus(id) != "paid" {
		t.Fatalf("payment: %d %s", c, e.payStatus(id))
	}
	// Replays and a second session do not double count.
	e.bookingPaid(id, user, 20000, uniq("pi_again"))
	var rows, paid int
	_ = e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM payments WHERE user_id=$1 AND type='booking'`, user).Scan(&rows)
	_ = e.db.QueryRow(context.Background(), `SELECT paid_cents FROM bookings WHERE id=$1::uuid`, id).Scan(&paid)
	if rows != 1 || paid != 20000 {
		t.Errorf("payment rows=%d paid=%d", rows, paid)
	}
	if c, _ := e.call("POST", "/payments/bookings/"+id+"/checkout", nil, user); c != 422 {
		t.Errorf("paying twice: %d", c)
	}

	// Staff cannot change the price of a paid booking; completing keeps it.
	if c, _ := e.callAs("POST", "/partners/bookings/"+id+"/decision", map[string]any{"action": "complete", "amount_cents": 1}, admin, "admin"); c != 422 {
		t.Errorf("changed a paid amount: %d", c)
	}
	if c, b := e.callAs("POST", "/partners/bookings/"+id+"/decision", map[string]any{"action": "complete"}, admin, "admin"); c != 200 || !strings.Contains(string(b), `"payment_status":"paid"`) {
		t.Fatalf("complete: %d %s", c, b)
	}
	_, rep := e.callAs("GET", "/partners/"+partner+"/report", nil, admin, "admin")
	for _, want := range []string{`"collected_cents":20000`, `"commission_cents":3000`, `"owed_to_partner_cents":17000`, `"refunds_due":0`} {
		if !strings.Contains(string(rep), want) {
			t.Errorf("report lacks %s: %s", want, rep)
		}
	}
}

func TestBookingRefundsAreRecordedOnlyWhenStripeConfirms(t *testing.T) {
	e := setup(t)
	st := e.withStripe()
	lat, lng := randomBase()
	partner := e.makePartner("activity", "", lat, lng, nil)
	user := e.user("Refunded")
	admin := e.newUser("admin", true, "Refunder")

	id := e.confirmedBooking(partner, user, 5000)
	if c, _ := e.callAs("POST", "/payments/bookings/"+id+"/refund", nil, admin, "admin"); c != 422 {
		t.Errorf("refunding an unpaid booking: %d", c)
	}
	intent := uniq("pi_refund")
	e.bookingPaid(id, user, 5000, intent)

	// The member cancels after paying: the platform now owes a refund.
	if c, _ := e.call("DELETE", "/partners/bookings/"+id, nil, user); c != 204 {
		t.Fatalf("cancel: %d", c)
	}
	_, rep := e.callAs("GET", "/partners/"+partner+"/report", nil, admin, "admin")
	if !strings.Contains(string(rep), `"refunds_due":1`) {
		t.Errorf("refund not flagged: %s", rep)
	}
	if c, _ := e.call("POST", "/payments/bookings/"+id+"/refund", nil, user); c != 403 {
		t.Errorf("member refunded: %d", c)
	}
	if c, b := e.callAs("POST", "/payments/bookings/"+id+"/refund", nil, admin, "admin"); c != 202 {
		t.Fatalf("refund: %d %s", c, b)
	}
	if len(st.refunds) != 1 || st.refunds[0] != intent {
		t.Errorf("stripe refunds: %v", st.refunds)
	}
	// Not recorded until Stripe says so.
	if e.payStatus(id) != "paid" {
		t.Errorf("marked refunded before confirmation: %s", e.payStatus(id))
	}
	if c := e.event(uniq("evt"), "charge.refunded", map[string]any{"id": uniq("ch"), "payment_intent": intent, "amount": 5000, "amount_refunded": 5000}); c != 200 {
		t.Fatalf("refund event: %d", c)
	}
	if e.payStatus(id) != "refunded" {
		t.Errorf("after confirmation: %s", e.payStatus(id))
	}
	var status string
	_ = e.db.QueryRow(context.Background(), `SELECT status FROM payments WHERE stripe_id=$1`, intent).Scan(&status)
	if status != "refunded" {
		t.Errorf("payment row: %s", status)
	}
	_, rep = e.callAs("GET", "/partners/"+partner+"/report", nil, admin, "admin")
	if !strings.Contains(string(rep), `"refunds_due":0`) || !strings.Contains(string(rep), `"collected_cents":0`) {
		t.Errorf("report after refund: %s", rep)
	}
}
