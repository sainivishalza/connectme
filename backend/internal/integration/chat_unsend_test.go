package integration

import "testing"

func TestAMessageCanBeUnsentOnlyByItsSender(t *testing.T) {
	e := needChat(t)
	a, b, mid := e.matched()
	stranger := e.user("Stranger")

	_, first := e.send(a, mid, map[string]any{"type": "text", "ciphertext": ct("from-a-1")})
	_, second := e.send(a, mid, map[string]any{"type": "text", "ciphertext": ct("from-a-2")})
	_, theirs := e.send(b, mid, map[string]any{"type": "text", "ciphertext": ct("from-b-1")})
	path := func(id string) string { return "/chat/matches/" + mid + "/messages/" + id }

	// The other person, a stranger and a malformed id cannot remove it.
	if c, _ := e.call("DELETE", path(first.ID), nil, b); c != 404 {
		t.Errorf("the recipient unsent a message: %d", c)
	}
	if c, _ := e.call("DELETE", path(first.ID), nil, stranger); c != 404 {
		t.Errorf("a stranger unsent a message: %d", c)
	}
	if c, _ := e.call("DELETE", path("not-an-id"), nil, a); c != 404 {
		t.Errorf("bad id: %d", c)
	}
	if n := len(e.list(a, mid)); n != 3 {
		t.Fatalf("messages after refused unsends: %d", n)
	}

	// The sender removes one; it disappears for both, the rest stay.
	if c, _ := e.call("DELETE", path(first.ID), nil, a); c != 204 {
		t.Fatalf("unsend: %d", c)
	}
	for _, who := range []string{a, b} {
		l := e.list(who, mid)
		if len(l) != 2 || l[0].ID != second.ID || l[1].ID != theirs.ID {
			t.Errorf("after unsend (%s): %+v", who, l)
		}
	}
	// Doing it again, or unsending someone else's, finds nothing.
	if c, _ := e.call("DELETE", path(first.ID), nil, a); c != 404 {
		t.Errorf("unsend twice: %d", c)
	}
	if c, _ := e.call("DELETE", path(theirs.ID), nil, a); c != 404 {
		t.Errorf("unsent the other person's message: %d", c)
	}
}
