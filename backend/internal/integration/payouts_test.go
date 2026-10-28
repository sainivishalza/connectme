package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

type earningsJSON struct {
	Available  int  `json:"available_coins"`
	Requested  int  `json:"requested_coins"`
	Paid       int  `json:"paid_coins"`
	CanRequest bool `json:"can_request"`
	NeedsID    string
	Payouts    []struct {
		ID, Status, Reference string
		Coins                 int
		AmountCents           int `json:"amount_cents"`
	} `json:"payouts"`
}

func (e *env) earnings(uid string) earningsJSON {
	e.t.Helper()
	c, b := e.call("GET", "/payouts/mine", nil, uid)
	if c != 200 {
		e.t.Fatalf("earnings: %d %s", c, b)
	}
	var out earningsJSON
	if err := json.Unmarshal(b, &out); err != nil {
		e.t.Fatal(err)
	}
	return out
}

// Gifts a host earned can be paid out once: asking stamps them, a rejection
// frees them, a recorded transfer settles them, and nothing is paid twice.
func TestHostPayoutsStampGiftsPayOnceAndRejectionFreesThem(t *testing.T) {
	e := setup(t)
	host, fan := e.user("Paid Host"), e.user("Generous Fan")
	room := e.goLive(host, "Earning show")
	e.exec(`UPDATE users SET id_verified_at = NULL WHERE id=$1::uuid`, host)
	// Give the host a ledger large enough to request: plain inserts keep the test independent of gift prices.
	e.exec(`INSERT INTO live_gifts (room_id, from_user, gift_id, coins, host_coins, platform_coins, request_id)
	        SELECT $1::uuid, $2::uuid, g.id, 6000, 4200, 1800, 'payout-seed-1' FROM gift_catalog g LIMIT 1`, room.Room.ID, fan)

	if er := e.earnings(host); er.Available != 4200 || er.CanRequest {
		t.Fatalf("below the minimum: %+v", er)
	}
	e.exec(`INSERT INTO live_gifts (room_id, from_user, gift_id, coins, host_coins, platform_coins, request_id)
	        SELECT $1::uuid, $2::uuid, g.id, 2000, 1400, 600, 'payout-seed-2' FROM gift_catalog g LIMIT 1`, room.Room.ID, fan)
	er := e.earnings(host)
	if er.Available != 5600 || er.CanRequest {
		t.Fatalf("enough coins but no verified ID must not allow a request: %+v", er)
	}
	if c, _ := e.call("POST", "/payouts/request", nil, host); c != 422 {
		t.Errorf("request without ID verification: %d", c)
	}

	e.exec(`UPDATE users SET id_verified_at = NOW() WHERE id=$1::uuid`, host)
	if er := e.earnings(host); !er.CanRequest {
		t.Fatalf("should be able to request: %+v", er)
	}
	c, body := e.call("POST", "/payouts/request", nil, host)
	if c != 201 {
		t.Fatalf("request: %d %s", c, body)
	}
	var p struct {
		ID    string
		Coins int
	}
	_ = json.Unmarshal(body, &p)
	if p.Coins != 5600 {
		t.Errorf("payout coins: %d", p.Coins)
	}
	if c, _ := e.call("POST", "/payouts/request", nil, host); c != 422 {
		t.Errorf("second open request: %d", c)
	}
	if er := e.earnings(host); er.Available != 0 || er.Requested != 5600 || er.CanRequest {
		t.Errorf("after requesting: %+v", er)
	}
	if n := e.count(`SELECT COUNT(*) FROM live_gifts WHERE payout_id=$1::uuid`, p.ID); n != 2 {
		t.Errorf("stamped gifts: %d", n)
	}

	// Only an admin sees or decides the queue.
	other := e.user("Nosy Member")
	mod := e.newUser("moderator", true, "Mod")
	admin := e.newUser("admin", true, "Boss")
	for _, who := range []struct{ id, role string }{{other, "user"}, {mod, "moderator"}} {
		if c, _ := e.callAs("GET", "/payouts/admin/queue", nil, who.id, who.role); c != 403 {
			t.Errorf("%s read the payout queue: %d", who.role, c)
		}
		if c, _ := e.callAs("POST", "/payouts/admin/"+p.ID+"/pay", map[string]any{"amount_cents": 5600, "method": "upi", "reference": "UPI123"}, who.id, who.role); c != 403 {
			t.Errorf("%s paid a payout: %d", who.role, c)
		}
	}
	if _, q := e.callAs("GET", "/payouts/admin/queue", nil, admin, "admin"); !strings.Contains(string(q), p.ID) || !strings.Contains(string(q), e.refOf(host)) {
		t.Errorf("queue misses the request: %s", q)
	}

	// Rejecting frees the gifts and allows a new request.
	if c, _ := e.callAs("POST", "/payouts/admin/"+p.ID+"/reject", map[string]any{"reason": "bank details missing"}, admin, "admin"); c != 204 {
		t.Fatalf("reject: %d", c)
	}
	if er := e.earnings(host); er.Available != 5600 || er.Requested != 0 || !er.CanRequest {
		t.Errorf("after rejection: %+v", er)
	}
	if c, _ := e.callAs("POST", "/payouts/admin/"+p.ID+"/reject", map[string]any{"reason": "again please"}, admin, "admin"); c != 404 {
		t.Errorf("rejecting twice: %d", c)
	}

	// Request again and pay: validation first, then once only, and the reference is unique.
	_, body = e.call("POST", "/payouts/request", nil, host)
	_ = json.Unmarshal(body, &p)
	for name, bad := range map[string]map[string]any{
		"no amount":  {"amount_cents": 0, "method": "upi", "reference": "UPI999"},
		"bad method": {"amount_cents": 5600, "method": "cash", "reference": "UPI999"},
		"no ref":     {"amount_cents": 5600, "method": "upi", "reference": ""},
	} {
		if c, _ := e.callAs("POST", "/payouts/admin/"+p.ID+"/pay", bad, admin, "admin"); c != 422 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if c, b := e.callAs("POST", "/payouts/admin/"+p.ID+"/pay", map[string]any{"amount_cents": 5600, "method": "upi", "reference": "UPI-" + p.ID[:8], "note": "March"}, admin, "admin"); c != 204 {
		t.Fatalf("pay: %d %s", c, b)
	}
	if c, _ := e.callAs("POST", "/payouts/admin/"+p.ID+"/pay", map[string]any{"amount_cents": 5600, "method": "upi", "reference": "UPI-again"}, admin, "admin"); c != 404 {
		t.Errorf("paying twice: %d", c)
	}
	er = e.earnings(host)
	if er.Paid != 5600 || er.Available != 0 || len(er.Payouts) != 2 || er.Payouts[0].Status != "paid" || er.Payouts[0].AmountCents != 5600 {
		t.Errorf("after paying: %+v", er)
	}
	// A transfer reference can be recorded once across all hosts.
	host2 := e.user("Second Host")
	room2 := e.goLive(host2, "Second show")
	e.exec(`UPDATE users SET id_verified_at = NOW() WHERE id=$1::uuid`, host2)
	e.exec(`INSERT INTO live_gifts (room_id, from_user, gift_id, coins, host_coins, platform_coins, request_id)
	        SELECT $1::uuid, $2::uuid, g.id, 8000, 5600, 2400, 'payout-seed-3' FROM gift_catalog g LIMIT 1`, room2.Room.ID, fan)
	_, b2 := e.call("POST", "/payouts/request", nil, host2)
	var p2 struct{ ID string }
	_ = json.Unmarshal(b2, &p2)
	if c, _ := e.callAs("POST", "/payouts/admin/"+p2.ID+"/pay", map[string]any{"amount_cents": 5600, "method": "upi", "reference": "UPI-" + p.ID[:8]}, admin, "admin"); c != 422 {
		t.Errorf("reused transfer reference: %d", c)
	}
	// Nothing about the payout reaches other members' view of the host.
	if _, pub := e.call("GET", "/users/"+e.refOf(host), nil, other); strings.Contains(string(pub), "UPI-") || strings.Contains(string(pub), "payout") {
		t.Errorf("payout data in a public profile: %s", pub)
	}
	if n := e.count(`SELECT COUNT(*) FROM staff_audit_log WHERE action IN ('host_payout_paid','host_payout_rejected') AND actor_id=$1::uuid`, admin); n != 2 {
		t.Errorf("audit entries: %d", n)
	}
}
