package integration

import (
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

type audioJoin struct {
	Room struct {
		ID string `json:"id"`
	} `json:"room"`
	Connect audioConnect `json:"connect"`
}

type audioConnect struct {
	Token      string `json:"token"`
	CanPublish bool   `json:"can_publish"`
	Me         struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	} `json:"me"`
}

type audioState struct {
	Room struct {
		Speakers  int `json:"speakers"`
		Listeners int `json:"listeners"`
	} `json:"room"`
	Members []struct {
		ID         string `json:"id"`
		Label      string `json:"label"`
		Role       string `json:"role"`
		HandRaised bool   `json:"hand_raised"`
		Me         bool   `json:"me"`
	} `json:"members"`
	Me struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	} `json:"me"`
}

func (e *env) openAudio(uid, title string, anonymous bool) audioJoin {
	e.t.Helper()
	c, b := e.call("POST", "/audio/rooms", map[string]any{"title": title, "anonymous": anonymous}, uid)
	if c != 201 {
		e.t.Fatalf("start audio room: %d %s", c, b)
	}
	var r audioJoin
	decodeInto(e.t, b, &r)
	return r
}

func (e *env) audioJoin(uid, room string) (int, audioConnect) {
	e.t.Helper()
	c, b := e.call("POST", "/audio/rooms/"+room+"/join", nil, uid)
	var j audioConnect
	if c == 200 {
		decodeInto(e.t, b, &j)
	}
	return c, j
}

func (e *env) audioState(uid, room string) (int, audioState) {
	e.t.Helper()
	c, b := e.call("GET", "/audio/rooms/"+room, nil, uid)
	var s audioState
	if c == 200 {
		decodeInto(e.t, b, &s)
	}
	return c, s
}

func tokenGrant(t *testing.T, tok string) (jwt.MapClaims, map[string]any) {
	t.Helper()
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(tok, claims, func(*jwt.Token) (any, error) { return []byte(liveSecret), nil },
		jwt.WithValidMethods([]string{"HS256"})); err != nil {
		t.Fatalf("token does not verify: %v", err)
	}
	return claims, claims["video"].(map[string]any)
}

func TestAudioStageIsControlledByTheHost(t *testing.T) {
	e := setup(t)
	host, guest, other := e.user("Hostess"), e.user("Guest"), e.user("Other")
	room := e.openAudio(host, "Sunday jazz talk", false)
	if !room.Connect.CanPublish || room.Connect.Me.Role != "host" {
		t.Fatalf("host connect: %+v", room.Connect)
	}

	c, g := e.audioJoin(guest, room.Room.ID)
	if c != 200 || g.CanPublish || g.Me.Role != "listener" {
		t.Fatalf("guest join: %d %+v", c, g)
	}
	// The LiveKit identity is the member id, never the user id.
	claims, grant := tokenGrant(t, g.Token)
	if claims["sub"] != g.Me.ID || claims["sub"] == guest || grant["canPublish"] != false || grant["room"] != room.Room.ID {
		t.Errorf("listener token: %v", claims)
	}

	// Listeners cannot promote anyone, including themselves.
	if c, _ := e.call("POST", "/audio/rooms/"+room.Room.ID+"/members/"+g.Me.ID, map[string]string{"action": "promote"}, guest); c != 404 {
		t.Errorf("listener promoted themselves: %d", c)
	}
	if c, _ := e.call("POST", "/audio/rooms/"+room.Room.ID+"/members/"+g.Me.ID, map[string]string{"action": "promote"}, other); c != 404 {
		t.Errorf("stranger promoted a member: %d", c)
	}

	// Raising a hand is visible; the host promotes; a fresh join now publishes.
	if c, _ := e.call("POST", "/audio/rooms/"+room.Room.ID+"/hand", map[string]bool{"up": true}, guest); c != 204 {
		t.Fatalf("raise hand: %d", c)
	}
	_, st := e.audioState(host, room.Room.ID)
	if len(st.Members) != 2 || !st.Members[1].HandRaised || st.Room.Listeners != 1 || st.Room.Speakers != 1 {
		t.Fatalf("state: %+v", st)
	}
	if c, b := e.call("POST", "/audio/rooms/"+room.Room.ID+"/members/"+g.Me.ID, map[string]string{"action": "promote"}, host); c != 204 {
		t.Fatalf("promote: %d %s", c, b)
	}
	_, g2 := e.audioJoin(guest, room.Room.ID)
	if !g2.CanPublish || g2.Me.Role != "speaker" || g2.Me.ID != g.Me.ID {
		t.Errorf("promoted guest: %+v", g2)
	}
	if _, grant := tokenGrant(t, g2.Token); grant["canPublish"] != true {
		t.Error("speaker token cannot publish")
	}
	if c, st := e.audioState(guest, room.Room.ID); c != 200 || st.Me.Role != "speaker" {
		t.Errorf("speaker state: %d %+v", c, st.Me)
	}

	// Demote takes the mic back; the host cannot be demoted or kicked.
	e.call("POST", "/audio/rooms/"+room.Room.ID+"/members/"+g.Me.ID, map[string]string{"action": "demote"}, host)
	if _, g3 := e.audioJoin(guest, room.Room.ID); g3.CanPublish {
		t.Error("demoted guest still publishes")
	}
	if c, _ := e.call("POST", "/audio/rooms/"+room.Room.ID+"/members/"+room.Connect.Me.ID, map[string]string{"action": "kick"}, host); c != 404 {
		t.Errorf("host kicked themselves: %d", c)
	}
}

