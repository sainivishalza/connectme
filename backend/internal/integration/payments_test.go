package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"connectme/internal/payments"
)

func TestCheckoutUsesServerSidePrices(t *testing.T) {
	e, st := needPay(t)
	u := e.user("Buyer")

	// Clients choose a catalog id; they cannot send an amount or a return URL.
	for name, body := range map[string]map[string]any{
		"amount smuggled": {"pack_id": "starter", "amount": 1},
		"redirect":        {"pack_id": "starter", "success_url": "https://evil.example"},
		"unknown pack":    {"pack_id": "free_money"},
	} {
		if c, _ := e.call("POST", "/payments/coins", body, u); c != 400 && c != 422 {
			t.Errorf("%s accepted: %d", name, c)
		}
	}
	c, b := e.call("POST", "/payments/coins", map[string]any{"pack_id": "medium"}, u)
	if c != 201 || !strings.Contains(string(b), "https://checkout.stripe.test/") {
		t.Fatalf("coins checkout: %d %s", c, b)
	}
	f := st.last()
	if f.Get("mode") != "payment" || f.Get("line_items[0][price_data][unit_amount]") != "999" ||
		f.Get("metadata[user_id]") != u || f.Get("metadata[pack_id]") != "medium" ||
		f.Get("success_url") != "connectme://ok" || f.Get("client_reference_id") != u {
		t.Fatalf("session params: %v", f)
	}

	c, _ = e.call("POST", "/payments/subscribe", map[string]any{"tier": "gold"}, u)
	f = st.last()
	if c != 201 || f.Get("mode") != "subscription" || f.Get("line_items[0][price_data][unit_amount]") != "1999" ||
		f.Get("line_items[0][price_data][recurring][interval]") != "month" ||
		f.Get("subscription_data[metadata][user_id]") != u || f.Get("subscription_data[metadata][tier]") != "gold" {
		t.Fatalf("subscription session: %d %v", c, f)
	}
	if c, _ := e.call("POST", "/payments/subscribe", map[string]any{"tier": "diamond"}, u); c != 422 {
		t.Fatalf("unknown tier: %d", c)
	}
	if c, _ := e.call("POST", "/payments/coins", map[string]any{"pack_id": "starter"}, e.newUser("user", false, "Unv")); c != 422 {
		t.Fatalf("unverified buyer: %d", c)
	}
}

func TestWebhookSecurity(t *testing.T) {
	e, _ := needPay(t)
	u := e.user("Payer")
	body, _ := json.Marshal(map[string]any{"id": uniq("evt"), "type": "checkout.session.completed",
		"data": map[string]any{"object": map[string]any{"id": uniq("cs"), "mode": "payment", "payment_status": "paid",
			"amount_total": 99, "currency": "usd", "metadata": map[string]string{"user_id": u, "pack_id": "starter"}}}})

	if c := e.webhookRaw(body, ""); c != 400 {
		t.Fatalf("unsigned: %d", c)
	}
	if c := e.webhookRaw(body, payments.SignPayload(body, "whsec_wrong", time.Now())); c != 400 {
		t.Fatalf("wrong secret: %d", c)
	}
	if c := e.webhookRaw(body, payments.SignPayload(body, whSecret, time.Now().Add(-10*time.Minute))); c != 400 {
		t.Fatalf("replayed old signature: %d", c)
	}
	tampered := append([]byte(nil), body...)
	tampered[len(tampered)-3] = '9'
	if c := e.webhookRaw(tampered, payments.SignPayload(body, whSecret, time.Now())); c != 400 {
		t.Fatalf("tampered body: %d", c)
	}
	if e.coins(u) != 0 {
		t.Fatal("a rejected webhook granted coins")
	}
	if c := e.webhookRaw(body, payments.SignPayload(body, whSecret, time.Now())); c != 200 {
		t.Fatalf("valid: %d", c)
	}
	if e.coins(u) != 50 {
		t.Fatalf("coins after valid event: %d want 50", e.coins(u))
	}
}

func TestWebhookWithoutSecretIsRefused(t *testing.T) {
	e := setup(t) // default config: no Stripe at all
	body := []byte(`{"id":"evt_x","type":"charge.refunded","data":{"object":{}}}`)
	if c := e.webhookRaw(body, payments.SignPayload(body, "", time.Now())); c != 503 {
		t.Fatalf("webhook with no secret configured: %d want 503", c)
	}
	u := e.user("Nobody")
	if c, _ := e.call("POST", "/payments/coins", map[string]any{"pack_id": "starter"}, u); c != 503 {
		t.Fatalf("checkout with no Stripe configured: %d want 503", c)
	}
}

