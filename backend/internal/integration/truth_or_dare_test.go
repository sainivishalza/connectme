package integration

import (
	"context"
	"strings"
	"testing"
)

type tdState struct {
	Active      bool   `json:"active"`
	Round       int    `json:"round"`
	YourTurn    bool   `json:"your_turn"`
	Phase       string `json:"phase"`
	Kind        string `json:"kind"`
	Prompt      string `json:"prompt"`
	SkipsLeft   int    `json:"skips_left"`
	CanTakeOver bool   `json:"can_take_over"`
}

func (e *env) td(method, id, action string, body any, uid string) (int, tdState) {
	e.t.Helper()
	path := "/games/td/" + id
	if action != "" {
		path += "/" + action
	}
	c, b := e.call(method, path, body, uid)
	var s tdState
	if c == 200 {
		decodeInto(e.t, b, &s)
	}
	return c, s
}

func TestTruthOrDareTurnsAlternateAndOnlyTheActivePlayerActs(t *testing.T) {
	e := setup(t)
	a, b, m := e.matched()

	if c, s := e.td("GET", m, "", nil, a); c != 200 || s.Active {
		t.Fatalf("before starting: %d %+v", c, s)
	}
	// Starting: the starter goes first; starting again resumes (does not reset).
	c, s := e.td("POST", m, "start", nil, a)
	if c != 200 || !s.Active || !s.YourTurn || s.Phase != "choose" || s.Round != 1 || s.SkipsLeft != 2 {
		t.Fatalf("start: %d %+v", c, s)
	}
	if _, s2 := e.td("POST", m, "start", nil, b); !s2.Active || s2.YourTurn {
		t.Errorf("second start changed the game: %+v", s2)
	}

	// The other player cannot act out of turn.
	if c, _ := e.td("POST", m, "pick", map[string]string{"kind": "truth"}, b); c != 422 {
		t.Errorf("out-of-turn pick: %d", c)
	}
	if c, _ := e.td("POST", m, "done", nil, a); c != 422 {
		t.Errorf("finishing before choosing: %d", c)
	}
	if c, _ := e.td("POST", m, "pick", map[string]string{"kind": "icebreaker"}, a); c != 422 {
		t.Errorf("bad kind: %d", c)
	}

	// A picks a truth: both players see the same prompt, from the curated bank.
	c, s = e.td("POST", m, "pick", map[string]string{"kind": "truth"}, a)
	if c != 200 || s.Phase != "answer" || s.Kind != "truth" || s.Prompt == "" || !s.YourTurn {
		t.Fatalf("pick: %d %+v", c, s)
	}
	_, theirs := e.td("GET", m, "", nil, b)
	if theirs.Prompt != s.Prompt || theirs.YourTurn || theirs.Phase != "answer" {
		t.Errorf("the waiting player's view: %+v", theirs)
	}
	var inBank bool
	for _, p := range truthsForTest(t, e) {
		inBank = inBank || p == s.Prompt
	}
	if !inBank {
		t.Errorf("prompt is not from the curated truths: %q", s.Prompt)
	}
	// A cannot pick again, and B cannot finish A's turn.
	if c, _ := e.td("POST", m, "pick", map[string]string{"kind": "dare"}, a); c != 422 {
		t.Errorf("picking twice: %d", c)
	}
	if c, _ := e.td("POST", m, "done", nil, b); c != 422 {
		t.Errorf("finishing someone else's turn: %d", c)
	}

	// Done passes the turn.
	c, s = e.td("POST", m, "done", nil, a)
	if c != 200 || s.YourTurn || s.Round != 2 || s.Phase != "choose" || s.Prompt != "" {
		t.Fatalf("done: %d %+v", c, s)
	}
	_, mine := e.td("GET", m, "", nil, b)
	if !mine.YourTurn || mine.Phase != "choose" {
		t.Errorf("B's view after A finished: %+v", mine)
	}
	// B draws a dare and skips it; skips are limited per player.
	e.td("POST", m, "pick", map[string]string{"kind": "dare"}, b)
	c, s = e.td("POST", m, "skip", nil, b)
	if c != 200 || s.SkipsLeft != 1 || s.YourTurn {
		t.Fatalf("skip: %d %+v", c, s)
	}
	e.td("POST", m, "pick", map[string]string{"kind": "truth"}, a) // A's turn again (round 3)
	e.td("POST", m, "done", nil, a)
	e.td("POST", m, "pick", map[string]string{"kind": "dare"}, b)
	if c, s = e.td("POST", m, "skip", nil, b); c != 200 || s.SkipsLeft != 0 {
		t.Fatalf("second skip: %d %+v", c, s)
	}
	e.td("POST", m, "pick", map[string]string{"kind": "truth"}, a)
	e.td("POST", m, "done", nil, a)
	e.td("POST", m, "pick", map[string]string{"kind": "dare"}, b)
	if c, _ := e.td("POST", m, "skip", nil, b); c != 422 {
		t.Errorf("a third skip: %d", c)
	}
	if c, s := e.td("POST", m, "done", nil, b); c != 200 || s.Round != 7 {
		t.Errorf("finishing instead of skipping: %d %+v", c, s)
	}
	_, as := e.td("GET", m, "", nil, a)
	if as.SkipsLeft != 2 {
		t.Errorf("A's skips should be untouched: %d", as.SkipsLeft)
	}

	// Either player can end the game.
	if c, _ := e.td("DELETE", m, "", nil, b); c != 204 {
		t.Fatalf("end: %d", c)
	}
	if _, s := e.td("GET", m, "", nil, a); s.Active {
		t.Error("game still active after ending")
	}
}

