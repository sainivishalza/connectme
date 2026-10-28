package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func (e *env) roomTopics(uid, query string) map[string]string {
	e.t.Helper()
	c, b := e.call("GET", "/live/rooms"+query, nil, uid)
	if c != 200 {
		e.t.Fatalf("rooms: %d %s", c, b)
	}
	var out struct {
		Rooms []struct {
			ID    string `json:"id"`
			Topic string `json:"topic"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		e.t.Fatal(err)
	}
	m := map[string]string{}
	for _, r := range out.Rooms {
		m[r.ID] = r.Topic
	}
	return m
}

func TestLiveRoomsHaveTopicsAndHostsCanPinOneLine(t *testing.T) {
	e := setup(t)
	music, chat, viewer, other := e.user("Music Host"), e.user("Chat Host"), e.user("Room Viewer"), e.user("Other Viewer")

	if c, _ := e.call("POST", "/live/rooms", map[string]string{"title": "Bad topic room", "topic": "weapons"}, music); c != 422 {
		t.Errorf("unknown topic: %d", c)
	}
	c, b := e.call("POST", "/live/rooms", map[string]string{"title": "Acoustic night", "topic": "music"}, music)
	if c != 201 || !strings.Contains(string(b), `"topic":"music"`) {
		t.Fatalf("start with topic: %d %s", c, b)
	}
	var started liveRoom
	decodeInto(t, b, &started)
	room := started.Room.ID
	other2 := e.goLive(chat, "Just talking") // topic defaults to chat

	// Filter by topic and by who you follow.
	if got := e.roomTopics(viewer, "?topic=music"); len(got) < 1 || got[room] != "music" || got[other2.Room.ID] != "" {
		t.Errorf("topic filter: %v", got)
	}
	if c, _ := e.call("GET", "/live/rooms?topic=nonsense", nil, viewer); c != 422 {
		t.Errorf("bad topic filter: %d", c)
	}
	e.call("POST", "/follows/"+e.refOf(chat), nil, viewer)
	if got := e.roomTopics(viewer, "?following=1"); len(got) != 1 || got[other2.Room.ID] != "chat" {
		t.Errorf("following filter: %v", got)
	}

	// Only the host can pin; everyone watching sees it; it is plain text of limited length.
	if c, _ := e.call("PUT", "/live/rooms/"+room+"/pin", map[string]string{"text": "Be kind"}, viewer); c != 404 {
		t.Errorf("viewer pinning: %d", c)
	}
	if c, _ := e.call("PUT", "/live/rooms/"+room+"/pin", map[string]string{"text": strings.Repeat("x", 121)}, music); c != 422 {
		t.Errorf("too long: %d", c)
	}
	if c, _ := e.call("PUT", "/live/rooms/"+room+"/pin", map[string]string{"text": "Requests open: tell us your favourite song"}, music); c != 204 {
		t.Fatalf("pin: %d", c)
	}
	_, b = e.call("GET", "/live/rooms/"+room+"/events", nil, other)
	if !strings.Contains(string(b), `"pinned":"Requests open: tell us your favourite song"`) {
		t.Errorf("viewer does not see the pin: %s", b)
	}
	if c, _ := e.call("PUT", "/live/rooms/"+room+"/pin", map[string]string{"text": ""}, music); c != 204 {
		t.Fatalf("unpin: %d", c)
	}
	_, b = e.call("GET", "/live/rooms/"+room+"/events", nil, other)
	if !strings.Contains(string(b), `"pinned":""`) {
		t.Errorf("pin not cleared: %s", b)
	}
}

func TestLiveRoomThemes(t *testing.T) {
	e := setup(t)
	host, viewer := e.user("Theme Host"), e.user("Theme Viewer")
	if c, _ := e.call("POST", "/live/rooms", map[string]string{"title": "Bad theme", "theme": "rainbow"}, host); c != 422 {
		t.Errorf("unknown theme: %d", c)
	}
	c, b := e.call("POST", "/live/rooms", map[string]string{"title": "Velvet lounge", "theme": "velvet"}, host)
	if c != 201 || !strings.Contains(string(b), `"theme":"velvet"`) {
		t.Fatalf("start: %d %s", c, b)
	}
	_, b = e.call("GET", "/live/rooms", nil, viewer)
	if !strings.Contains(string(b), `"theme":"velvet"`) || !strings.Contains(string(b), `"themes"`) {
		t.Errorf("list: %s", b)
	}
	// Default is plain.
	e.call("DELETE", "/live/rooms/"+strings.Split(strings.Split(string(b), `"id":"`)[1], `"`)[0], nil, host)
	c, b = e.call("POST", "/live/rooms", map[string]string{"title": "No theme chosen"}, host)
	if c != 201 || !strings.Contains(string(b), `"theme":"plain"`) {
		t.Errorf("default theme: %d %s", c, b)
	}
}

func TestLivePollsAreHostRunVotedOnceAndShownLive(t *testing.T) {
	e := setup(t)
	host, co, v1, v2 := e.user("Poll Host"), e.user("Poll Co"), e.user("Poll Viewer One"), e.user("Poll Viewer Two")
	room := e.goLive(host, "Game night").Room.ID
	e.call("POST", "/live/rooms/"+room+"/cohosts", map[string]string{"ref": e.refOf(co), "action": "add"}, host)
	base := "/live/rooms/" + room + "/poll"

	// Only the host or a co-host runs a poll; answers are 2 to 4.
	if c, _ := e.call("POST", base, map[string]any{"question": "Pizza or tacos?", "options": []string{"Pizza", "Tacos"}}, v1); c != 404 {
		t.Errorf("viewer starting: %d", c)
	}
	if c, _ := e.call("POST", base, map[string]any{"question": "Only one?", "options": []string{"Solo"}}, host); c != 422 {
		t.Errorf("one answer: %d", c)
	}
	c, b := e.call("POST", base, map[string]any{"question": "Pizza or tacos?", "options": []string{"Pizza", "Tacos"}}, co)
	if c != 201 || !strings.Contains(string(b), `"question":"Pizza or tacos?"`) {
		t.Fatalf("co-host starts: %d %s", c, b)
	}
	var p struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(b, &p)
	if c, _ := e.call("POST", base, map[string]any{"question": "Second?", "options": []string{"A", "B"}}, host); c != 422 {
		t.Errorf("a second poll while one runs: %d", c)
	}

	// Votes: once each, valid choices only, totals live for everyone.
	vote := func(u string, choice int) int {
		c, _ := e.call("POST", base+"/vote", map[string]any{"poll_id": p.ID, "choice": choice}, u)
		return c
	}
	if c := vote(v1, 5); c != 422 {
		t.Errorf("out of range: %d", c)
	}
	if c := vote(v1, 1); c != 200 {
		t.Fatalf("vote: %d", c)
	}
	if c := vote(v1, 0); c != 200 {
		t.Errorf("a repeat vote is accepted quietly: %d", c)
	}
	vote(v2, 1)
	_, ev := e.call("GET", "/live/rooms/"+room+"/events", nil, v2)
	if !strings.Contains(string(ev), `"counts":[0,2]`) || !strings.Contains(string(ev), `"my_choice":1`) {
		t.Errorf("events poll: %s", ev)
	}
	if strings.Contains(string(ev), v1) {
		t.Error("who voted is exposed")
	}

	// Closing ends voting but keeps the result visible for a moment.
	if c, _ := e.call("DELETE", base, nil, v1); c != 404 {
		t.Errorf("viewer closing: %d", c)
	}
	if c, _ := e.call("DELETE", base, nil, host); c != 204 {
		t.Fatalf("close: %d", c)
	}
	if c := vote(e.user("Late Viewer"), 0); c != 422 {
		t.Errorf("voting after close: %d", c)
	}
	_, ev = e.call("GET", "/live/rooms/"+room+"/events", nil, v1)
	if !strings.Contains(string(ev), `"closed":true`) {
		t.Errorf("result after close: %s", ev)
	}
}
