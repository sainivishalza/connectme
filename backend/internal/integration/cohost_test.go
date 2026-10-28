package integration

import (
	"strings"
	"testing"
)

func TestCohostsShareTheRoomButCannotEndItOrModerateTheHost(t *testing.T) {
	e := setup(t)
	host, co, co2, co3, viewer, shy := e.user("Main Host"), e.user("Co Host"), e.user("Co Host Two"), e.user("Co Host Three"), e.user("Room Viewer"), e.user("Shy Person")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)
	room := e.goLive(host, "Shared show").Room.ID
	add := func(who, ref string, action string) int {
		c, _ := e.call("POST", "/live/rooms/"+room+"/cohosts", map[string]string{"ref": ref, "action": action}, who)
		return c
	}

	// Only the host can add; not yourself, not an anonymous member, not a stranger's ref.
	if c := add(viewer, e.refOf(co), "add"); c != 404 {
		t.Errorf("viewer adding: %d", c)
	}
	if c := add(host, e.refOf(host), "add"); c != 422 {
		t.Errorf("self: %d", c)
	}
	if c := add(host, e.refOf(shy), "add"); c != 404 {
		t.Errorf("anonymous member: %d", c)
	}
	if c := add(host, e.refOf(co), "bogus"); c != 422 {
		t.Errorf("bad action: %d", c)
	}
	for _, u := range []string{co, co2} {
		if c := add(host, e.refOf(u), "add"); c != 204 {
			t.Fatalf("add: %d", c)
		}
	}
	if c := add(host, e.refOf(co), "add"); c != 204 {
		t.Errorf("adding twice is idempotent: %d", c)
	}
	for i := 0; i < 5; i++ { // seven guests in all
		if c := add(host, e.refOf(e.user("Filler Guest")), "add"); c != 204 {
			t.Fatalf("guest %d: %d", i+3, c)
		}
	}
	if c := add(host, e.refOf(co3), "add"); c != 422 {
		t.Errorf("eighth guest: %d", c)
	}

	// A co-host joins with a publish token, can pin and moderate, and is listed.
	c, b := e.call("POST", "/live/rooms/"+room+"/join", nil, co)
	if c != 200 || !strings.Contains(string(b), `"can_publish":true`) || !strings.Contains(string(b), `"cohost":true`) {
		t.Errorf("co-host join: %d %s", c, b)
	}
	if c, b := e.call("POST", "/live/rooms/"+room+"/join", nil, viewer); c != 200 || strings.Contains(string(b), `"can_publish":true`) {
		t.Errorf("viewer join: %d %s", c, b)
	}
	if c, _ := e.call("PUT", "/live/rooms/"+room+"/pin", map[string]string{"text": "Co-host says hi"}, co); c != 204 {
		t.Errorf("co-host pin: %d", c)
	}
	if c, _ := e.call("POST", "/live/rooms/"+room+"/moderate", map[string]string{"ref": e.refOf(viewer), "action": "mute"}, co); c != 204 {
		t.Errorf("co-host mute: %d", c)
	}
	if c, _ := e.call("POST", "/live/rooms/"+room+"/moderate", map[string]string{"ref": e.refOf(host), "action": "kick"}, co); c != 422 {
		t.Errorf("co-host moderating the host: %d", c)
	}
	if c, _ := e.call("DELETE", "/live/rooms/"+room, nil, co); c == 204 {
		t.Errorf("a co-host ended the room")
	}
	if c, _ := add(co, e.refOf(co3), "add"), 0; c != 404 {
		t.Errorf("co-host adding another: %d", c)
	}
	_, b = e.call("GET", "/live/rooms/"+room+"/events", nil, viewer)
	if !strings.Contains(string(b), "Co Host") || !strings.Contains(string(b), "Co-host says hi") {
		t.Errorf("events lack co-hosts/pin: %s", b)
	}

	// Removing takes the rights away.
	if c := add(host, e.refOf(co), "remove"); c != 204 {
		t.Fatalf("remove: %d", c)
	}
	if c, _ := e.call("PUT", "/live/rooms/"+room+"/pin", map[string]string{"text": "still here?"}, co); c != 404 {
		t.Errorf("removed co-host pinning: %d", c)
	}
	if c, b := e.call("POST", "/live/rooms/"+room+"/join", nil, co); c != 200 || strings.Contains(string(b), `"can_publish":true`) {
		t.Errorf("removed co-host join: %d %s", c, b)
	}
}