func TestTruthOrDareIdleTurnCanBeTakenOver(t *testing.T) {
	e := setup(t)
	a, b, m := e.matched()
	e.td("POST", m, "start", nil, a)
	e.td("POST", m, "pick", map[string]string{"kind": "truth"}, a)

	if c, _ := e.td("POST", m, "take-over", nil, b); c != 422 {
		t.Errorf("taking over a fresh turn: %d", c)
	}
	if c, _ := e.td("POST", m, "take-over", nil, a); c != 422 {
		t.Errorf("taking over your own turn: %d", c)
	}
	e.exec(`UPDATE td_games SET updated_at = NOW() - INTERVAL '10 minutes' WHERE match_id=$1::uuid`, m)
	_, s := e.td("GET", m, "", nil, b)
	if !s.CanTakeOver {
		t.Fatalf("idle turn not offered for take-over: %+v", s)
	}
	if _, s := e.td("GET", m, "", nil, a); s.CanTakeOver {
		t.Error("the idle player is offered a take-over")
	}
	c, s := e.td("POST", m, "take-over", nil, b)
	if c != 200 || !s.YourTurn || s.Phase != "choose" || s.SkipsLeft != 2 {
		t.Fatalf("take over: %d %+v", c, s)
	}
}

func TestTruthOrDareAccessRules(t *testing.T) {
	e := setup(t)
	a, b, m := e.matched()
	stranger := e.user("Stranger")
	for _, call := range []struct{ method, action string }{{"GET", ""}, {"POST", "start"}, {"POST", "pick"}, {"POST", "done"}, {"POST", "skip"}, {"POST", "take-over"}, {"DELETE", ""}} {
		if c, _ := e.td(call.method, m, call.action, map[string]string{"kind": "truth"}, stranger); c != 404 {
			t.Errorf("%s %s as a stranger: %d", call.method, call.action, c)
		}
	}
	e.td("POST", m, "start", nil, a)
	// Blocking ends access for both, and the game goes with the match.
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(b)}, a)
	if c, _ := e.td("GET", m, "", nil, a); c != 404 {
		t.Errorf("game readable after a block: %d", c)
	}
	e.exec(`DELETE FROM matches WHERE id=$1::uuid`, m)
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM td_games WHERE match_id=$1::uuid`, m).Scan(&n); err != nil || n != 0 {
		t.Errorf("game outlived its match: %d %v", n, err)
	}
	if c, _ := e.td("POST", "not-a-uuid", "start", nil, a); c != 404 {
		t.Errorf("bad id: %d", c)
	}
}

// truthsForTest reads the curated truths through the public prompt endpoint
// until it has seen them all (the bank is small and fixed).
func truthsForTest(t *testing.T, e *env) []string {
	t.Helper()
	u := e.user("Prompt Reader")
	seen := map[string]bool{}
	for i := 0; i < 400; i++ {
		_, b := e.call("GET", "/games/prompt?kind=truth", nil, u)
		s := string(b)
		i0 := strings.Index(s, `"text":"`)
		if i0 < 0 {
			continue
		}
		s = s[i0+8:]
		seen[s[:strings.Index(s, `"`)]] = true
	}
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	return out
}
