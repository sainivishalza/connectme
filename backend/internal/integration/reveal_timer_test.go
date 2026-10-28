package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"connectme/internal/matching"
	"connectme/internal/users"
)

type timerView struct {
	MatchID string `json:"match_id"`
	Timer   *struct {
		ByMe       bool `json:"by_me"`
		Accepted   bool `json:"accepted"`
		NeedsReply bool `json:"needs_reply"`
	} `json:"reveal_timer"`
	Profile struct {
		Anonymous   bool   `json:"anonymous"`
		DisplayName string `json:"display_name"`
	} `json:"profile"`
}

func (e *env) timerOf(uid, matchID string) timerView {
	e.t.Helper()
	_, b := e.call("GET", "/matching/matches", nil, uid)
	var out struct {
		Matches []timerView `json:"matches"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		e.t.Fatal(err)
	}
	for _, m := range out.Matches {
		if m.MatchID == matchID {
			return m
		}
	}
	e.t.Fatalf("match not listed: %s", b)
	return timerView{}
}

func TestRevealTimerNeedsBothAndFiresOnTimeUnlessCancelled(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	a, b := e.user("Timer Anna"), e.user("Timer Ben")
	for _, u := range []string{a, b} {
		e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, u)
		e.locate(u, lat, lng)
	}
	e.swipe(a, e.refOf(b), "like")
	_, r := e.swipe(b, e.refOf(a), "like")
	m := r.MatchID
	path := "/matching/matches/" + m + "/reveal-timer"

	if c, _ := e.call("POST", path, map[string]int{"hours": 0}, a); c != 422 {
		t.Errorf("zero hours: %d", c)
	}
	if c, _ := e.call("POST", path, map[string]int{"hours": 100}, a); c != 422 {
		t.Errorf("too long: %d", c)
	}
	outsider := e.user("Not In It")
	if c, _ := e.call("POST", path, map[string]int{"hours": 2}, outsider); c != 404 {
		t.Errorf("outsider: %d", c)
	}

	// A proposal alone reveals nothing and waits for the other person.
	if c, body := e.call("POST", path, map[string]int{"hours": 2}, a); c != 200 || !strings.Contains(string(body), `"by_me":true`) {
		t.Fatalf("propose: %d %s", c, body)
	}
	if v := e.timerOf(b, m); v.Timer == nil || !v.Timer.NeedsReply || v.Timer.Accepted {
		t.Fatalf("their view: %+v", v.Timer)
	}
	svc := matching.NewService(e.db, users.NewService(e.db, users.LocalStore{Dir: t.TempDir()}, nil))
	e.exec(`UPDATE reveal_schedules SET reveal_at = NOW() - INTERVAL '1 minute' WHERE match_id=$1`, m)
	if n, _ := svc.RunDueReveals(context.Background()); n != 0 {
		t.Fatalf("an unanswered proposal fired: %d", n)
	}

	// The other person agrees: it is now fixed.
	e.call("POST", path, map[string]int{"hours": 2}, a)
	if c, body := e.call("POST", path, map[string]int{"hours": 5}, b); c != 200 || !strings.Contains(string(body), `"accepted":true`) {
		t.Fatalf("accept: %d %s", c, body)
	}
	if c, _ := e.call("POST", path, map[string]int{"hours": 5}, a); c != 422 {
		t.Errorf("changing an agreed time: %d", c)
	}
	// Not yet due: nothing happens and the card is still anonymous.
	if n, _ := svc.RunDueReveals(context.Background()); n != 0 {
		t.Fatalf("fired early: %d", n)
	}
	if v := e.timerOf(a, m); !v.Profile.Anonymous {
		t.Fatal("revealed early")
	}

	// Cancelling by either side stops it.
	e.call("DELETE", path, nil, b)
	e.exec(`UPDATE reveal_schedules SET reveal_at = NOW() - INTERVAL '1 minute' WHERE match_id=$1`, m)
	if n, _ := svc.RunDueReveals(context.Background()); n != 0 {
		t.Errorf("a cancelled timer fired: %d", n)
	}

	// Agree again, let it come due: both are revealed, exactly as if both pressed Reveal.
	e.call("POST", path, map[string]int{"hours": 1}, a)
	e.call("POST", path, map[string]int{"hours": 1}, b)
	e.exec(`UPDATE reveal_schedules SET reveal_at = NOW() - INTERVAL '1 minute' WHERE match_id=$1`, m)
	if n, err := svc.RunDueReveals(context.Background()); err != nil || n != 1 {
		t.Fatalf("due: %d %v", n, err)
	}
	if n, _ := svc.RunDueReveals(context.Background()); n != 0 {
		t.Errorf("fired twice: %d", n)
	}
	va := e.timerOf(a, m)
	if va.Profile.Anonymous || va.Profile.DisplayName != "Timer Ben" || va.Timer != nil {
		t.Errorf("after the reveal: %+v", va)
	}
}
