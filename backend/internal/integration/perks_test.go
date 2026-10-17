package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fillLikes records n likes for actor against n fresh users, bypassing the API.
func (e *env) fillLikes(actor string, n int) {
	e.t.Helper()
	for i := 0; i < n; i++ {
		target := e.user(fmt.Sprintf("Filler%d", i))
		e.exec(`INSERT INTO swipes (actor_id, target_id, action) VALUES ($1,$2,'like')`, actor, target)
	}
}

func TestFreeLikeCapAndSubscriberUnlimited(t *testing.T) {
	e, _ := needPay(t)
	u := e.user("Capped")
	e.fillLikes(u, 50)
	target := e.refOf(e.user("Next"))

	if c, b := e.swipe(u, target, "like"); c != 402 {
		t.Fatalf("51st like of the day: %d %+v", c, b)
	}
	// Passing is never capped.
	if c, _ := e.swipe(u, target, "pass"); c != 200 {
		t.Fatalf("pass at the cap: %d", c)
	}
	// Subscribing lifts the cap immediately.
	e.subscribe(u, "silver", "active", 30*24*time.Hour)
	other := e.refOf(e.user("Another"))
	if c, _ := e.swipe(u, other, "like"); c != 200 {
		t.Fatalf("subscriber like: %d", c)
	}
	_, b := e.call("GET", "/payments/balance", nil, u)
	if strings.Contains(string(b), `"likes_left_today"`) {
		t.Fatalf("subscriber must show unlimited likes: %s", b)
	}
}

func TestSuperLikeAllowanceAndBoostPerk(t *testing.T) {
	e, _ := needPay(t)
	u := e.user("Gold")
	e.subscribe(u, "gold", "active", 30*24*time.Hour)

	// 5 free super likes a day with zero coins; the 6th needs coins.
	for i := 1; i <= 5; i++ {
		c, _ := e.call("POST", "/matching/superlike", map[string]string{"target_ref": e.refOf(e.user("T"))}, u)
		if c != 200 {
			t.Fatalf("free super like %d: %d", i, c)
		}
	}
	six := e.refOf(e.user("Sixth"))
	if c, _ := e.call("POST", "/matching/superlike", map[string]string{"target_ref": six}, u); c != 402 {
		t.Fatalf("6th super like without coins: %d", c)
	}
	e.grant(u, 5)
	if c, _ := e.call("POST", "/matching/superlike", map[string]string{"target_ref": six}, u); c != 200 || e.coins(u) != 0 {
		t.Fatalf("6th super like with coins: %d balance=%d", c, e.coins(u))
	}

	// One free boost a week for Gold; the next costs coins.
	if c, _ := e.call("POST", "/matching/boost", nil, u); c != 200 || e.coins(u) != 0 {
		t.Fatalf("free weekly boost: %d balance=%d", c, e.coins(u))
	}
	if c, _ := e.call("POST", "/matching/boost", nil, u); c != 402 {
		t.Fatalf("second boost without coins: %d", c)
	}
	e.grant(u, 20)
	if c, _ := e.call("POST", "/matching/boost", nil, u); c != 200 || e.coins(u) != 0 {
		t.Fatalf("paid boost: %d balance=%d", c, e.coins(u))
	}

	// Platinum: unlimited free boosts.
	p := e.user("Plat")
	e.subscribe(p, "platinum", "active", 30*24*time.Hour)
	for i := 0; i < 3; i++ {
		if c, _ := e.call("POST", "/matching/boost", nil, p); c != 200 {
			t.Fatalf("platinum boost %d: %d", i, c)
		}
	}
	if e.coins(p) != 0 {
		t.Fatal("platinum boosts must not cost coins")
	}
	// A user with no subscription pays for the first boost.
	f := e.user("Free")
	if c, _ := e.call("POST", "/matching/boost", nil, f); c != 402 {
		t.Fatalf("free user boost: %d", c)
	}
}

