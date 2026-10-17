package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectme/internal/auth"
	"connectme/internal/server"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"
)

const erasePW = "Correct-Horse-9!"

// userWithPassword inserts a verified user whose password is known.
func (e *env) userWithPassword(name string) (id, email string) {
	e.t.Helper()
	h, _ := bcrypt.GenerateFromPassword([]byte(erasePW), 4)
	email = fmt.Sprintf("erase-%d@example.com", time.Now().UnixNano())
	err := e.db.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, date_of_birth, is_verified)
		VALUES ($1,$2,'1990-04-04',TRUE) RETURNING id::text`, email, string(h)).Scan(&id)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.db.Exec(context.Background(),
		`INSERT INTO profiles (user_id, display_name, intro, gender, tags) VALUES ($1,$2,'hello','woman','{hiking}')`, id, name); err != nil {
		e.t.Fatal(err)
	}
	return id, email
}

func (e *env) erase(uid, pw, confirm string) int {
	c, _ := e.call("POST", "/privacy/erase", map[string]string{"password": pw, "confirm": confirm}, uid)
	return c
}

func TestEraseGuards(t *testing.T) {
	e := setup(t)
	u, _ := e.userWithPassword("Guarded")
	if c := e.erase(u, "wrong-password", "DELETE"); c != 403 {
		t.Fatalf("wrong password: %d", c)
	}
	if c := e.erase(u, erasePW, "yes"); c != 422 {
		t.Fatalf("missing confirmation word: %d", c)
	}
	if c, _ := e.call("POST", "/privacy/erase", map[string]any{"password": erasePW, "confirm": "DELETE", "extra": 1}, u); c != 400 {
		t.Fatalf("unknown field: %d", c)
	}
	if e.count(`SELECT COUNT(*) FROM users WHERE id=$1`, u) != 1 {
		t.Fatal("a refused erasure must not delete anything")
	}
	// Staff accounts must be demoted first so the platform cannot be locked out.
	admin, _ := e.userWithPassword("Admin")
	e.exec(`UPDATE users SET role='admin' WHERE id=$1`, admin)
	if c, _ := e.callAs("POST", "/privacy/erase", map[string]string{"password": erasePW, "confirm": "DELETE"}, admin, "admin"); c != 409 {
		t.Fatalf("admin erase: %d", c)
	}
}

func TestExportContainsOnlyYourData(t *testing.T) {
	e := setup(t)
	a, _ := e.userWithPassword("Exporter")
	b, _ := e.userWithPassword("Counterpart")
	e.call("PATCH", "/users/me", map[string]any{"bio": "my bio"}, a)
	e.swipe(a, e.refOf(b), "like")
	e.swipe(b, e.refOf(a), "like")
	e.grant(a, 25)

	c, body := e.call("GET", "/privacy/export", nil, a)
	if c != 200 {
		t.Fatalf("export: %d %s", c, body)
	}
	s := string(body)
	for _, want := range []string{"my bio", "\"balance\":25", "counterpart_ref", e.refOf(b), "end-to-end encrypted"} {
		if !strings.Contains(s, want) {
			t.Errorf("export missing %q", want)
		}
	}
	for _, leak := range []string{b, "Counterpart", "password_hash", "lat\":", "lng\":"} {
		if strings.Contains(s, leak) {
			t.Errorf("export leaks %q", leak)
		}
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
}

func TestErasureRemovesPersonalDataEverywhere(t *testing.T) {
	e := needChat(t)
	st := e.withStripe()
	ctx := context.Background()
	lat, lng := randomBase()

	alice, aliceEmail := e.userWithPassword("Alice Erasable")
	bob, _ := e.userWithPassword("Bob Stays")
	carol, _ := e.userWithPassword("Carol Stays")
	e.locate(alice, lat, lng)
	e.locate(carol, lat+0.001, lng)

	// A photo (file on disk), a match with chat + attachment.
	if c := uploadPhoto(e, alice, jpegBytesFor(t)); c != 201 {
		t.Fatalf("photo: %d", c)
	}
	_, me := e.call("GET", "/users/me", nil, alice)
	var prof struct{ Photos []struct{ ID string } }
	decodeInto(t, me, &prof)
	photoID := prof.Photos[0].ID
	if !e.photoStored(photoID) {
		t.Fatal("photo should be stored before erasure")
	}

	e.swipe(alice, e.refOf(bob), "like")
	_, r := e.swipe(bob, e.refOf(alice), "like")
	if !r.Matched {
		t.Fatal("setup: no match")
	}
	e.send(alice, r.MatchID, map[string]any{"type": "text", "ciphertext": ct("from alice")})
	e.send(bob, r.MatchID, map[string]any{"type": "text", "ciphertext": ct("from bob")})
	_, mb := e.raw("POST", "/chat/matches/"+r.MatchID+"/media", []byte("blob"), alice)
	var up struct {
		MediaID string `json:"media_id"`
	}
	decodeInto(t, mb, &up)

	// A double date with Carol, matched with another pair.
	_, gA := e.newGroup(alice, "double", "dinner", 0)
	e.join(alice, carol, gA.ID)
	d1, d2, gB := e.double(lat+0.002, lng, "dinner")
	_, _ = d1, d2
	e.groupMatch(gA.ID, gB, alice)
	e.groupMatch(gA.ID, gB, carol)
	e.groupMatch(gB, gA.ID, d1)
	_, gm := e.groupMatch(gB, gA.ID, d2)
	if !gm.Matched {
		t.Fatal("setup: group match")
	}
	e.send(alice, gm.GroupMatchID, map[string]any{"type": "text", "ciphertext": ct("group hello")})

	// Money: a subscription, a purchase, a gift, a block, a report.
	sub := e.subscribe(alice, "gold", "active", 30*24*time.Hour)
	piID := uniq("pi")
	e.event(uniq("evt"), "checkout.session.completed", map[string]any{"id": uniq("cs"), "mode": "payment",
		"payment_status": "paid", "payment_intent": piID, "amount_total": 499, "currency": "usd",
		"metadata": map[string]string{"user_id": alice, "pack_id": "small"}})
	e.call("POST", "/payments/gift", map[string]string{"to_ref": e.refOf(bob), "gift_id": "rose"}, alice)
	e.call("POST", "/moderation/reports", map[string]any{"ref": e.refOf(carol), "reason": "spam"}, alice)

	aliceRefreshTokens := auth.NewTokenStore(e.rdb.Client, time.Hour)
	refresh, _, _ := aliceRefreshTokens.Issue(ctx, alice, "")

	// ---- erase ----
	if c := e.erase(alice, erasePW, "DELETE"); c != 204 {
		t.Fatalf("erase: %d", c)
	}

	count := func(sql string, args ...any) int { return e.count(sql, args...) }
	for name, q := range map[string]string{
		"user":         `SELECT COUNT(*) FROM users WHERE id=$1`,
		"profile":      `SELECT COUNT(*) FROM profiles WHERE user_id=$1`,
		"swipes":       `SELECT COUNT(*) FROM swipes WHERE actor_id=$1 OR target_id=$1`,
		"matches":      `SELECT COUNT(*) FROM matches WHERE user1_id=$1 OR user2_id=$1`,
		"memberships":  `SELECT COUNT(*) FROM group_members WHERE user_id=$1`,
		"ledger":       `SELECT COUNT(*) FROM coin_ledger WHERE user_id=$1`,
		"subscription": `SELECT COUNT(*) FROM subscriptions WHERE user_id=$1`,
		"gifts":        `SELECT COUNT(*) FROM gifts_sent WHERE from_user=$1 OR to_user=$1`,
		"key bundle":   `SELECT COUNT(*) FROM key_bundles WHERE user_id=$1`,
		"reports by":   `SELECT COUNT(*) FROM reports WHERE reporter_id=$1`,
	} {
		if n := count(q, alice); n != 0 {
			t.Errorf("%s survived erasure (%d rows)", name, n)
		}
	}
	if e.photoStored(photoID) {
		t.Error("photo must be deleted from storage")
	}
	cm := e.deps.Mongo
	if n, _ := cm.Collection("chat_messages").CountDocuments(ctx, bson.M{"match_id": r.MatchID}); n != 0 {
		t.Errorf("%d 1:1 messages survived", n)
	}
	if n, _ := cm.Collection("chat_media").CountDocuments(ctx, bson.M{"_id": up.MediaID}); n != 0 {
		t.Error("attachment record survived")
	}
	if n, _ := cm.Collection("chat_messages").CountDocuments(ctx, bson.M{"sender_id": alice}); n != 0 {
		t.Errorf("%d group messages by the user survived", n)
	}

	// Retained, but anonymised: financial records.
	if n := count(`SELECT COUNT(*) FROM payments WHERE stripe_id=$1 AND user_id IS NULL`, piID); n != 1 {
		t.Errorf("payment record must be kept with user_id NULL, got %d", n)
	}
	// The compliance record of the erasure itself.
	if n := count(`SELECT COUNT(*) FROM erasure_requests WHERE user_id=$1 AND completed_at IS NOT NULL`, alice); n != 1 {
		t.Errorf("erasure record missing: %d", n)
	}
	// Stripe was told to stop billing.
	st.mu.Lock()
	cancelled := false
	for _, c := range st.cancels {
		if c == sub {
			cancelled = true
		}
	}
	st.mu.Unlock()
	if !cancelled {
		t.Error("the Stripe subscription must be cancelled immediately")
	}

	// Others are unharmed but no longer see her.
	if c, _ := e.call("GET", "/chat/matches/"+r.MatchID+"/messages", nil, bob); c != 404 {
		t.Errorf("counterpart still reaches the deleted chat: %d", c)
	}
	if _, body := e.call("GET", "/matching/matches", nil, bob); strings.Contains(string(body), r.MatchID) {
		t.Error("counterpart still lists the match")
	}
	_, gbody := e.call("GET", "/groups/"+gA.ID, nil, carol)
	var g struct {
		MemberCount int  `json:"member_count"`
		Ready       bool `json:"ready"`
		Locked      bool `json:"locked"`
	}
	decodeInto(t, gbody, &g)
	if g.MemberCount != 1 || g.Ready || g.Locked {
		t.Errorf("group after member erased: %s", gbody)
	}
	if n := count(`SELECT COUNT(*) FROM group_members WHERE group_id=$1 AND user_id=$2 AND role='owner'`, gA.ID, carol); n != 1 {
		t.Error("the remaining member must become owner")
	}
	if c, _ := e.call("GET", "/chat/matches/"+gm.GroupMatchID+"/messages", nil, d1); c != 404 {
		t.Errorf("a broken group match must lose its chat: %d", c)
	}

	// The account cannot be used any more.
	if c, _ := e.call("GET", "/users/me", nil, alice); c != 404 {
		t.Errorf("deleted account still resolves: %d", c)
	}
	if _, _, err := aliceRefreshTokens.Rotate(ctx, refresh); err == nil {
		t.Error("refresh tokens must be revoked")
	}
	if c, _ := authLogin(e, aliceEmail, erasePW); c != 401 {
		t.Errorf("login after erasure: %d", c)
	}
}

func TestStripeFailureAbortsErasureBeforeAnythingIsDeleted(t *testing.T) {
	e, _ := needPay(t)
	// A Stripe that refuses to cancel.
	e.deps.Cfg.StripeAPIBase = "http://127.0.0.1:1"
	e.h = server.New(e.deps).Handler
	u, _ := e.userWithPassword("StillBilled")
	e.subscribe(u, "gold", "active", 30*24*time.Hour)

	if c := e.erase(u, erasePW, "DELETE"); c != 500 {
		t.Fatalf("erase with billing unreachable: %d", c)
	}
	if e.count(`SELECT COUNT(*) FROM users WHERE id=$1`, u) != 1 ||
		e.count(`SELECT COUNT(*) FROM subscriptions WHERE user_id=$1`, u) != 1 {
		t.Fatal("nothing may be deleted while the subscription may still be charging")
	}
}

func TestBannedAccountCannotReRegisterAfterErasure(t *testing.T) {
	e := setup(t)
	u, email := e.userWithPassword("Banned Erasing")
	e.exec(`UPDATE users SET is_banned=TRUE WHERE id=$1`, u)
	if c := e.erase(u, erasePW, "DELETE"); c != 204 {
		t.Fatalf("erase: %d", c)
	}
	// Registering again looks identical to any other signup, but creates nothing.
	c, _ := authRegister(e, email, erasePW)
	if c != 201 {
		t.Fatalf("register response must not reveal the ban: %d", c)
	}
	if n := e.count(`SELECT COUNT(*) FROM users WHERE LOWER(email)=LOWER($1)`, email); n != 0 {
		t.Fatal("a banned identity re-registered")
	}
	// An ordinary (not banned) erased email may sign up again.
	u2, email2 := e.userWithPassword("Free To Return")
	if c := e.erase(u2, erasePW, "DELETE"); c != 204 {
		t.Fatal(c)
	}
	authRegister(e, email2, erasePW)
	if n := e.count(`SELECT COUNT(*) FROM users WHERE LOWER(email)=LOWER($1)`, email2); n != 1 {
		t.Fatalf("a normal erased user must be able to return: %d", n)
	}
}

func authLogin(e *env, email, pw string) (int, []byte) {
	return e.call("POST", "/auth/login", map[string]string{"email": email, "password": pw}, "anon")
}

func authRegister(e *env, email, pw string) (int, []byte) {
	return e.call("POST", "/auth/register",
		map[string]string{"email": email, "password": pw, "date_of_birth": "1990-01-01"}, "anon")
}
