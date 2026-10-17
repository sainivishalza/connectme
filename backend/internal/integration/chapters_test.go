package integration

import (
	"strings"
	"testing"
)

func TestChaptersFollowPhotoVisibility(t *testing.T) {
	e := setup(t)
	open, shy, viewer, blocked := e.user("Ona"), e.user("Shy"), e.user("Viewer"), e.user("Blocked")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)

	put := func(u string, cs []map[string]string) (int, []byte) {
		return e.call("PUT", "/chapters/me", map[string]any{"chapters": cs}, u)
	}
	story := []map[string]string{
		{"title": "The year I moved abroad", "body": "I packed two bags and a plant and never looked back."},
		{"title": "Learning to cook", "body": "Burnt rice taught me patience, then I got good."},
	}
	if c, b := put(open, story); c != 200 {
		t.Fatalf("put: %d %s", c, b)
	}
	put(shy, story)

	// Order is kept; the owner reads their own.
	_, b := e.call("GET", "/chapters/me", nil, open)
	if !strings.Contains(string(b), "moved abroad") || strings.Index(string(b), "moved abroad") > strings.Index(string(b), "Learning to cook") {
		t.Errorf("mine: %s", b)
	}
	// A person who can see the full profile reads them; the list view only advertises a count.
	if c, b := e.call("GET", "/chapters/"+e.refOf(open), nil, viewer); c != 200 || !strings.Contains(string(b), "two bags") {
		t.Errorf("viewer: %d %s", c, b)
	}
	_, body := e.call("GET", "/users/"+e.refOf(open), nil, viewer)
	if !strings.Contains(string(body), `"chapters":2`) {
		t.Errorf("open profile does not advertise chapters: %s", body)
	}
	// Anonymous profile: not found and no hint.
	if c, _ := e.call("GET", "/chapters/"+e.refOf(shy), nil, viewer); c != 404 {
		t.Errorf("incognito chapters: %d", c)
	}
	_, body = e.call("GET", "/users/"+e.refOf(shy), nil, viewer)
	if strings.Contains(string(body), "chapters") {
		t.Errorf("anonymous profile leaks chapters: %s", body)
	}
	// Blocks hide them.
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(blocked)}, open)
	if c, _ := e.call("GET", "/chapters/"+e.refOf(open), nil, blocked); c != 404 {
		t.Errorf("blocked reads chapters: %d", c)
	}

	// Validation and replacement.
	tooMany := make([]map[string]string, 6)
	for i := range tooMany {
		tooMany[i] = story[0]
	}
	for name, cs := range map[string][]map[string]string{
		"six":         tooMany,
		"short title": {{"title": "ab", "body": "long enough body text"}},
		"short body":  {{"title": "A title", "body": "short"}},
		"long body":   {{"title": "A title", "body": strings.Repeat("x", 601)}},
	} {
		if c, _ := put(open, cs); c != 422 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if c, _ := put(open, nil); c != 200 {
		t.Errorf("clear: %d", c)
	}
	if c, _ := e.call("GET", "/chapters/"+e.refOf(open), nil, viewer); c != 200 {
		t.Errorf("cleared list: %d", c)
	}
	_, body = e.call("GET", "/users/"+e.refOf(open), nil, viewer)
	if strings.Contains(string(body), "chapters") {
		t.Errorf("cleared chapters still advertised: %s", body)
	}
}
