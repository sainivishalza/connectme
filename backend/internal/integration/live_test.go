package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

const liveSecret = "livekit-test-secret-at-least-32-bytes!!"

type liveRoom struct {
	Room struct {
		ID string `json:"id"`
	} `json:"room"`
	Connect struct {
		Token      string `json:"token"`
		CanPublish bool   `json:"can_publish"`
	} `json:"connect"`
}

func (e *env) goLive(uid, title string) liveRoom {
	e.t.Helper()
	c, b := e.call("POST", "/live/rooms", map[string]string{"title": title}, uid)
	if c != 201 {
		e.t.Fatalf("go live: %d %s", c, b)
	}
	var r liveRoom
	decodeInto(e.t, b, &r)
	return r
}

func TestLiveTokenIsScopedToRoomAndRole(t *testing.T) {
	e := setup(t)
	host, viewer := e.user("Host"), e.user("Viewer")
	room := e.goLive(host, "Friday night chat")
	if !room.Connect.CanPublish {
		t.Error("host cannot publish")
	}
	c, b := e.call("POST", "/live/rooms/"+room.Room.ID+"/join", nil, viewer)
	if c != 200 {
		t.Fatalf("join: %d %s", c, b)
	}
	var j struct {
		Token      string `json:"token"`
		CanPublish bool   `json:"can_publish"`
	}
	decodeInto(t, b, &j)
	if j.CanPublish {
		t.Error("viewer was granted publish")
	}
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(j.Token, claims, func(*jwt.Token) (any, error) { return []byte(liveSecret), nil },
		jwt.WithValidMethods([]string{"HS256"})); err != nil {
		t.Fatalf("token does not verify: %v", err)
	}
	grant := claims["video"].(map[string]any)
	if grant["room"] != room.Room.ID || grant["canPublish"] != false || claims["sub"] != viewer {
		t.Errorf("token not scoped: %v", claims)
	}
}

func TestLiveHostingRules(t *testing.T) {
	e := setup(t)
	unverified := e.newUser("user", false, "Unv")
	incog := e.user("Incog")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, incog)
	banned := e.user("Banned")
	e.exec(`UPDATE users SET is_banned=true WHERE id=$1`, banned)
	for name, u := range map[string]string{"unverified": unverified, "incognito": incog, "banned": banned} {
		if c, _ := e.call("POST", "/live/rooms", map[string]string{"title": "My stream"}, u); c != 403 && c != 401 {
			t.Errorf("%s user went live: %d", name, c)
		}
	}
	ok := e.user("Ok")
	e.goLive(ok, "First stream")
	if c, _ := e.call("POST", "/live/rooms", map[string]string{"title": "Second stream"}, ok); c != 422 {
		t.Errorf("second concurrent room: %d", c)
	}
	if c, _ := e.call("POST", "/live/rooms", map[string]string{"title": "x"}, e.user("Short")); c != 422 {
		t.Errorf("short title accepted: %d", c)
	}
}

func TestLiveBlockedUsersCannotSeeOrJoin(t *testing.T) {
	e := setup(t)
	host, viewer := e.user("Host"), e.user("Viewer")
	room := e.goLive(host, "Evening stream")
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(viewer)}, host)
	if c, _ := e.call("POST", "/live/rooms/"+room.Room.ID+"/join", nil, viewer); c != 404 {
		t.Errorf("blocked viewer joined: %d", c)
	}
	_, b := e.call("GET", "/live/rooms", nil, viewer)
	if strings.Contains(string(b), room.Room.ID) {
		t.Error("blocked viewer sees the room in the list")
	}
}

func TestLiveEndCutsOffNewJoinsAndIsHostOnly(t *testing.T) {
	e := setup(t)
	host, other := e.user("Host"), e.user("Other")
	room := e.goLive(host, "Short stream")
	if c, _ := e.call("DELETE", "/live/rooms/"+room.Room.ID, nil, other); c != 404 {
		t.Errorf("non-host ended the room: %d", c)
	}
	if c, _ := e.call("DELETE", "/live/rooms/"+room.Room.ID, nil, host); c != 204 {
		t.Errorf("host end: %d", c)
	}
	if c, _ := e.call("DELETE", "/live/rooms/"+room.Room.ID, nil, host); c != 204 {
		t.Errorf("end not idempotent: %d", c)
	}
	if c, _ := e.call("POST", "/live/rooms/"+room.Room.ID+"/join", nil, other); c != 410 {
		t.Errorf("join after end: %d", c)
	}
}

func TestLiveBanEndsRoom(t *testing.T) {
	e := setup(t)
	host, viewer := e.user("Host"), e.user("Viewer")
	room := e.goLive(host, "Soon banned")
	e.exec(`UPDATE users SET is_banned=true WHERE id=$1`, host)
	if c, _ := e.call("POST", "/live/rooms/"+room.Room.ID+"/join", nil, viewer); c != 410 {
		t.Errorf("join banned host's room: %d", c)
	}
	_, b := e.call("GET", "/live/rooms", nil, viewer)
	if strings.Contains(string(b), room.Room.ID) {
		t.Error("banned host's room still listed")
	}
}

