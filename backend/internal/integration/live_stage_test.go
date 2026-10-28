package integration

import (
	"strings"
	"testing"
)

func TestViewersCanRequestToJoinAndTheHostCanInviteSeveralPeople(t *testing.T) {
	e := setup(t)
	host, a, b, c, shy := e.user("Stage Host"), e.user("Guest A"), e.user("Guest B"), e.user("Guest C"), e.user("Shy Guest")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)
	room := e.goLive(host, "Open stage").Room.ID
	base := "/live/rooms/" + room
	post := func(who, path string, body any) int {
		code, _ := e.call("POST", base+path, body, who)
		return code
	}
	state := func(who string) string {
		_, body := e.call("GET", base+"/events?after=0", nil, who)
		return string(body)
	}

	// A viewer raises a hand; the host sees it, the viewer sees "requested".
	if code := post(a, "/stage/request", nil); code != 204 {
		t.Fatalf("request: %d", code)
	}
	if s := state(a); !strings.Contains(s, `"me":"requested"`) || strings.Contains(s, "Guest A") {
		t.Errorf("viewer state: %s", s)
	}
	if s := state(host); !strings.Contains(s, `"name":"Guest A"`) || !strings.Contains(s, `"me":"on_stage"`) {
		t.Errorf("host state: %s", s)
	}
	// A hidden (anonymous) member cannot go on camera; the host cannot ask themselves.
	if code := post(shy, "/stage/request", nil); code != 422 {
		t.Errorf("anonymous request: %d", code)
	}
	if code := post(host, "/stage/request", nil); code != 422 {
		t.Errorf("host request: %d", code)
	}
	// Only people who run the room can decide.
	if code := post(b, "/stage/decide", map[string]any{"ref": e.refOf(a), "accept": true}); code != 404 {
		t.Errorf("viewer deciding: %d", code)
	}
	if code := post(host, "/stage/decide", map[string]any{"ref": e.refOf(a), "accept": true}); code != 204 {
		t.Fatalf("accept: %d", code)
	}
	if code, body := e.call("POST", base+"/join", nil, a); code != 200 || !strings.Contains(string(body), `"can_publish":true`) {
		t.Errorf("accepted guest joins as publisher: %d %s", code, body)
	}
	if code := post(host, "/stage/decide", map[string]any{"ref": e.refOf(a), "accept": true}); code != 404 {
		t.Errorf("deciding twice: %d", code)
	}

	// The host invites two more people at once; each answers for themselves.
	for _, u := range []string{b, c} {
		if code := post(host, "/stage/invite", map[string]any{"ref": e.refOf(u)}); code != 204 {
			t.Fatalf("invite: %d", code)
		}
	}
	if code := post(host, "/stage/invite", map[string]any{"ref": e.refOf(shy)}); code != 404 {
		t.Errorf("inviting an anonymous member: %d", code)
	}
	if s := state(b); !strings.Contains(s, `"me":"invited"`) {
		t.Errorf("invited state: %s", s)
	}
	if code := post(b, "/stage/respond", map[string]any{"accept": true}); code != 204 {
		t.Fatalf("accept invite: %d", code)
	}
	if code := post(c, "/stage/respond", map[string]any{"accept": false}); code != 204 {
		t.Fatalf("decline invite: %d", code)
	}
	if code := post(c, "/stage/respond", map[string]any{"accept": true}); code != 404 {
		t.Errorf("answering a withdrawn invite: %d", code)
	}
	if s := state(host); !strings.Contains(s, `"guests":2`) {
		t.Errorf("two guests on stage: %s", s)
	}

	// Asking after being invited accepts the invite; leaving puts you back among viewers.
	if code := post(host, "/stage/invite", map[string]any{"ref": e.refOf(c)}); code != 204 {
		t.Fatalf("re-invite: %d", code)
	}
	if code := post(c, "/stage/request", nil); code != 204 {
		t.Fatalf("request after invite: %d", code)
	}
	if s := state(c); !strings.Contains(s, `"me":"on_stage"`) {
		t.Errorf("c should be on stage: %s", s)
	}
	if code := post(c, "/stage/leave", nil); code != 204 {
		t.Fatalf("leave: %d", code)
	}
	if code, body := e.call("POST", base+"/join", nil, c); code != 200 || strings.Contains(string(body), `"can_publish":true`) {
		t.Errorf("left guest rejoins as viewer: %d %s", code, body)
	}

	// A blocked pair cannot be put on stage together.
	d := e.user("Blocked Guest")
	e.call("POST", "/moderation/blocks", map[string]any{"ref": e.refOf(host)}, d)
	if code := post(host, "/stage/invite", map[string]any{"ref": e.refOf(d)}); code != 404 {
		t.Errorf("inviting someone who blocked the host: %d", code)
	}
}