func TestAudioStageHasLimits(t *testing.T) {
	e := setup(t)
	host := e.user("Host")
	room := e.openAudio(host, "Open mic", false)
	var ids []string
	for i := 0; i < 8; i++ {
		u := e.user("L" + string(rune('A'+i)))
		_, j := e.audioJoin(u, room.Room.ID)
		ids = append(ids, j.Me.ID)
	}
	for i, id := range ids[:7] {
		if c, b := e.call("POST", "/audio/rooms/"+room.Room.ID+"/members/"+id, map[string]string{"action": "promote"}, host); c != 204 {
			t.Fatalf("promote %d: %d %s", i, c, b)
		}
	}
	// 1 host + 7 speakers = a full stage of 8.
	if c, _ := e.call("POST", "/audio/rooms/"+room.Room.ID+"/members/"+ids[7], map[string]string{"action": "promote"}, host); c != 422 {
		t.Errorf("ninth speaker: %d", c)
	}
	// One room per host.
	if c, _ := e.call("POST", "/audio/rooms", map[string]any{"title": "Second room"}, host); c != 422 {
		t.Errorf("second room: %d", c)
	}
}

func TestAudioAnonymityRules(t *testing.T) {
	e := setup(t)
	open1, open2, shy := e.user("Opal"), e.user("Orion"), e.user("Shy")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)

	named := e.openAudio(open1, "Named chat", false)
	e.audioJoin(open2, named.Room.ID)
	e.audioJoin(shy, named.Room.ID)
	_, st := e.audioState(open1, named.Room.ID)
	labels := map[string]bool{}
	for _, m := range st.Members {
		labels[m.Label] = true
	}
	if !labels["Opal"] || !labels["Orion"] || labels["Shy"] || len(st.Members) != 3 {
		t.Errorf("named room labels: %v", labels)
	}

	// An Incognito host can only run an anonymous lounge.
	lounge := e.openAudio(shy, "Late night lounge", false)
	e.audioJoin(open1, lounge.Room.ID)
	_, ls := e.audioState(shy, lounge.Room.ID)
	for _, m := range ls.Members {
		if m.Label == "Opal" || m.Label == "Shy" || !strings.Contains(m.Label, " ") {
			t.Errorf("lounge leaked a name: %q", m.Label)
		}
	}
	// An Open-mode person in an anonymous room is also an alias.
	anon := e.openAudio(open2, "Confessions", true)
	_, as := e.audioState(open2, anon.Room.ID)
	if as.Members[0].Label == "Orion" {
		t.Error("anonymous room showed the host's name")
	}
}

