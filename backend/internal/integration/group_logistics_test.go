package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// twoMatchedDoubles returns the four members, the two group ids and a
// scheduled date between them.
func (e *env) matchedDoublesWithDate(t *testing.T) (a1, a2, b1, b2, dateID string) {
	t.Helper()
	lat, lng := randomBase()
	a1, a2, ga := e.double(lat, lng, "dinner")
	b1, b2, gb := e.double(lat+0.001, lng+0.001, "dinner")
	e.groupMatch(ga, gb, a1)
	e.groupMatch(gb, ga, b1)
	e.groupMatch(ga, gb, a2)
	_, r := e.groupMatch(gb, ga, b2)
	if !r.Matched {
		t.Fatal("groups did not match")
	}
	start := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	c, body := e.call("POST", "/groups/"+ga+"/schedule", map[string]any{"group_match_id": r.GroupMatchID, "venue": "Bistro", "starts_at": start}, a1)
	if c != 201 {
		t.Fatalf("schedule: %d %s", c, body)
	}
	return a1, a2, b1, b2, between(string(body), `"id":"`, `"`)
}

func TestGroupDateLogisticsAreAPaidPerPersonPlan(t *testing.T) {
	e := setup(t)
	a1, a2, b1, _, date := e.matchedDoublesWithDate(t)
	stranger := e.user("Not In The Groups")

	// Everyone in either group sees the price; the plan is hidden until paid.
	for _, u := range []string{a1, b1} {
		c, b := e.call("GET", "/groups/dates/"+date+"/logistics", nil, u)
		if c != 200 || !strings.Contains(string(b), `"price_coins":100`) || !strings.Contains(string(b), `"paid":false`) || strings.Contains(string(b), `"plan"`) {
			t.Fatalf("before paying: %d %s", c, b)
		}
	}
	if c, _ := e.call("GET", "/groups/dates/"+date+"/logistics", nil, stranger); c != 404 {
		t.Errorf("a stranger saw the logistics: %d", c)
	}
	if c, _ := e.call("POST", "/groups/dates/"+date+"/logistics", nil, stranger); c != 404 {
		t.Errorf("a stranger paid: %d", c)
	}

	// Without coins: payment required, nothing charged, no plan leaks.
	c, b := e.call("POST", "/groups/dates/"+date+"/logistics", nil, a1)
	if c != 402 || strings.Contains(string(b), `"plan"`) {
		t.Fatalf("broke member: %d %s", c, b)
	}
	if e.balance(a1) != 0 {
		t.Errorf("failed payment charged: %d", e.balance(a1))
	}

	// Paying unlocks the plan; paying again does not charge twice.
	e.grantCoins(a1, 250)
	for i := 0; i < 2; i++ {
		c, b = e.call("POST", "/groups/dates/"+date+"/logistics", nil, a1)
		if c != 200 || !strings.Contains(string(b), `"paid":true`) || !strings.Contains(string(b), `"venues"`) || !strings.Contains(string(b), `"icebreakers"`) {
			t.Fatalf("pay %d: %d %s", i, c, b)
		}
	}
	if got := e.balance(a1); got != 150 {
		t.Errorf("charged %d, want exactly one fee (balance 150)", 250-got)
	}
	// Others still have to pay for themselves, and the plan is shared.
	_, theirs := e.call("GET", "/groups/dates/"+date+"/logistics", nil, a2)
	if !strings.Contains(string(theirs), `"paid":false`) || strings.Contains(string(theirs), `"venues"`) {
		t.Errorf("a teammate got the plan for free: %s", theirs)
	}
	e.grantCoins(b1, 100)
	_, b = e.call("POST", "/groups/dates/"+date+"/logistics", nil, b1)
	_, a1plan := e.call("GET", "/groups/dates/"+date+"/logistics", nil, a1)
	if between(string(b), `"venues":[`, `]`) != between(string(a1plan), `"venues":[`, `]`) {
		t.Errorf("payers see different plans:\n%s\n%s", b, a1plan)
	}
	var plans int
	_ = e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM group_date_plans WHERE date_id=$1::uuid`, date).Scan(&plans)
	if plans != 1 {
		t.Errorf("plan generated %d times", plans)
	}
	var ledger int
	_ = e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM coin_ledger WHERE reason='group_logistics' AND ref LIKE $1`, "logistics:"+date+":%").Scan(&ledger)
	if ledger != 2 {
		t.Errorf("ledger entries: %d", ledger)
	}
}

func TestCancellingTheDateRefundsEveryonePaid(t *testing.T) {
	e := setup(t)
	a1, _, b1, b2, date := e.matchedDoublesWithDate(t)
	e.grantCoins(a1, 100)
	e.grantCoins(b1, 100)
	e.call("POST", "/groups/dates/"+date+"/logistics", nil, a1)
	e.call("POST", "/groups/dates/"+date+"/logistics", nil, b1)
	if e.balance(a1) != 0 || e.balance(b1) != 0 {
		t.Fatalf("setup: %d %d", e.balance(a1), e.balance(b1))
	}
	if c, _ := e.call("DELETE", "/groups/dates/"+date, nil, b2); c != 204 {
		t.Fatalf("cancel: %d", c)
	}
	if e.balance(a1) != 100 || e.balance(b1) != 100 {
		t.Errorf("refunds: %d %d", e.balance(a1), e.balance(b1))
	}
	if c, _ := e.call("GET", "/groups/dates/"+date+"/logistics", nil, a1); c != 404 {
		t.Errorf("logistics of a cancelled date: %d", c)
	}
	if c, _ := e.call("POST", "/groups/dates/"+date+"/logistics", nil, a1); c != 404 {
		t.Errorf("paying for a cancelled date: %d", c)
	}
}

func TestLogisticsCannotBePaidAfterTheDateStarted(t *testing.T) {
	e := setup(t)
	a1, _, _, _, date := e.matchedDoublesWithDate(t)
	e.grantCoins(a1, 100)
	e.exec(`UPDATE group_dates SET starts_at = NOW() - INTERVAL '1 hour' WHERE id=$1::uuid`, date)
	if c, _ := e.call("POST", "/groups/dates/"+date+"/logistics", nil, a1); c != 422 {
		t.Errorf("paying after the start: %d", c)
	}
	if e.balance(a1) != 100 {
		t.Errorf("charged for a started date: %d", e.balance(a1))
	}
}
