package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func (e *env) likeWith(u, target, action, comment string) (int, string) {
	e.t.Helper()
	body := map[string]string{"target_ref": e.refOf(target), "action": action}
	if comment != "" {
		body["comment"] = comment
	}
	c, b := e.call("POST", "/matching/swipe", body, u)
	return c, string(b)
}

func TestANoteSentWithALikeReachesTheOtherPersonAndOnlyWhenTheSenderIsVisible(t *testing.T) {
	e := setup(t)
	e.withStripe()
	lat, lng := randomBase()
	sender, receiver := e.located("Sender", lat, lng), e.located("Receiver", lat+0.002, lng)
	e.subscribe(receiver, "gold", "active", 24*time.Hour) // Gold can see who liked them

	// Rules for the note itself.
	if c, _ := e.likeWith(sender, receiver, "pass", "hello there"); c != 422 {
		t.Errorf("a note with a pass: %d", c)
	}
	if c, _ := e.likeWith(sender, receiver, "like", strings.Repeat("x", 141)); c != 422 {
		t.Errorf("a long note: %d", c)
	}
	if c, _ := e.likeWith(sender, receiver, "like", "line one\nline two"); c != 422 {
		t.Errorf("a two-line note: %d", c)
	}
	if c, b := e.likeWith(sender, receiver, "like", "Your hiking photo made me smile"); c != 200 || strings.Contains(b, `"matched":true`) {
		t.Fatalf("like with a note: %d %s", c, b)
	}

	// The receiver (Gold) sees the note next to the person who sent it.
	_, b := e.call("GET", "/matching/likes-you", nil, receiver)
	if !strings.Contains(string(b), "Your hiking photo made me smile") {
		t.Errorf("likes-you lacks the note: %s", b)
	}
	var ly struct {
		Comments map[string]string `json:"comments"`
	}
	_ = json.Unmarshal(b, &ly)
	if ly.Comments[e.refOf(sender)] == "" {
		t.Errorf("note not keyed by the sender's ref: %s", b)
	}

	// The receiver likes back: a match, and the swipe response carries the sender's note.
	c, b2 := e.likeWith(receiver, sender, "like", "Ha, thank you")
	if c != 200 || !strings.Contains(b2, `"matched":true`) || !strings.Contains(b2, "Your hiking photo made me smile") {
		t.Fatalf("matching like: %d %s", c, b2)
	}
	// Both notes appear in the match list, each labelled from the viewer's side.
	_, mb := e.call("GET", "/matching/matches", nil, receiver)
	if !strings.Contains(string(mb), `"their_comment":"Your hiking photo made me smile"`) || !strings.Contains(string(mb), `"my_comment":"Ha, thank you"`) {
		t.Errorf("receiver's match view: %s", mb)
	}
	_, mb = e.call("GET", "/matching/matches", nil, sender)
	if !strings.Contains(string(mb), `"their_comment":"Ha, thank you"`) || !strings.Contains(string(mb), `"my_comment":"Your hiking photo made me smile"`) {
		t.Errorf("sender's match view: %s", mb)
	}
}

func TestAnAnonymousSendersNoteIsNeverShownBecauseFreeTextCouldUnmaskThem(t *testing.T) {
	e := setup(t)
	e.withStripe()
	lat, lng := randomBase()
	anon, receiver := e.located("Hidden Sender", lat, lng), e.located("Receiver", lat+0.002, lng)
	e.subscribe(receiver, "gold", "active", 24*time.Hour)
	if c, b := e.call("POST", "/users/me/mode", map[string]string{"mode": "incognito"}, anon); c != 200 {
		t.Fatalf("mode: %d %s", c, b)
	}
	if c, b := e.likeWith(anon, receiver, "like", "I saw you at the Hilton bar on Friday"); c != 200 {
		t.Fatalf("anonymous like with a note: %d %s", c, b)
	}
	_, b := e.call("GET", "/matching/likes-you", nil, receiver)
	if strings.Contains(string(b), "Hilton") || strings.Contains(string(b), `"comments"`) {
		t.Errorf("an anonymous sender's note leaked to likes-you: %s", b)
	}
	c, b2 := e.likeWith(receiver, anon, "like", "")
	if c != 200 || !strings.Contains(b2, `"matched":true`) {
		t.Fatalf("match: %d %s", c, b2)
	}
	if strings.Contains(b2, "Hilton") {
		t.Errorf("note leaked in the swipe result: %s", b2)
	}
	_, mb := e.call("GET", "/matching/matches", nil, receiver)
	if strings.Contains(string(mb), "Hilton") {
		t.Errorf("note leaked in the match list: %s", mb)
	}
}
