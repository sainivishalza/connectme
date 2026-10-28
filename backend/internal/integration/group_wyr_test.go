package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type wyr struct {
	Active   bool   `json:"active"`
	PollID   string `json:"poll_id"`
	A        string `json:"a"`
	B        string `json:"b"`
	Closed   bool   `json:"closed"`
	Members  int    `json:"members"`
	Voted    int    `json:"voted"`
	MyChoice string `json:"my_choice"`
	CountA   *int   `json:"count_a"`
	CountB   *int   `json:"count_b"`
}

func (e *env) wyrCall(method, path string, body any, uid string) (int, wyr, string) {
	e.t.Helper()
	c, b := e.call(method, path, body, uid)
	var w wyr
	if c == 200 {
		if err := json.Unmarshal(b, &w); err != nil {
			e.t.Fatal(err)
		}
	}
	return c, w, string(b)
}

func TestGroupWouldYouRatherIsAnonymousOneRoundAtATimeAndMembersOnly(t *testing.T) {
	e := setup(t)
	a1, a2, b1, gm := e.matchedDoubles(t)
	stranger := e.user("Outsider")
	base := "/games/group/" + gm

	// Nothing is running at first; strangers and bad ids get nothing.
	if c, w, _ := e.wyrCall("GET", base, nil, a1); c != 200 || w.Active || w.Members != 4 {
		t.Fatalf("idle state: %d %+v", c, w)
	}
	for _, p := range []struct{ m, path string }{{"GET", base}, {"POST", base + "/start"}, {"POST", base + "/vote"}} {
		if c, _, _ := e.wyrCall(p.m, p.path, map[string]string{"poll_id": "x", "choice": "a"}, stranger); c != 404 {
			t.Errorf("stranger %s %s: %d", p.m, p.path, c)
		}
	}
	if c, _, _ := e.wyrCall("POST", "/games/group/not-a-uuid/start", nil, a1); c != 404 {
		t.Errorf("bad id: %d", c)
	}

	// Anyone in either group starts a round; a second start is refused while it runs.
	c, w, raw := e.wyrCall("POST", base+"/start", nil, a1)
	if c != 200 || !w.Active || w.A == "" || w.B == "" || w.Closed || w.PollID == "" {
		t.Fatalf("start: %d %s", c, raw)
	}
	if c, _, _ := e.wyrCall("POST", base+"/start", nil, b1); c != 422 {
		t.Errorf("second start while running: %d", c)
	}
	poll := w.PollID

	// Totals stay hidden until you have voted; the first vote stands.
	if _, w, raw := e.wyrCall("GET", base, nil, a2); w.CountA != nil || w.CountB != nil || w.MyChoice != "" {
		t.Errorf("totals shown before voting: %s", raw)
	}
	if c, _, _ := e.wyrCall("POST", base+"/vote", map[string]string{"poll_id": poll, "choice": "c"}, a1); c != 422 {
		t.Errorf("bad choice: %d", c)
	}
	if c, _, _ := e.wyrCall("POST", base+"/vote", map[string]string{"poll_id": "00000000-0000-0000-0000-000000000000", "choice": "a"}, a1); c != 422 {
		t.Errorf("unknown round: %d", c)
	}
	_, w, raw = e.wyrCall("POST", base+"/vote", map[string]string{"poll_id": poll, "choice": "a"}, a1)
	if w.MyChoice != "a" || w.CountA == nil || *w.CountA != 1 || *w.CountB != 0 || w.Voted != 1 {
		t.Fatalf("after first vote: %s", raw)
	}
	_, w, _ = e.wyrCall("POST", base+"/vote", map[string]string{"poll_id": poll, "choice": "b"}, a1)
	if w.MyChoice != "a" || *w.CountA != 1 || *w.CountB != 0 {
		t.Errorf("a vote was changed: %+v", w)
	}

	// The payload never says who voted for what.
	for _, leak := range []string{a1, a2, b1, "user_id", "voters", "started_by"} {
		if strings.Contains(raw, leak) {
			t.Errorf("response leaks %q: %s", leak, raw)
		}
	}

	// A different member sees the same totals after voting; the round ends when everyone has voted.
	e.wyrCall("POST", base+"/vote", map[string]string{"poll_id": poll, "choice": "b"}, a2)
	e.wyrCall("POST", base+"/vote", map[string]string{"poll_id": poll, "choice": "b"}, b1)
	if _, w, _ := e.wyrCall("GET", base, nil, a1); w.Closed {
		t.Error("closed before everyone voted")
	}
	b2 := e.otherMember(t, gm, a1, a2, b1)
	e.wyrCall("POST", base+"/vote", map[string]string{"poll_id": poll, "choice": "a"}, b2)
	_, w, raw = e.wyrCall("GET", base, nil, a1)
	if !w.Closed || w.Voted != 4 || *w.CountA != 2 || *w.CountB != 2 {
		t.Fatalf("finished round: %s", raw)
	}
	if c, _, _ := e.wyrCall("POST", base+"/vote", map[string]string{"poll_id": poll, "choice": "a"}, a1); c != 422 {
		t.Errorf("vote on a finished round: %d", c)
	}

	// A new round picks a different question and starts clean.
	c, w2, raw := e.wyrCall("POST", base+"/start", nil, b1)
	if c != 200 || w2.PollID == poll || (w2.A == w.A && w2.B == w.B) || w2.Voted != 0 || w2.CountA != nil {
		t.Errorf("next round: %d %s", c, raw)
	}

	// A round left alone times out and the next person can move on.
	e.exec(`UPDATE group_polls SET created_at = NOW() - INTERVAL '11 minutes' WHERE group_match_id=$1::uuid AND closed_at IS NULL`, gm)
	if _, w, _ := e.wyrCall("GET", base, nil, a2); !w.Closed {
		t.Errorf("an abandoned round did not time out: %+v", w)
	}
	if c, _, _ := e.wyrCall("POST", base+"/start", nil, a2); c != 200 {
		t.Errorf("start after timeout: %d", c)
	}
}

// otherMember returns the member of a group match who is none of the given users.
func (e *env) otherMember(t *testing.T, groupMatch string, not ...string) string {
	t.Helper()
	rows, err := e.db.Query(context.Background(), `
		SELECT m.user_id::text FROM group_matches gm JOIN group_members m ON m.group_id IN (gm.group_a, gm.group_b)
		 WHERE gm.id=$1::uuid`, groupMatch)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		skip := false
		for _, n := range not {
			skip = skip || n == id
		}
		if !skip {
			return id
		}
	}
	t.Fatal("no other member")
	return ""
}