func TestSeeWhoLikedYouIsGoldPerk(t *testing.T) {
	e, _ := needPay(t)
	me, fan := e.user("Me"), e.user("Admirer Name")
	e.swipe(fan, e.refOf(me), "like")

	var r struct {
		Count    int
		Locked   bool
		Profiles []map[string]any
	}
	_, b := e.call("GET", "/matching/likes-you", nil, me)
	decodeInto(t, b, &r)
	if r.Count != 1 || !r.Locked || len(r.Profiles) != 0 || strings.Contains(string(b), "Admirer") {
		t.Fatalf("free user: %s", b)
	}

	e.subscribe(me, "gold", "active", 30*24*time.Hour)
	_, b = e.call("GET", "/matching/likes-you", nil, me)
	r.Profiles = nil
	decodeInto(t, b, &r)
	if r.Locked || len(r.Profiles) != 1 || !strings.Contains(string(b), "Admirer Name") {
		t.Fatalf("gold user: %s", b)
	}

	// Someone who liked you anonymously stays anonymous even to Gold.
	anon := e.user("Hidden Admirer")
	e.call("POST", "/users/me/mode", map[string]string{"mode": "incognito"}, anon)
	e.swipe(anon, e.refOf(me), "like")
	_, b = e.call("GET", "/matching/likes-you", nil, me)
	if strings.Contains(string(b), "Hidden Admirer") {
		t.Fatalf("incognito liker unmasked by the perk: %s", b)
	}

	// Once you swipe on them they leave the list.
	e.swipe(me, e.refOf(fan), "like")
	_, b = e.call("GET", "/matching/likes-you", nil, me)
	if strings.Contains(string(b), "Admirer Name") {
		t.Fatalf("already-swiped liker still listed: %s", b)
	}
}

func TestInvisibilityAndVIPBadge(t *testing.T) {
	e, _ := needPay(t)
	lat, lng := randomBase()
	viewer, ghost, vip := e.user("Viewer"), e.user("Ghost Gold"), e.user("Star Plat")
	e.locate(viewer, lat, lng)
	e.locate(ghost, lat+0.002, lng)
	e.locate(vip, lat+0.003, lng)

	if c, _ := e.call("POST", "/users/me/invisible", map[string]bool{"enabled": true}, ghost); c != 402 {
		t.Fatalf("invisibility without a plan: %d", c)
	}
	gsub := e.subscribe(ghost, "gold", "active", 30*24*time.Hour)
	if c, _ := e.call("POST", "/users/me/invisible", map[string]bool{"enabled": true}, ghost); c != 200 {
		t.Fatalf("invisibility as gold: %d", c)
	}
	e.subscribe(vip, "platinum", "active", 30*24*time.Hour)

	_, b := e.call("GET", "/users/nearby?radius_km=5", nil, viewer)
	s := string(b)
	if strings.Contains(s, "Ghost Gold") {
		t.Fatalf("invisible user was listed: %s", s)
	}
	if !strings.Contains(s, "Star Plat") || !strings.Contains(s, `"vip":true`) {
		t.Fatalf("platinum VIP badge missing: %s", s)
	}
	// Matches can still see an invisible user (invisibility is for discovery).
	e.swipe(viewer, e.refOf(ghost), "like")
	if _, r := e.swipe(ghost, e.refOf(viewer), "like"); !r.Matched {
		t.Fatal("an invisible user must still be able to match")
	}

	// When the plan ends the user becomes discoverable again.
	e.subEvent("customer.subscription.deleted", gsub, ghost, "gold", "canceled", 0)
	_, b = e.call("GET", "/users/nearby?radius_km=5", nil, e.user("Viewer2"))
	_ = b
	var me struct{ Invisible bool }
	_, mb := e.call("GET", "/users/me", nil, ghost)
	decodeInto(t, mb, &me)
	if me.Invisible {
		t.Fatalf("invisibility must switch off when the perk ends: %s", mb)
	}
}

