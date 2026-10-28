package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"connectme/internal/eventgroups"
)

func TestEventGroupIsOnlyForPeopleGoingAndKeepsAnonymousNamesHidden(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	partner := e.makePartner("activity", "", lat, lng, nil)
	_, ev := e.makeEvent(partner, "concert", 5, nil, 0)
	open, shy, outsider, blocked := e.user("Going Open"), e.user("Going Shy"), e.user("Staying Home"), e.user("Going Blocked")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)
	for _, u := range []string{open, shy, blocked} {
		e.locate(u, lat+0.001, lng)
		if c, b := e.call("POST", "/partners/events/"+ev.ID+"/rsvp", nil, u); c != 200 {
			t.Fatalf("rsvp: %d %s", c, b)
		}
	}
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(blocked)}, open)
	board := "/eventgroups/" + ev.ID

	// Only people with a spot can read or write.
	if c, _ := e.call("GET", board, nil, outsider); c != 404 {
		t.Errorf("outsider reads: %d", c)
	}
	if c, _ := e.call("POST", board+"/messages", map[string]string{"body": "hello"}, outsider); c != 404 {
		t.Errorf("outsider writes: %d", c)
	}

	// Messages: named only when shown in full; a block hides each other.
	post := func(u, body string) int {
		c, _ := e.call("POST", board+"/messages", map[string]string{"body": body}, u)
		return c
	}
	if c := post(open, "Who is coming early?"); c != 204 {
		t.Fatalf("post: %d", c)
	}
	if c := post(shy, "I might be"); c != 204 {
		t.Fatalf("anonymous post: %d", c)
	}
	if c := post(blocked, "Hello from the blocked one"); c != 204 {
		t.Fatalf("blocked post: %d", c)
	}
	if c := post(open, ""); c != 422 {
		t.Errorf("empty: %d", c)
	}
	_, b := e.call("GET", board, nil, open)
	var got struct {
		Going    int `json:"going"`
		Members  []struct{ Name string }
		Messages []struct {
			ID, Body, Name string
			Mine           bool
		}
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Going != 3 {
		t.Errorf("going: %d", got.Going)
	}
	for _, m := range got.Members {
		if m.Name == "Going Shy" || m.Name == "Going Blocked" || m.Name == "Going Open" {
			t.Errorf("member list shows %q", m.Name)
		}
	}
	names := map[string]string{}
	for _, m := range got.Messages {
		names[m.Body] = m.Name
	}
	if names["Who is coming early?"] != "Going Open" || names["I might be"] != "Someone" {
		t.Errorf("names: %v", names)
	}
	if _, seen := names["Hello from the blocked one"]; seen {
		t.Error("a blocked person's message is visible")
	}
	if strings.Contains(string(b), "Going Shy") {
		t.Errorf("anonymous member named: %s", b)
	}

	// Delete own; report someone else's; cannot delete theirs.
	var theirs, mine string
	for _, m := range got.Messages {
		if m.Mine {
			mine = m.ID
		} else {
			theirs = m.ID
		}
	}
	if c, _ := e.call("DELETE", "/eventgroups/messages/"+theirs, nil, open); c != 404 {
		t.Errorf("deleting theirs: %d", c)
	}
	if c, _ := e.call("POST", "/eventgroups/messages/"+theirs+"/report", map[string]string{"reason": "spam"}, open); c != 204 {
		t.Errorf("report: %d", c)
	}
	if c, _ := e.call("DELETE", "/eventgroups/messages/"+mine, nil, open); c != 204 {
		t.Errorf("deleting mine: %d", c)
	}

	// Leaving the event closes the board to you; the job clears it after the event.
	e.call("DELETE", "/partners/events/"+ev.ID+"/rsvp", nil, open)
	if c, _ := e.call("GET", board, nil, open); c != 404 {
		t.Errorf("after leaving: %d", c)
	}
	e.exec(`UPDATE events SET starts_at = NOW() - INTERVAL '30 hours', ends_at = NOW() - INTERVAL '27 hours' WHERE id=$1`, ev.ID)
	n, err := eventgroups.NewService(e.db, nil).Expire(context.Background())
	if err != nil || n < 1 {
		t.Errorf("expire: %d %v", n, err)
	}
}