func TestCoinsGrantedExactlyOnce(t *testing.T) {
	e, _ := needPay(t)
	u := e.user("Once")
	session := uniq("cs_once")
	obj := map[string]any{"id": session, "mode": "payment", "payment_status": "paid", "payment_intent": uniq("pi"),
		"amount_total": 499, "currency": "usd", "metadata": map[string]string{"user_id": u, "pack_id": "small"}}

	evt := uniq("evt")
	e.event(evt, "checkout.session.completed", obj)
	if c := e.event(evt, "checkout.session.completed", obj); c != 200 { // Stripe retry of the same event
		t.Fatalf("replay: %d", c)
	}
	// The same session announced by a different event must not double-credit.
	e.event(uniq("evt"), "checkout.session.async_payment_succeeded", obj)
	if got := e.coins(u); got != 275 {
		t.Fatalf("coins = %d, want exactly 275", got)
	}

	// Unpaid, wrong amount, unknown pack, subscription-mode: nothing granted.
	bad := []map[string]any{
		{"id": uniq("cs"), "mode": "payment", "payment_status": "unpaid", "amount_total": 499, "currency": "usd",
			"metadata": map[string]string{"user_id": u, "pack_id": "small"}},
		{"id": uniq("cs"), "mode": "payment", "payment_status": "paid", "amount_total": 100, "currency": "usd",
			"metadata": map[string]string{"user_id": u, "pack_id": "mega"}},
		{"id": uniq("cs"), "mode": "payment", "payment_status": "paid", "amount_total": 499, "currency": "usd",
			"metadata": map[string]string{"user_id": u, "pack_id": "nonexistent"}},
		{"id": uniq("cs"), "mode": "subscription", "payment_status": "paid", "amount_total": 499, "currency": "usd",
			"metadata": map[string]string{"user_id": u, "pack_id": "small"}},
	}
	for i, o := range bad {
		if c := e.event(uniq("evt"), "checkout.session.completed", o); c != 200 {
			t.Fatalf("bad case %d should be acknowledged: %d", i, c)
		}
	}
	if got := e.coins(u); got != 275 {
		t.Fatalf("invalid checkouts changed the balance: %d", got)
	}
	if c := e.event(uniq("evt"), "some.unknown.event", map[string]any{}); c != 200 {
		t.Fatalf("unknown event type: %d", c)
	}
}

func TestSubscriptionLifecycleAndCancel(t *testing.T) {
	e, st := needPay(t)
	u := e.user("Sub")

	acct := func() (tier string, perks map[string]any, body []byte) {
		_, b := e.call("GET", "/payments/balance", nil, u)
		var a struct {
			Tier  string
			Perks map[string]any
		}
		decodeInto(t, b, &a)
		return a.Tier, a.Perks, b
	}
	if tier, _, _ := acct(); tier != "" {
		t.Fatalf("new user tier %q", tier)
	}

	sub := e.subscribe(u, "gold", "active", 30*24*time.Hour)
	tier, perks, _ := acct()
	if tier != "gold" || perks["see_who_liked_you"] != true || perks["vip_badge"] != false || perks["unlimited_likes"] != true {
		t.Fatalf("gold perks: %s %v", tier, perks)
	}
	if c, _ := e.call("POST", "/payments/subscribe", map[string]any{"tier": "platinum"}, u); c != 409 {
		t.Fatalf("second subscription must be refused: %d", c)
	}

	// Cancel is requested from Stripe; the local tier only changes on its webhook.
	if c, _ := e.call("POST", "/payments/cancel", nil, u); c != 202 || len(st.cancels) != 1 || st.cancels[0] != sub {
		t.Fatalf("cancel: cancels=%v", st.cancels)
	}
	if tier, _, _ := acct(); tier != "gold" {
		t.Fatalf("tier must stay until the period ends, got %q", tier)
	}
	// Stripe reports it set to cancel at period end, then the subscription ends.
	e.event(uniq("evt"), "customer.subscription.updated", map[string]any{"id": sub, "status": "active",
		"cancel_at_period_end": true, "current_period_end": time.Now().Add(24 * time.Hour).Unix(),
		"metadata": map[string]string{"user_id": u, "tier": "gold"}})
	if _, _, b := acct(); !strings.Contains(string(b), `"cancels_at_period_end":true`) {
		t.Fatalf("cancellation flag missing: %s", b)
	}
	e.subEvent("customer.subscription.deleted", sub, u, "gold", "canceled", 0)
	if tier, _, _ := acct(); tier != "" {
		t.Fatalf("tier after deletion: %q", tier)
	}
	if c, _ := e.call("POST", "/payments/cancel", nil, u); c != 404 {
		t.Fatalf("cancel with no subscription: %d", c)
	}

	// An unpaid/expired period is not an entitlement.
	u2 := e.user("Lapsed")
	e.subscribe(u2, "platinum", "active", -time.Hour)
	_, b := e.call("GET", "/payments/balance", nil, u2)
	if !strings.Contains(string(b), `"tier":""`) {
		t.Fatalf("expired subscription still entitles: %s", b)
	}
	u3 := e.user("PastDue")
	e.subscribe(u3, "silver", "past_due", 24*time.Hour)
	if _, b := e.call("GET", "/payments/balance", nil, u3); !strings.Contains(string(b), `"tier":""`) {
		t.Fatalf("past_due must not entitle: %s", b)
	}
}

func TestRefundClawsBackCoins(t *testing.T) {
	e, _ := needPay(t)
	u := e.user("Refunded")
	pi := uniq("pi_refund")
	e.event(uniq("evt"), "checkout.session.completed", map[string]any{"id": uniq("cs"), "mode": "payment",
		"payment_status": "paid", "payment_intent": pi, "amount_total": 999, "currency": "usd",
		"metadata": map[string]string{"user_id": u, "pack_id": "medium"}})
	if e.coins(u) != 600 {
		t.Fatalf("setup: %d", e.coins(u))
	}
	e.grant(u, 0+1) // 601 total; the user then spends 5 (superlike) before the refund
	ref := map[string]any{"id": uniq("ch"), "payment_intent": pi, "amount": 999, "amount_refunded": 400}
	e.event(uniq("evt"), "charge.refunded", ref) // partial refund: no automatic clawback
	if e.coins(u) != 601 {
		t.Fatalf("partial refund must not claw back automatically: %d", e.coins(u))
	}
	ref["amount_refunded"] = 999
	evt := uniq("evt")
	e.event(evt, "charge.refunded", ref)
	e.event(evt, "charge.refunded", ref)
	e.event(uniq("evt"), "charge.refunded", ref) // same charge, new event id
	if got := e.coins(u); got != 1 {
		t.Fatalf("after full refund balance = %d, want 1", got)
	}
}