func TestLiveGiftSplitIsIdempotentAndCannotOverdraw(t *testing.T) {
	e := setup(t)
	host, fan, poor := e.user("Host"), e.user("Fan"), e.user("Poor")
	room := e.goLive(host, "Gift stream")
	e.grant(fan, 100)
	path := "/live/rooms/" + room.Room.ID + "/gift"
	req := map[string]string{"gift_id": "champagne", "request_id": "req-0001-abcd"} // 50 coins

	if c, b := e.call("POST", path, req, fan); c != 200 {
		t.Fatalf("gift: %d %s", c, b)
	}
	if got := e.coins(fan); got != 50 {
		t.Errorf("fan balance %d, want 50", got)
	}
	if got := e.coins(host); got != 35 { // 70% of 50
		t.Errorf("host got %d, want 35", got)
	}
	// Retrying the same request must not charge again.
	e.call("POST", path, req, fan)
	if got := e.coins(fan); got != 50 {
		t.Errorf("replay charged again: %d", got)
	}
	if n := e.count(`SELECT COUNT(*) FROM live_gifts WHERE room_id=$1::uuid`, room.Room.ID); n != 1 {
		t.Errorf("gift rows %d", n)
	}
	// Insufficient funds: 402, and nothing recorded.
	if c, _ := e.call("POST", path, map[string]string{"gift_id": "rose", "request_id": "req-0002-abcd"}, poor); c != 402 {
		t.Errorf("poor viewer: %d", c)
	}
	if n := e.count(`SELECT COUNT(*) FROM live_gifts WHERE from_user=$1`, poor); n != 0 {
		t.Error("failed gift left a row behind")
	}
	// No self-gifting.
	e.grant(host, 100)
	if c, _ := e.call("POST", path, map[string]string{"gift_id": "rose", "request_id": "req-0003-abcd"}, host); c != 403 {
		t.Errorf("self gift: %d", c)
	}
	// Unknown gift and malformed ids.
	if c, _ := e.call("POST", path, map[string]string{"gift_id": "nope", "request_id": "req-0004-abcd"}, fan); c != 404 {
		t.Errorf("unknown gift: %d", c)
	}
	if c, _ := e.call("POST", fmt.Sprintf("/live/rooms/%s/gift", "not-a-uuid"), req, fan); c != 404 {
		t.Errorf("bad room id: %d", c)
	}
}

type liveEvents struct {
	Events []struct {
		ID   int64  `json:"id"`
		Kind string `json:"kind"`
		Name string `json:"name"`
		Text string `json:"text"`
	} `json:"events"`
}

func TestLiveChatAndGiftEventsAreVisibleBlockAwareAndAnonymised(t *testing.T) {
	e := setup(t)
	host, fan, shy, blocked := e.user("Host"), e.user("Fan"), e.user("Shy"), e.user("Blocked")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)
	room := e.goLive(host, "Chatty stream")
	base := "/live/rooms/" + room.Room.ID

	post := func(uid, text string) int {
		c, _ := e.call("POST", base+"/chat", map[string]string{"text": text}, uid)
		return c
	}
	if c := post(fan, "hello everyone"); c != 204 {
		t.Fatalf("chat: %d", c)
	}
	if c := post(shy, "psst"); c != 204 {
		t.Fatalf("incognito chat: %d", c)
	}
	if c := post(blocked, "I am blocked by the viewer"); c != 204 {
		t.Fatalf("blocked chat: %d", c)
	}
	e.grant(fan, 20)
	e.call("POST", base+"/gift", map[string]string{"gift_id": "rose", "request_id": "chat-gift-0001"}, fan)

	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(blocked)}, host)
	_, b := e.call("GET", base+"/events", nil, host)
	var ev liveEvents
	decodeInto(t, b, &ev)
	var texts []string
	for _, x := range ev.Events {
		texts = append(texts, x.Name+": "+x.Text)
		if x.Text == "psst" && x.Name != "Someone" {
			t.Errorf("incognito sender exposed as %q", x.Name)
		}
		if strings.Contains(x.Text, "blocked by the viewer") {
			t.Error("blocked user's chat visible to the blocker")
		}
	}
	if len(ev.Events) < 3 {
		t.Fatalf("events: %v", texts)
	}
	last := ev.Events[len(ev.Events)-1]
	if last.Kind != "gift" || last.Text != "sent a Rose" {
		t.Errorf("gift not announced: %+v", last)
	}
	// Cursor: nothing newer than the last id.
	_, b = e.call("GET", fmt.Sprintf("%s/events?after=%d", base, last.ID), nil, host)
	var none liveEvents
	decodeInto(t, b, &none)
	if len(none.Events) != 0 {
		t.Errorf("cursor returned %d events", len(none.Events))
	}
}