func TestGifts(t *testing.T) {
	e, _ := needPay(t)
	a, b, mid := e.matched()
	_ = mid
	stranger := e.user("Stranger")
	refB := e.refOf(b)

	if c, _ := e.call("POST", "/payments/gift", map[string]string{"to_ref": refB, "gift_id": "rose"}, a); c != 402 {
		t.Fatalf("gift without coins: %d", c)
	}
	e.grant(a, 100)
	if c, _ := e.call("POST", "/payments/gift", map[string]string{"to_ref": e.refOf(stranger), "gift_id": "rose"}, a); c != 404 {
		t.Fatalf("gift to a non-match: %d", c)
	}
	if c, _ := e.call("POST", "/payments/gift", map[string]string{"to_ref": refB, "gift_id": "yacht"}, a); c != 404 {
		t.Fatalf("unknown gift: %d", c)
	}
	if c, _ := e.call("POST", "/payments/gift", map[string]string{"to_ref": e.refOf(a), "gift_id": "rose"}, a); c != 422 {
		t.Fatalf("gift to self: %d", c)
	}
	if e.coins(a) != 100 {
		t.Fatalf("failed gifts must not charge: %d", e.coins(a))
	}

	body := map[string]string{"to_ref": refB, "gift_id": "champagne", "request_id": "r-1"}
	for i := 0; i < 3; i++ { // client retries must not double charge
		if c, _ := e.call("POST", "/payments/gift", body, a); c != 204 {
			t.Fatalf("gift attempt %d: %d", i, c)
		}
	}
	if e.coins(a) != 50 {
		t.Fatalf("balance after idempotent gift: %d want 50", e.coins(a))
	}

	_, rb := e.call("GET", "/payments/gifts", nil, b)
	var got struct {
		Gifts []struct {
			GiftID  string `json:"gift_id"`
			FromRef string `json:"from_ref"`
		}
	}
	if err := json.Unmarshal(rb, &got); err != nil || len(got.Gifts) != 1 || got.Gifts[0].GiftID != "champagne" ||
		got.Gifts[0].FromRef != e.refOf(a) {
		t.Fatalf("received: %s", rb)
	}
	if strings.Contains(string(rb), a) {
		t.Fatal("gift listing exposes the sender's user id")
	}

	// A block ends the ability to gift and hides received gifts.
	e.call("POST", "/moderation/blocks", map[string]string{"ref": refB}, a)
	if c, _ := e.call("POST", "/payments/gift", map[string]string{"to_ref": refB, "gift_id": "rose"}, a); c != 404 {
		t.Fatalf("gift to a blocked user: %d", c)
	}
	if _, rb = e.call("GET", "/payments/gifts", nil, b); strings.Contains(string(rb), "champagne") {
		t.Fatalf("gift from a blocking sender still shown: %s", rb)
	}
}

func TestHotelGiftsAreDeletedWithTheStay(t *testing.T) {
	e, _ := needPay(t)
	lat, lng := randomBase()
	hotelID := e.hotel(lat, lng)
	u1, u2 := e.user("G1"), e.user("G2")
	e.locate(u1, lat, lng)
	e.locate(u2, lat+0.0002, lng)
	e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u1)
	e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u2)
	e.grant(u1, 50)

	// Not matched, but both are guests at the same hotel.
	if c, b := e.call("POST", "/payments/gift", map[string]string{"to_ref": e.refOf(u2), "gift_id": "drink"}, u1); c != 204 {
		t.Fatalf("hotel gift: %d %s", c, b)
	}
	if n := e.count(`SELECT COUNT(*) FROM gifts_sent WHERE from_user=$1 AND checkin_id IS NOT NULL`, u1); n != 1 {
		t.Fatal("a gift sent inside a stay must be bound to it")
	}
	e.exec(`UPDATE hotel_checkins SET checked_out_at=NOW(), auto_delete_at = NOW() - INTERVAL '1 minute' WHERE user_id=$1`, u1)
	if _, err := server_geo(e).RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM gifts_sent WHERE from_user=$1`, u1); n != 0 {
		t.Fatalf("gift survived the stay purge: %d", n)
	}
	// Coins spent are not refunded: the ledger is append-only.
	if e.coins(u1) != 35 {
		t.Fatalf("balance %d want 35", e.coins(u1))
	}
}
