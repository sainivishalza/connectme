package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type grp struct {
	ID          string `json:"id"`
	MemberCount int    `json:"member_count"`
	Ready       bool   `json:"ready"`
	Locked      bool   `json:"locked"`
}

// located returns a verified user placed at (lat+dLat, lng).
func (e *env) located(name string, lat, lng float64) string {
	u := e.user(name)
	e.locate(u, lat, lng)
	return u
}

func (e *env) newGroup(owner, typ, activity string, size int) (int, grp) {
	body := map[string]any{"name": "Crew " + typ, "type": typ, "activity": activity}
	if size > 0 {
		body["target_size"] = size
	}
	c, b := e.call("POST", "/groups", body, owner)
	var g grp
	_ = json.Unmarshal(b, &g)
	return c, g
}

// join makes `friend` a member of group g (invite + accept through the API).
func (e *env) join(owner, friend, groupID string) {
	e.t.Helper()
	if c, b := e.call("POST", "/groups/"+groupID+"/invite", map[string]string{"ref": e.refOf(friend)}, owner); c != 204 {
		e.t.Fatalf("invite: %d %s", c, b)
	}
	if c, b := e.call("POST", "/groups/"+groupID+"/join", nil, friend); c != 204 {
		e.t.Fatalf("join: %d %s", c, b)
	}
}

// double builds a full double date group: owner + friend.
func (e *env) double(lat, lng float64, activity string) (a, b, groupID string) {
	e.t.Helper()
	a, b = e.located("Dbl", lat, lng), e.user("DblFriend")
	c, g := e.newGroup(a, "double", activity, 0)
	if c != 201 {
		e.t.Fatalf("create double: %d", c)
	}
	e.join(a, b, g.ID)
	return a, b, g.ID
}

func (e *env) groupMatch(myGroup, target, user string) (int, struct {
	Matched      bool   `json:"matched"`
	GroupMatchID string `json:"group_match_id"`
}) {
	c, b := e.call("POST", "/groups/"+myGroup+"/match", map[string]string{"target_group_id": target}, user)
	var r struct {
		Matched      bool   `json:"matched"`
		GroupMatchID string `json:"group_match_id"`
	}
	_ = json.Unmarshal(b, &r)
	return c, r
}

func TestGroupCreationRulesAndSizes(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	owner := e.located("Owner", lat, lng)

	bad := []map[string]any{
		{"name": "x", "type": "double", "activity": "dinner", "target_size": 3},
		{"name": "x", "type": "group", "activity": "dinner", "target_size": 1},
		{"name": "x", "type": "group", "activity": "dinner", "target_size": 7},
		{"name": "x", "type": "group", "activity": "dinner"},
		{"name": "x", "type": "party", "activity": "dinner"},
		{"name": "x", "type": "double", "activity": "skydiving"},
		{"name": "x", "type": "double", "activity": "custom"},
		{"name": "  ", "type": "double", "activity": "dinner"},
	}
	for i, b := range bad {
		if c, _ := e.call("POST", "/groups", b, owner); c != 422 {
			t.Errorf("case %d accepted: %d", i, c)
		}
	}
	if c, _ := e.call("POST", "/groups", map[string]any{"name": "n", "type": "double", "activity": "dinner"}, e.user("NoLoc")); c != 422 {
		t.Fatalf("no location: %d", c)
	}
	if c, _ := e.call("POST", "/groups", map[string]any{"name": "n", "type": "double", "activity": "dinner"},
		e.newUser("user", false, "Unverified")); c != 403 {
		t.Fatalf("unverified: %d", c)
	}

	// A group of 3: two invites fill it; a third invite is refused; joining is invite-only.
	c, g := e.newGroup(owner, "group", "hiking", 3)
	if c != 201 || g.Ready || g.MemberCount != 1 {
		t.Fatalf("create group: %d %+v", c, g)
	}
	f1, f2, f3, outsider := e.user("F1"), e.user("F2"), e.user("F3"), e.user("Out")
	if c, _ := e.call("POST", "/groups/"+g.ID+"/join", nil, outsider); c != 404 {
		t.Fatalf("uninvited join: %d", c)
	}
	e.call("POST", "/groups/"+g.ID+"/invite", map[string]string{"ref": e.refOf(f1)}, owner)
	e.call("POST", "/groups/"+g.ID+"/invite", map[string]string{"ref": e.refOf(f2)}, owner)
	if c, _ := e.call("POST", "/groups/"+g.ID+"/invite", map[string]string{"ref": e.refOf(f3)}, owner); c != 409 {
		t.Fatalf("invite beyond open slots: %d", c)
	}
	if c, _ := e.call("POST", "/groups/"+g.ID+"/invite", map[string]string{"ref": e.refOf(f1)}, owner); c != 409 {
		t.Fatalf("duplicate invite: %d", c)
	}
	if c, _ := e.call("POST", "/groups/"+g.ID+"/invite", map[string]string{"ref": e.refOf(f3)}, outsider); c != 404 {
		t.Fatalf("non-member inviting: %d", c)
	}
	e.call("POST", "/groups/"+g.ID+"/join", nil, f1)
	// f2 declines; the slot frees up and f3 can take it.
	_, body := e.call("GET", "/groups/invites", nil, f2)
	var inv struct{ Invites []struct{ ID string } }
	decodeInto(t, body, &inv)
	if len(inv.Invites) != 1 {
		t.Fatalf("invites: %s", body)
	}
	if c, _ := e.call("POST", "/groups/invites/"+inv.Invites[0].ID+"/decline", nil, f2); c != 204 {
		t.Fatalf("decline: %d", c)
	}
	e.join(owner, f3, g.ID)
	if c, _ := e.call("GET", "/groups/"+g.ID, nil, owner); c != 200 {
		t.Fatalf("get: %d", c)
	}
	if n := e.count(`SELECT COUNT(*) FROM group_members WHERE group_id=$1`, g.ID); n != 3 {
		t.Fatalf("members: %d", n)
	}
	// Full: nobody else can be invited; the DB also refuses a 4th row via the service path.
	if c, _ := e.call("POST", "/groups/"+g.ID+"/invite", map[string]string{"ref": e.refOf(outsider)}, owner); c != 409 {
		t.Fatalf("invite into full group: %d", c)
	}
	// Non-members cannot read the group.
	if c, _ := e.call("GET", "/groups/"+g.ID, nil, outsider); c != 404 {
		t.Fatalf("non-member get: %d", c)
	}

	// Cap of 5 groups per person.
	busy := e.located("Busy", lat+0.01, lng)
	for i := 0; i < 5; i++ {
		if c, _ := e.newGroup(busy, "double", "dinner", 0); c != 201 {
			t.Fatalf("group %d: %d", i+1, c)
		}
	}
	if c, _ := e.newGroup(busy, "double", "dinner", 0); c != 409 {
		t.Fatalf("6th group: %d", c)
	}
}