func TestAudioAccessRules(t *testing.T) {
	e := setup(t)
	host := e.user("Host")
	unverified := e.newUser("user", false, "Unv")
	hotel := e.user("Hotel")
	e.exec(`UPDATE profiles SET mode='hotel' WHERE user_id=$1`, hotel)
	banned := e.user("Banned")
	blocked := e.user("Blocked")
	kicked := e.user("Kicked")

	if c, _ := e.call("POST", "/audio/rooms", map[string]any{"title": "Nope nope"}, unverified); c != 403 {
		t.Errorf("unverified host: %d", c)
	}
	if c, _ := e.call("POST", "/audio/rooms", map[string]any{"title": "Nope nope"}, hotel); c != 403 {
		t.Errorf("hotel-mode host: %d", c)
	}
	e.exec(`UPDATE users SET is_banned=true WHERE id=$1`, banned)
	if c, _ := e.call("POST", "/audio/rooms", map[string]any{"title": "Nope nope"}, banned); c != 401 && c != 403 {
		t.Errorf("banned host: %d", c)
	}
	if c, _ := e.call("POST", "/audio/rooms", map[string]any{"title": "x"}, host); c != 422 {
		t.Errorf("short title: %d", c)
	}

	room := e.openAudio(host, "Everyone welcome", false)
	if c, _ := e.audioJoin(hotel, room.Room.ID); c != 403 {
		t.Errorf("hotel-mode join: %d", c)
	}
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(blocked)}, host)
	if c, _ := e.audioJoin(blocked, room.Room.ID); c != 404 {
		t.Errorf("blocked join: %d", c)
	}
	if _, b := e.call("GET", "/audio/rooms", nil, blocked); strings.Contains(string(b), room.Room.ID) {
		t.Error("blocked person sees the room")
	}

	// Kicked people stay out.
	_, kj := e.audioJoin(kicked, room.Room.ID)
	if c, _ := e.call("POST", "/audio/rooms/"+room.Room.ID+"/members/"+kj.Me.ID, map[string]string{"action": "kick"}, host); c != 204 {
		t.Fatalf("kick: %d", c)
	}
	if c, _ := e.audioJoin(kicked, room.Room.ID); c != 404 {
		t.Errorf("kicked rejoin: %d", c)
	}
	if c, _ := e.audioState(kicked, room.Room.ID); c != 404 {
		t.Errorf("kicked state: %d", c)
	}
}

func TestAudioRoomEnds(t *testing.T) {
	e := setup(t)
	host, guest := e.user("Host"), e.user("Guest")
	room := e.openAudio(host, "Short chat", false)
	e.audioJoin(guest, room.Room.ID)

	if c, _ := e.call("DELETE", "/audio/rooms/"+room.Room.ID, nil, guest); c != 404 {
		t.Errorf("guest ended the room: %d", c)
	}
	// A host leaving ends the room for everyone.
	if c, _ := e.call("POST", "/audio/rooms/"+room.Room.ID+"/leave", nil, host); c != 204 {
		t.Fatalf("host leave: %d", c)
	}
	if c, _ := e.audioState(guest, room.Room.ID); c != 410 {
		t.Errorf("state after end: %d", c)
	}
	if c, _ := e.audioJoin(guest, room.Room.ID); c != 410 {
		t.Errorf("join after end: %d", c)
	}
	if c, _ := e.call("DELETE", "/audio/rooms/"+room.Room.ID, nil, host); c != 204 {
		t.Errorf("end is not idempotent: %d", c)
	}

	// Members who stop polling drop out; a room without its host is swept.
	r2 := e.openAudio(host, "Second chat", false)
	_, g := e.audioJoin(guest, r2.Room.ID)
	e.exec(`UPDATE audio_members SET last_seen = NOW() - INTERVAL '5 minutes' WHERE id=$1::uuid`, g.Me.ID)
	e.call("GET", "/audio/rooms", nil, host) // listing sweeps
	if _, st := e.audioState(host, r2.Room.ID); st.Room.Listeners != 0 {
		t.Errorf("stale listener still present: %+v", st.Room)
	}
	e.exec(`UPDATE audio_members SET last_seen = NOW() - INTERVAL '5 minutes' WHERE room_id=$1::uuid`, r2.Room.ID)
	e.call("GET", "/audio/rooms", nil, guest)
	if c, _ := e.audioState(host, r2.Room.ID); c != 404 && c != 410 {
		t.Errorf("hostless room still live: %d", c)
	}
}