func TestLiveChatValidationAndAccess(t *testing.T) {
	e := setup(t)
	host, fan := e.user("Host"), e.user("Fan")
	room := e.goLive(host, "Rules stream")
	base := "/live/rooms/" + room.Room.ID + "/chat"
	for name, text := range map[string]string{"empty": "   ", "long": strings.Repeat("a", 201), "control": "hi\x00there"} {
		if c, _ := e.call("POST", base, map[string]string{"text": text}, fan); c != 422 {
			t.Errorf("%s message: %d", name, c)
		}
	}
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(fan)}, host)
	if c, _ := e.call("POST", base, map[string]string{"text": "hi"}, fan); c != 404 {
		t.Errorf("blocked viewer chatted: %d", c)
	}
	unv := e.newUser("user", false, "Unv")
	if c, _ := e.call("POST", base, map[string]string{"text": "hi"}, unv); c != 403 && c != 401 {
		t.Errorf("unverified chatted: %d", c)
	}
}

func TestStaffCanForceEndStreamWithAuditedReason(t *testing.T) {
	e := setup(t)
	host, viewer := e.user("Host"), e.user("Viewer")
	mod := e.newUser("moderator", true, "Mod")
	room := e.goLive(host, "Rule breaking stream")
	path := "/moderation/live/" + room.Room.ID + "/end"

	if c, _ := e.call("POST", path, map[string]string{"reason": "harassment"}, viewer); c != 403 {
		t.Errorf("ordinary user force-ended: %d", c)
	}
	if c, _ := e.callAs("POST", path, map[string]string{"reason": "no"}, mod, "moderator"); c != 422 {
		t.Errorf("short reason: %d", c)
	}
	if c, b := e.callAs("POST", path, map[string]string{"reason": "Harassing viewers on stream"}, mod, "moderator"); c != 204 {
		t.Fatalf("force end: %d %s", c, b)
	}
	if c, _ := e.call("POST", "/live/rooms/"+room.Room.ID+"/join", nil, viewer); c != 410 {
		t.Errorf("join after force end: %d", c)
	}
	if n := e.count(`SELECT COUNT(*) FROM staff_audit_log WHERE action='end_stream' AND target_user_id=$1`, host); n != 1 {
		t.Errorf("audit entries %d", n)
	}
}

func TestBanningHostEndsRoomImmediately(t *testing.T) {
	e := setup(t)
	host, viewer := e.user("Host"), e.user("Viewer")
	mod := e.newUser("moderator", true, "Mod")
	room := e.goLive(host, "About to be banned")
	c, b := e.callAs("POST", "/moderation/users/"+e.refOf(host)+"/ban", map[string]string{"reason": "Scam streams"}, mod, "moderator")
	if c != 204 {
		t.Fatalf("ban: %d %s", c, b)
	}
	if n := e.count(`SELECT COUNT(*) FROM live_rooms WHERE id=$1::uuid AND status='ended'`, room.Room.ID); n != 1 {
		t.Error("room still live after ban")
	}
	_ = viewer
}

func TestHostCanMuteAndKickViewers(t *testing.T) {
	e := setup(t)
	host, loud, rude, other := e.user("Host"), e.user("Loud"), e.user("Rude"), e.user("Other")
	room := e.goLive(host, "Moderated stream")
	base := "/live/rooms/" + room.Room.ID
	mod := func(uid, target, action string) int {
		c, _ := e.call("POST", base+"/moderate", map[string]string{"ref": e.refOf(target), "action": action}, uid)
		return c
	}

	if c := mod(other, loud, "mute"); c != 404 {
		t.Errorf("non-host moderated: %d", c)
	}
	if c := mod(host, host, "mute"); c != 422 {
		t.Errorf("self-moderation: %d", c)
	}
	if c := mod(host, loud, "ban"); c != 422 {
		t.Errorf("unknown action: %d", c)
	}

	if c, _ := e.call("POST", base+"/chat", map[string]string{"text": "first"}, loud); c != 204 {
		t.Fatalf("pre-mute chat: %d", c)
	}
	if c := mod(host, loud, "mute"); c != 204 {
		t.Fatalf("mute: %d", c)
	}
	if c, _ := e.call("POST", base+"/chat", map[string]string{"text": "again"}, loud); c != 403 {
		t.Errorf("muted viewer chatted: %d", c)
	}
	if c, _ := e.call("POST", base+"/join", nil, loud); c != 200 {
		t.Errorf("muted viewer should still watch: %d", c)
	}

	if c := mod(host, rude, "kick"); c != 204 {
		t.Fatalf("kick: %d", c)
	}
	if c, _ := e.call("POST", base+"/join", nil, rude); c != 404 {
		t.Errorf("kicked viewer rejoined: %d", c)
	}
	_, b := e.call("GET", "/live/rooms", nil, rude)
	if strings.Contains(string(b), room.Room.ID) {
		t.Log("kicked viewer can still see the room in the list (acceptable; join is refused)")
	}
	// A mute must not downgrade a kick.
	mod(host, rude, "mute")
	if c, _ := e.call("POST", base+"/join", nil, rude); c != 404 {
		t.Errorf("mute downgraded a kick: %d", c)
	}
	// Events expose a ref the host can act on.
	_, b = e.call("GET", base+"/events", nil, host)
	if !strings.Contains(string(b), e.refOf(loud)) {
		t.Error("events do not carry the sender ref")
	}
}
