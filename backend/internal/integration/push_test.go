package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectme/internal/push"
)

const tokA = "device-token-aaaaaaaaaaaaaaaaaaaaaaaa"

func (e *env) register(uid, token, platform string) int {
	c, _ := e.call("PUT", "/push/devices", map[string]string{"token": token, "platform": platform}, uid)
	return c
}

// waitPush collects what the stub receives for up to ~1.5 s (delivery is
// asynchronous), stopping early once n messages have arrived.
func (e *env) waitPush(n int) []push.Sent {
	var got []push.Sent
	for i := 0; i < 30; i++ {
		got = append(got, e.push.Taken()...)
		if len(got) >= n {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return got
}

// quiet asserts nothing is sent for a short while.
func (e *env) quiet() []push.Sent {
	time.Sleep(400 * time.Millisecond)
	return e.push.Taken()
}

func TestPushDeviceRegistration(t *testing.T) {
	e := setup(t)
	a, b := e.user("Pusher A"), e.user("Pusher B")
	tok := "shared-phone-token-" + e.refOf(a)

	for name, tc := range map[string]struct{ token, platform string }{
		"short token":  {"abc", "android"},
		"spaces":       {"has spaces in the token value here", "ios"},
		"huge token":   {strings.Repeat("x", 2000), "ios"},
		"bad platform": {tok, "windows"},
	} {
		if c := e.register(a, tc.token, tc.platform); c != 422 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if c := e.register(a, tok, "android"); c != 204 {
		t.Fatalf("register: %d", c)
	}
	count := func(u string) int {
		var n int
		if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM push_devices WHERE user_id=$1`, u).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// A token belongs to whoever registered it last (a shared phone).
	if c := e.register(b, tok, "android"); c != 204 || count(a) != 0 || count(b) != 1 {
		t.Errorf("token not moved: %d a=%d b=%d", c, count(a), count(b))
	}
	// Ten devices at most; the oldest go.
	for i := 0; i < 12; i++ {
		e.register(a, "device-"+strings.Repeat("z", 20)+string(rune('a'+i)), "ios")
	}
	if n := count(a); n != 10 {
		t.Errorf("devices kept: %d", n)
	}
	if c, _ := e.call("POST", "/push/devices/remove", map[string]string{"token": tok}, a); c != 204 || count(b) != 1 {
		t.Errorf("a removed b's token: %d b=%d", c, count(b))
	}
	if c, _ := e.call("POST", "/push/devices/remove", map[string]string{"token": tok}, b); c != 204 || count(b) != 0 {
		t.Errorf("remove: %d b=%d", c, count(b))
	}
}

func TestPushPrefs(t *testing.T) {
	e := setup(t)
	u := e.user("Prefs")
	_, b := e.call("GET", "/push/prefs", nil, u)
	if !strings.Contains(string(b), `"messages":true`) || !strings.Contains(string(b), `"calls":true`) || !strings.Contains(string(b), `"devices":0`) {
		t.Errorf("defaults: %s", b)
	}
	if c, _ := e.call("PUT", "/push/prefs", map[string]bool{"messages": false, "matches": true, "calls": true, "bookings": false}, u); c != 204 {
		t.Fatalf("set: %d", c)
	}
	_, b = e.call("GET", "/push/prefs", nil, u)
	if !strings.Contains(string(b), `"messages":false`) || !strings.Contains(string(b), `"bookings":false`) || !strings.Contains(string(b), `"matches":true`) {
		t.Errorf("after set: %s", b)
	}
	if c, _ := e.call("PUT", "/push/prefs", map[string]any{"messages": "no"}, u); c != 400 {
		t.Errorf("bad prefs body: %d", c)
	}
}

func TestPushMessagesAreGenericCollapsedAndRespectPrefs(t *testing.T) {
	e := needChat(t)
	a, b, m := e.matched()
	e.register(b, tokA, "android")

	// Two messages in a row: one notification, with nothing of the message in it.
	e.send(a, m, map[string]any{"type": "text", "ciphertext": ct("super secret plans")})
	e.send(a, m, map[string]any{"type": "text", "ciphertext": ct("second one")})
	got := e.waitPush(1)
	extra := e.quiet()
	if len(got)+len(extra) != 1 {
		t.Fatalf("want 1 notification for a burst, got %d", len(got)+len(extra))
	}
	n := got[0]
	if n.Token != tokA || n.Platform != "android" || n.Msg.Body != "You have a new message" || n.Msg.Title != "ConnectMe" {
		t.Errorf("notification: %+v", n)
	}
	if len(n.Msg.Data) != 2 || n.Msg.Data["kind"] != "message" || n.Msg.Data["ref"] != m {
		t.Errorf("data should be only kind and the conversation id: %v", n.Msg.Data)
	}
	all := n.Msg.Title + n.Msg.Body + n.Msg.Data["ref"] + n.Msg.Data["kind"]
	for _, leak := range []string{"secret", "plans", "ChatA", "ChatB", e.refOf(a)} {
		if strings.Contains(all, leak) {
			t.Errorf("notification leaks %q", leak)
		}
	}
	// The sender is never notified of their own message.
	e.register(a, "sender-device-token-aaaaaaaaaaaaaa", "ios")
	e.send(b, m, map[string]any{"type": "text", "ciphertext": ct("reply")})
	for _, s := range e.waitPush(1) {
		if s.Token == "sender-device-token-aaaaaaaaaaaaaa" {
			continue // the reply goes to a, the other participant: expected
		}
		t.Errorf("unexpected recipient: %+v", s)
	}

	// Preferences switch a kind off.
	e.call("PUT", "/push/prefs", map[string]bool{"messages": false, "matches": true, "calls": true, "bookings": true}, b)
	e.send(a, m, map[string]any{"type": "text", "ciphertext": ct("after opt-out")})
	e.clearPushDedupe() // the collapse window would hide it otherwise
	e.send(a, m, map[string]any{"type": "text", "ciphertext": ct("again")})
	if got := e.quiet(); len(got) != 0 {
		t.Errorf("sent despite opt-out: %+v", got)
	}
}

func TestPushMatchesCallsAndDeadTokens(t *testing.T) {
	e := setup(t)
	a, b := e.user("Liker"), e.user("Liked")
	e.register(a, "liker-device-token-aaaaaaaaaaaaaaaa", "ios")
	e.register(b, "liked-device-token-bbbbbbbbbbbbbbbb", "android")

	// The first like is silent; the match notifies the person who liked first.
	e.swipe(a, e.refOf(b), "like")
	if got := e.quiet(); len(got) != 0 {
		t.Fatalf("a like alone notified someone: %+v", got)
	}
	_, r := e.swipe(b, e.refOf(a), "like")
	got := e.waitPush(1)
	if len(got) != 1 || got[0].Token != "liker-device-token-aaaaaaaaaaaaaaaa" || got[0].Msg.Body != "You have a new match" || got[0].Msg.Data["ref"] != r.MatchID {
		t.Fatalf("match notification: %+v", got)
	}
	e.quiet()

	// A call rings the callee with a high-priority, short-lived notification.
	c, call := e.startCall(a, r.MatchID, false)
	if c != 201 {
		t.Fatalf("call: %d", c)
	}
	got = e.waitPush(1)
	if len(got) != 1 || got[0].Token != "liked-device-token-bbbbbbbbbbbbbbbb" || got[0].Msg.Body != "Incoming call" ||
		!got[0].Msg.HighPrio || got[0].Msg.TTLSeconds != 45 || got[0].Msg.Data["ref"] != call.Call.ID {
		t.Fatalf("call notification: %+v", got)
	}
	e.endAllCalls()

	// A token the provider rejects is deleted, not retried forever.
	e.push.Bad = map[string]bool{"liked-device-token-bbbbbbbbbbbbbbbb": true}
	e.clearPushDedupe()
	e.startCall(a, r.MatchID, false)
	e.quiet()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM push_devices WHERE token='liked-device-token-bbbbbbbbbbbbbbbb'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("dead token kept: %d %v", n, err)
	}
	e.endAllCalls()
}

func (e *env) clearPushDedupe() {
	ctx := context.Background()
	keys, _ := e.rdb.Client.Keys(ctx, "push:dedupe:*").Result()
	if len(keys) > 0 {
		e.rdb.Client.Del(ctx, keys...)
	}
}

func TestPushSkipsBannedAndUnverifiedMembers(t *testing.T) {
	e := setup(t)
	stub := &push.Stub{}
	svc := push.NewService(e.db, e.rdb.Client, stub)
	svc.Start(context.Background())
	ok, banned, unverified := e.user("Fine"), e.user("Banned"), e.newUser("user", false, "Unverified")
	for i, u := range []string{ok, banned, unverified} {
		if err := svc.Register(context.Background(), u, "ios", "skip-test-device-token-"+strings.Repeat(string(rune('a'+i)), 20)); err != nil {
			t.Fatal(err)
		}
	}
	e.exec(`UPDATE users SET is_banned=true WHERE id=$1`, banned)
	for _, u := range []string{ok, banned, unverified} {
		svc.Notify(context.Background(), u, push.KindMatch, "ref-"+u)
	}
	time.Sleep(600 * time.Millisecond)
	got := stub.Taken()
	if len(got) != 1 || !strings.HasPrefix(got[0].Token, "skip-test-device-token-a") {
		t.Errorf("only the verified, unbanned member should be notified: %+v", got)
	}
}

func TestSigningOutEverywhereForgetsEveryDevice(t *testing.T) {
	e := setup(t)
	u := e.user("Leaver Out")
	e.register(u, "phone-one-token-aaaaaaaaaaaaaaaaaaa", "ios")
	e.register(u, "phone-two-token-bbbbbbbbbbbbbbbbbbb", "android")
	if c, _ := e.call("POST", "/auth/logout-all", nil, u); c != 204 {
		t.Fatalf("logout-all: %d", c)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM push_devices WHERE user_id=$1`, u).Scan(&n); err != nil || n != 0 {
		t.Errorf("devices after sign-out-everywhere: %d %v", n, err)
	}
}

func TestQuietHoursSilenceEverythingButCalls(t *testing.T) {
	e := needChat(t)
	a, b, m := e.matched()
	e.register(b, tokA, "android")
	now := time.Now().UTC()
	mid := now.Hour()*60 + now.Minute()
	start, end := (mid+1440-30)%1440, (mid+30)%1440
	q := map[string]any{"quiet": map[string]any{"on": true, "start": start, "end": end, "tz_min": 0}}
	if c, _ := e.call("PUT", "/push/prefs", map[string]any{"quiet": map[string]any{"on": true, "start": 2000, "end": 0, "tz_min": 0}}, b); c != 422 {
		t.Errorf("bad window: %d", c)
	}
	if c, _ := e.call("PUT", "/push/prefs", q, b); c != 204 {
		t.Fatalf("set quiet: %d", c)
	}
	// Other preferences were not reset by sending only the quiet window.
	_, body := e.call("GET", "/push/prefs", nil, b)
	if !strings.Contains(string(body), `"messages":true`) || !strings.Contains(string(body), `"on":true`) {
		t.Errorf("prefs: %s", body)
	}
	e.send(a, m, map[string]any{"type": "text", "ciphertext": ct("shh")})
	if got := e.quiet(); len(got) != 0 {
		t.Errorf("message pushed during quiet hours: %+v", got)
	}
	// Switching it off restores delivery.
	e.call("PUT", "/push/prefs", map[string]any{"quiet": map[string]any{"on": false, "start": start, "end": end, "tz_min": 0}}, b)
	e.clearPushDedupe()
	e.send(a, m, map[string]any{"type": "text", "ciphertext": ct("hello again")})
	if got := e.waitPush(1); len(got) != 1 {
		t.Errorf("not delivered after quiet hours off: %+v", got)
	}
}
