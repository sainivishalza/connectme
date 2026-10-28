package integration

import (
	"strings"
	"testing"
)

func (e *env) matchedDoubles(t *testing.T) (a1, a2, b1 string, gm string) {
	t.Helper()
	lat, lng := randomBase()
	a1, a2, ga := e.double(lat, lng, "dinner")
	b1, b2, gb := e.double(lat+0.001, lng+0.001, "dinner")
	e.groupMatch(ga, gb, a1)
	e.groupMatch(gb, ga, b1)
	e.groupMatch(ga, gb, a2)
	_, r := e.groupMatch(gb, ga, b2)
	if !r.Matched {
		t.Fatal("groups did not match")
	}
	return a1, a2, b1, r.GroupMatchID
}

func TestWingmanReplyIdeasAreOptInMembersOnlyAndForwardNothingElse(t *testing.T) {
	e := setup(t)
	st := e.withAI()
	a1, _, b1, gm := e.matchedDoubles(t)
	stranger := e.user("Outsider")
	lines := []map[string]string{{"who": "other", "text": "Who wants bowling on Friday?"}, {"who": "me", "text": "Maybe!"}}
	path := "/groups/matches/" + gm + "/reply-ideas"

	c, b := e.call("POST", path, map[string]any{"lines": lines}, a1)
	if c != 200 || !strings.Contains(string(b), `"source":"llm"`) || !strings.Contains(string(b), "count me in") {
		t.Fatalf("reply ideas: %d %s", c, b)
	}
	// Only the chosen lines (no names, ids, group or user data) reach the AI service.
	if len(st.replyIn) != 1 || strings.Contains(st.replyIn[0], a1) || strings.Contains(st.replyIn[0], gm) ||
		!strings.Contains(st.replyIn[0], "bowling") || strings.Count(st.replyIn[0], `"who"`) != 2 {
		t.Errorf("forwarded body: %v", st.replyIn)
	}
	// A member of the other group may ask too; a stranger and a non-chat id may not.
	if c, _ := e.call("POST", path, map[string]any{"lines": lines}, b1); c != 200 {
		t.Errorf("other group member: %d", c)
	}
	if c, _ := e.call("POST", path, map[string]any{"lines": lines}, stranger); c != 404 {
		t.Errorf("stranger: %d", c)
	}
	if c, _ := e.call("POST", "/groups/matches/not-a-uuid/reply-ideas", map[string]any{"lines": lines}, a1); c != 404 {
		t.Errorf("bad id: %d", c)
	}

	// Input rules.
	long := strings.Repeat("x", 301)
	nine := make([]map[string]string, 9)
	for i := range nine {
		nine[i] = map[string]string{"who": "me", "text": "hi"}
	}
	for name, body := range map[string]map[string]any{
		"none":     {"lines": []map[string]string{}},
		"nine":     {"lines": nine},
		"too long": {"lines": []map[string]string{{"who": "me", "text": long}}},
		"who":      {"lines": []map[string]string{{"who": "Alice", "text": "hi"}}},
		"empty":    {"lines": []map[string]string{{"who": "me", "text": "   "}}},
		"control":  {"lines": []map[string]string{{"who": "me", "text": "a\x00b"}}},
	} {
		if c, b := e.call("POST", path, body, a1); c != 422 {
			t.Errorf("%s: %d %s", name, c, b)
		}
	}
	if c, _ := e.call("POST", path, map[string]any{"lines": lines, "extra": 1}, a1); c != 400 {
		t.Errorf("unknown field accepted: %d", c)
	}

	// With the AI service down the member still gets friendly generic replies.
	st.mu.Lock()
	st.failing = true
	st.mu.Unlock()
	c, b = e.call("POST", path, map[string]any{"lines": lines}, a1)
	if c != 200 || !strings.Contains(string(b), `"source":"fallback"`) || strings.Count(string(b), "?")+strings.Count(string(b), "!") == 0 {
		t.Errorf("fallback: %d %s", c, b)
	}
}