func TestBrowseMatchUnanimityAndMergedChat(t *testing.T) {
	e := needChat(t)
	lat, lng := randomBase()
	a1, a2, ga := e.double(lat, lng, "dinner")
	b1, b2, gb := e.double(lat+0.004, lng+0.004, "dinner")

	// Noise that must never be offered: another type, another activity, far away, incomplete.
	e.double(lat+0.002, lng, "hiking")
	_, _, farG := e.double(lat+3, lng, "dinner")
	solo := e.located("Solo", lat+0.001, lng)
	_, incomplete := e.newGroup(solo, "double", "dinner", 0)
	c1 := e.located("G1", lat+0.001, lng)
	_, groupType := e.newGroup(c1, "group", "dinner", 2)
	e.join(c1, e.user("G2"), groupType.ID)

	c, body := e.call("GET", "/groups/browse?for_group="+ga+"&radius_km=25", nil, a1)
	if c != 200 {
		t.Fatalf("browse: %d %s", c, body)
	}
	s := string(body)
	if !strings.Contains(s, gb) {
		t.Fatalf("matching double not offered: %s", s)
	}
	for name, id := range map[string]string{"far": farG, "incomplete": incomplete.ID, "other type": groupType.ID, "own": ga} {
		if strings.Contains(s, id) {
			t.Errorf("%s group must not be offered", name)
		}
	}
	if !strings.Contains(s, `"distance":"within`) || strings.Contains(s, "location") {
		t.Fatalf("distance must be bucketed: %s", s)
	}
	if c, _ := e.call("GET", "/groups/browse?for_group="+ga+"&activity=hiking&radius_km=25", nil, a1); c != 200 {
		t.Fatalf("activity filter: %d", c)
	}
	if c, _ := e.call("GET", "/groups/browse?for_group="+ga+"&radius_km=25", nil, b1); c != 404 {
		t.Fatalf("browsing for someone else's group: %d", c)
	}
	if c, _ := e.call("GET", "/groups/browse?for_group="+incomplete.ID+"&radius_km=25", nil, solo); c != 422 {
		t.Fatalf("incomplete group browsing: %d", c)
	}

	// Unanimity: every member of both groups must consent.
	if c, r := e.groupMatch(ga, gb, a1); c != 200 || r.Matched {
		t.Fatalf("a1: %d %+v", c, r)
	}
	if _, r := e.groupMatch(gb, ga, b1); r.Matched {
		t.Fatal("b1: premature match")
	}
	if _, r := e.groupMatch(ga, gb, a2); r.Matched {
		t.Fatal("a2: premature match (b2 has not consented)")
	}
	_, r := e.groupMatch(gb, ga, b2)
	if !r.Matched || r.GroupMatchID == "" {
		t.Fatalf("b2 should complete the match: %+v", r)
	}
	gm := r.GroupMatchID
	// Matched groups are no longer offered to each other.
	if _, body := e.call("GET", "/groups/browse?for_group="+ga+"&radius_km=25", nil, a1); strings.Contains(string(body), gb) {
		t.Fatal("matched group still offered")
	}
	if c, _ := e.groupMatch(ga, farG, a1); c != 404 {
		t.Fatalf("matching an ineligible group: %d", c)
	}

	// Merged chat: all four can talk; outsiders cannot.
	if c, _ := e.send(a1, gm, map[string]any{"type": "text", "ciphertext": ct("hello both crews")}); c != 201 {
		t.Fatalf("group send: %d", c)
	}
	for _, u := range []string{a2, b1, b2} {
		if msgs := e.list(u, gm); len(msgs) != 1 || msgs[0].FromMe {
			t.Fatalf("member did not receive group message: %+v", msgs)
		}
	}
	if c, _ := e.call("GET", "/chat/matches/"+gm+"/messages", nil, e.user("Stranger")); c != 404 {
		t.Fatalf("outsider reading group chat: %d", c)
	}

	// A matched group is locked: no new members.
	if c, _ := e.call("POST", "/groups/"+ga+"/invite", map[string]string{"ref": e.refOf(e.user("Late"))}, a1); c != 409 {
		t.Fatalf("invite into locked group: %d", c)
	}

	// Dates: both groups must confirm.
	start := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if c, _ := e.call("POST", "/groups/"+ga+"/schedule", map[string]any{"group_match_id": gm, "venue": "Bistro", "starts_at": past}, a1); c != 422 {
		t.Fatalf("past date: %d", c)
	}
	if c, _ := e.call("POST", "/groups/"+ga+"/schedule", map[string]any{"group_match_id": gm, "venue": "Bistro", "starts_at": start}, b1); c != 404 {
		t.Fatalf("scheduling for another group: %d", c)
	}
	c, body = e.call("POST", "/groups/"+ga+"/schedule",
		map[string]any{"group_match_id": gm, "venue": "<b>Bistro</b>", "address": "1 Main St", "starts_at": start}, a1)
	var d struct {
		ID, Status, Venue string
		Confirmations     int
	}
	decodeInto(t, body, &d)
	if c != 201 || d.Status != "proposed" || d.Venue != "Bistro" || d.Confirmations != 1 {
		t.Fatalf("schedule: %d %s", c, body)
	}
	_, body = e.call("POST", "/groups/dates/"+d.ID+"/confirm", nil, a2) // same group: still proposed
	decodeInto(t, body, &d)
	if d.Status != "proposed" {
		t.Fatalf("same-group confirmation must not confirm the date: %s", body)
	}
	_, body = e.call("POST", "/groups/dates/"+d.ID+"/confirm", nil, b1)
	decodeInto(t, body, &d)
	if d.Status != "confirmed" {
		t.Fatalf("both groups confirmed: %s", body)
	}
	if c, _ := e.call("POST", "/groups/dates/"+d.ID+"/confirm", nil, e.user("Stranger2")); c != 404 {
		t.Fatalf("stranger confirming: %d", c)
	}
	if c, _ := e.call("DELETE", "/groups/dates/"+d.ID, nil, b2); c != 204 {
		t.Fatalf("cancel: %d", c)
	}

	// Leaving breaks the match; chat access ends and the sweeper wipes it.
	if c, _ := e.call("DELETE", "/groups/"+ga+"/members/me", nil, a2); c != 204 {
		t.Fatalf("leave: %d", c)
	}
	if c, _ := e.call("GET", "/chat/matches/"+gm+"/messages", nil, b1); c != 404 {
		t.Fatalf("chat after the match ended: %d", c)
	}
	if _, err := e.chat.RunJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.deps.Mongo.Collection("chat_messages").CountDocuments(context.Background(), map[string]any{"match_id": gm}); n != 0 {
		t.Fatalf("%d group messages survived", n)
	}
	// Ownership passed on; group is open again and no longer ready.
	_, body = e.call("GET", "/groups/"+ga, nil, a1)
	var g grp
	decodeInto(t, body, &g)
	if g.Ready || g.Locked || g.MemberCount != 1 {
		t.Fatalf("group after leave: %s", body)
	}
}
