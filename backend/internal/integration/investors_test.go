package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"connectme/internal/investors"
)

type boardJSON struct {
	Holders []struct {
		Rank    int    `json:"rank"`
		Name    string `json:"name"`
		Percent string `json:"percent"`
		Founder bool   `json:"founder"`
	} `json:"holders"`
	FounderPct string `json:"founder_percent"`
	PoolPct    string `json:"pool_percent"`
}

func (e *env) ownership() (boardJSON, string) {
	e.t.Helper()
	c, b := e.call("GET", "/investors/public", nil, "")
	if c != 200 {
		e.t.Fatalf("board: %d %s", c, b)
	}
	var out boardJSON
	if err := json.Unmarshal(b, &out); err != nil {
		e.t.Fatal(err)
	}
	return out, string(b)
}

func (e *env) pledge(uid, amount string, show bool) (int, string) {
	c, b := e.call("POST", "/investors/pledges", map[string]any{"amount_usd": amount, "show_name": show, "accept_terms": true}, uid)
	var p struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(b, &p)
	return c, p.ID
}

func (e *env) decide(id, decision string) int {
	c, _ := e.callAs("POST", "/investors/admin/pledges/"+id+"/"+decision, nil, e.user("Owner Admin"), "admin")
	return c
}

func TestInvestorBoardSharesFortyNinePercentByMoneyAndDilutes(t *testing.T) {
	e := setup(t)
	e.exec(`DELETE FROM investments`)
	t.Cleanup(func() { e.exec(`DELETE FROM investments`) }) // leave the shared dev board empty
	a, b, c := e.user("Early Investor"), e.user("Later Investor"), e.user("Quiet Investor")

	// Empty board: the founder alone, 51%.
	bd, _ := e.ownership()
	if len(bd.Holders) != 1 || bd.Holders[0].Name != "Vishal Saini" || bd.Holders[0].Percent != "51" || !bd.Holders[0].Founder || bd.PoolPct != "49" {
		t.Fatalf("empty board: %+v", bd)
	}

	// Validation and permissions.
	if code, _ := e.pledge(a, "0.50", true); code != 422 {
		t.Errorf("under a dollar: %d", code)
	}
	if code, _ := e.pledge(a, "abc", true); code != 422 {
		t.Errorf("not a number: %d", code)
	}
	if code, _ := e.call("POST", "/investors/pledges", map[string]any{"amount_usd": "10", "accept_terms": false}, a); code != 422 {
		t.Errorf("terms not accepted: %d", code)
	}
	if code, _ := e.call("POST", "/investors/pledges", map[string]any{"amount_usd": "10", "accept_terms": true}, ""); code != 401 {
		t.Errorf("signed out: %d", code)
	}

	// A pledge counts for nothing until the owner confirms it.
	_, idA := e.pledge(a, "1000", true)
	if bd, _ := e.ownership(); len(bd.Holders) != 1 {
		t.Errorf("a pending pledge appeared: %+v", bd)
	}
	if code, _ := e.call("POST", "/investors/admin/pledges/"+idA+"/confirm", nil, a); code != 403 {
		t.Errorf("a member confirming their own pledge: %d", code)
	}
	if code := e.decide(idA, "confirm"); code != 204 {
		t.Fatalf("confirm: %d", code)
	}
	if code := e.decide(idA, "confirm"); code != 404 {
		t.Errorf("confirming twice: %d", code)
	}
	// Sole investor owns the whole pool.
	bd, _ = e.ownership()
	if len(bd.Holders) != 2 || bd.Holders[1].Name != "Early Investor" || bd.Holders[1].Percent != "49" {
		t.Fatalf("sole investor: %+v", bd)
	}

	// A second equal pledge dilutes the first to half.
	_, idB := e.pledge(b, "1000", true)
	e.decide(idB, "confirm")
	bd, _ = e.ownership()
	if bd.Holders[1].Percent != "24.5" || bd.Holders[2].Percent != "24.5" {
		t.Fatalf("after second: %+v", bd.Holders)
	}

	// Pricing example: $49,000 fills the pool at $1,000 per 1%; $1 is 0.001%.
	e.exec(`DELETE FROM investments`)
	_, big := e.pledge(a, "48999", true)
	_, tiny := e.pledge(c, "1", false)
	e.decide(big, "confirm")
	e.decide(tiny, "confirm")
	bd, raw := e.ownership()
	if bd.Holders[1].Name != "Early Investor" || bd.Holders[1].Percent != "48.999" || bd.Holders[2].Name != "Anonymous investor" || bd.Holders[2].Percent != "0.001" {
		t.Fatalf("pricing: %+v", bd.Holders)
	}

	// Only names and percentages are public.
	for _, leak := range []string{"amount", "email", "@example.com", "reference", "48999"} {
		if strings.Contains(raw, leak) {
			t.Errorf("public board leaks %q: %s", leak, raw)
		}
	}

	// My own view, cancel, and the staff audit.
	_, idC := e.pledge(c, "20", true)
	_, mb := e.call("GET", "/investors/mine", nil, c)
	if !strings.Contains(string(mb), `"percent":"0.001"`) || !strings.Contains(string(mb), idC) {
		t.Errorf("mine: %s", mb)
	}
	if code, _ := e.call("DELETE", "/investors/pledges/"+idC, nil, c); code != 204 {
		t.Errorf("cancel: %d", code)
	}
	if code, _ := e.call("DELETE", "/investors/pledges/"+idC, nil, c); code != 404 {
		t.Errorf("cancel twice: %d", code)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM staff_audit_log WHERE action='investment_confirm'`).Scan(&n); err != nil || n < 3 {
		t.Errorf("audit entries: %d %v", n, err)
	}
}

// The fixed-price model is the alternative to dilution: $1,000 buys 1% and
// stays 1%, and the 49% sells out instead of shrinking everyone.
func TestInvestorFixedPriceModelNeverDilutesAndSellsOut(t *testing.T) {
	e := setup(t)
	e.exec(`DELETE FROM investments`)
	t.Cleanup(func() { e.exec(`DELETE FROM investments`) })
	ctx := context.Background()
	svc := investors.NewService(e.db, "Vishal Saini", "")
	svc.SetModel(investors.ModeFixed, 100_000)
	staff := e.newUser("admin", true, "Owner")
	a, b, c := e.user("Fixed A"), e.user("Fixed B"), e.user("Fixed C")

	pledge := func(uid, amount string) (string, error) {
		p, err := svc.Pledge(ctx, uid, amount, true, true)
		if err != nil {
			return "", err
		}
		return p.ID, nil
	}
	confirm := func(id string) error { return svc.Decide(ctx, staff, "admin", id, "confirm") }

	ida, err := pledge(a, "1000")
	if err != nil {
		t.Fatal(err)
	}
	if err := confirm(ida); err != nil {
		t.Fatal(err)
	}
	idb, _ := pledge(b, "1000")
	if err := confirm(idb); err != nil {
		t.Fatal(err)
	}
	board, err := svc.Public(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Two investors of $1,000 each hold 1% each: the second did not dilute the first.
	if board.Model != "fixed" || board.PricePerPercent != "1000" || board.OpenPercent != "47" {
		t.Errorf("board: %+v", board)
	}
	if len(board.Holders) != 3 || board.Holders[1].Percent != "1" || board.Holders[2].Percent != "1" {
		t.Errorf("holders: %+v", board.Holders)
	}
	if m, _ := svc.Mine(ctx, a); m.Percent != "1" {
		t.Errorf("first investor now holds %s", m.Percent)
	}

	// A pledge over what is left is refused, and so is a confirmation that would overfill.
	if _, err := pledge(c, "47001"); err == nil {
		t.Error("pledged more than is open")
	}
	idc, err := pledge(c, "47000")
	if err != nil {
		t.Fatal(err)
	}
	// Money that arrives outside the app can't push the pool past 49%: fill it elsewhere first.
	e.exec(`INSERT INTO investments (user_id, amount_cents, status, reference) VALUES ($1::uuid, 100, 'confirmed', 'INV-OVERFLOW')`, a)
	if err := confirm(idc); err != investors.ErrSoldOut {
		t.Errorf("overfilling confirm: %v, want ErrSoldOut", err)
	}
	e.exec(`DELETE FROM investments WHERE reference='INV-OVERFLOW'`)
	if err := confirm(idc); err != nil {
		t.Fatalf("exact fill: %v", err)
	}
	board, _ = svc.Public(ctx)
	if board.OpenPercent != "0" {
		t.Errorf("open after selling out: %s", board.OpenPercent)
	}
	if _, err := pledge(a, "1"); err == nil {
		t.Error("pledged into a full pool")
	}
}
