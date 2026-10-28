package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type partyState struct {
	Active    bool   `json:"active"`
	YourTurn  bool   `json:"your_turn"`
	Phase     string `json:"phase"`
	Kind      string `json:"kind"`
	Prompt    string `json:"prompt"`
	SkipsLeft int    `json:"skips_left"`
	Round     int    `json:"round"`
	Players   []struct {
		Label string `json:"label"`
		You   bool   `json:"you"`
		Turn  bool   `json:"turn"`
	} `json:"players"`
}

func (e *env) party(method, path string, body any, uid string) (int, partyState) {
	e.t.Helper()
	c, b := e.call(method, path, body, uid)
	var s partyState
	if c == 200 {
		if err := json.Unmarshal(b, &s); err != nil {
			e.t.Fatal(err)
		}
	}
	return c, s
}

func TestGroupTruthOrDareRotatesThroughEveryoneWithSkipsAndTakeOver(t *testing.T) {
	e := setup(t)
	a1, a2, b1, gm := e.matchedDoubles(t)
	stranger := e.user("Not In Group")
	base := "/games/party/" + gm

	if c, _ := e.party("GET", base, nil, stranger); c != 404 {
		t.Errorf("stranger: %d", c)
	}
	c, st := e.party("POST", base+"/start", nil, a1)
	if c != 200 || !st.Active || !st.YourTurn || st.Phase != "choose" || len(st.Players) != 4 {
		t.Fatalf("start: %d %+v", c, st)
	}
	var order []string
	if err := e.db.QueryRow(context.Background(), `SELECT play_order::text[] FROM group_td WHERE room_id=$1`, gm).Scan(&order); err != nil || len(order) != 4 {
		t.Fatalf("order: %v %v", order, err)
	}
	// whoseTurn finds the player who is up.
	whoseTurn := func() string {
		for _, u := range order {
			if _, s := e.party("GET", base, nil, u); s.YourTurn {
				return u
			}
		}
		t.Fatal("nobody's turn")
		return ""
	}

	// Only the player whose turn it is may pick.
	if c, _ := e.party("POST", base+"/pick", map[string]string{"kind": "dare"}, a2); c != 422 {
		t.Errorf("pick out of turn: %d", c)
	}
	if c, _ := e.party("POST", base+"/pick", map[string]string{"kind": "kiss"}, a1); c != 422 {
		t.Errorf("bad kind: %d", c)
	}
	_, st = e.party("POST", base+"/pick", map[string]string{"kind": "truth"}, a1)
	if st.Phase != "answer" || st.Kind != "truth" || st.Prompt == "" {
		t.Fatalf("pick: %+v", st)
	}
	_, seen := e.party("GET", base, nil, b1)
	if seen.Prompt != st.Prompt || seen.YourTurn {
		t.Errorf("others see: %+v", seen)
	}

	// Turns go round all four people in the fixed order and return to the first.
	var played []string
	e.party("POST", base+"/done", nil, a1)
	played = append(played, a1)
	for i := 0; i < 3; i++ {
		u := whoseTurn()
		played = append(played, u)
		e.party("POST", base+"/pick", map[string]string{"kind": "dare"}, u)
		e.party("POST", base+"/done", nil, u)
	}
	seenPlayers := map[string]bool{}
	for _, u := range played {
		seenPlayers[u] = true
	}
	if len(seenPlayers) != 4 {
		t.Errorf("not everyone had a turn: %v", played)
	}
	if whoseTurn() != a1 {
		t.Error("the turn did not come back to the first player")
	}

	// Two skips each, the third is refused.
	u := whoseTurn()
	skipped := 0
	for i := 0; i < 3; i++ {
		// get the turn back to u
		for guard := 0; guard < 8 && whoseTurn() != u; guard++ {
			cur := whoseTurn()
			e.party("POST", base+"/pick", map[string]string{"kind": "dare"}, cur)
			e.party("POST", base+"/done", nil, cur)
		}
		e.party("POST", base+"/pick", map[string]string{"kind": "dare"}, u)
		if c, _ := e.party("POST", base+"/skip", nil, u); c == 200 {
			skipped++
		}
	}
	if skipped != 2 {
		t.Errorf("skips allowed: %d, want 2", skipped)
	}

	// Taking over only when the player is idle.
	cur := whoseTurn()
	other := order[0]
	if other == cur {
		other = order[1]
	}
	if c, _ := e.party("POST", base+"/take-over", nil, other); c != 422 {
		t.Errorf("take over too early: %d", c)
	}
	e.exec(`UPDATE group_td SET updated_at = NOW() - INTERVAL '10 minutes' WHERE room_id=$1`, gm)
	if c, _ := e.party("POST", base+"/take-over", nil, other); c != 200 {
		t.Errorf("take over when idle: %d", c)
	}
	if whoseTurn() == cur {
		t.Error("the idle player still holds the turn")
	}
	if c, _ := e.call("DELETE", base, nil, a1); c != 204 {
		t.Errorf("end: %d", c)
	}
	if c, s := e.party("GET", base, nil, a1); c != 200 || s.Active {
		t.Errorf("after end: %d %+v", c, s)
	}
	_ = a2
}

func TestMaskedRoomSeatsStrangersUnderMadeUpNamesAndKeepsAnswersScreenedAndReportable(t *testing.T) {
	e := setup(t)
	e.exec(`DELETE FROM masked_waiting`)
	p := []string{e.user("Masked Alice"), e.user("Masked Bob"), e.user("Masked Cara"), e.user("Masked Dan")}
	bad := e.user("Blocker")
	_ = bad

	// Two people wait: no room yet. A third (after the wait) opens one.
	for _, u := range p[:2] {
		if c, b := e.call("POST", "/games/masked/join", nil, u); c != 200 || !strings.Contains(string(b), `"waiting"`) {
			t.Fatalf("join: %d %s", c, b)
		}
	}
	e.call("POST", "/games/masked/join", nil, p[2])
	e.exec(`UPDATE masked_waiting SET created_at = NOW() - INTERVAL '30 seconds'`)
	e.call("POST", "/games/masked/join", nil, p[3])
	_, b := e.call("GET", "/games/masked/status", nil, p[0])
	var status struct {
		State  string `json:"state"`
		RoomID string `json:"room_id"`
	}
	_ = json.Unmarshal(b, &status)
	if status.State != "in_room" || status.RoomID == "" {
		t.Fatalf("not seated: %s", b)
	}
	room := "/games/masked/rooms/" + status.RoomID

	// Everyone sees only made-up names; no real name, ref or id appears.
	_, rb := e.call("GET", room, nil, p[1])
	for _, leak := range []string{"Masked Alice", "Masked Bob", "Masked Cara", "Masked Dan", e.refOf(p[0]), p[0]} {
		if strings.Contains(string(rb), leak) {
			t.Errorf("masked room leaks %q: %s", leak, rb)
		}
	}
	if c, _ := e.call("GET", room, nil, e.user("Outsider")); c != 404 {
		t.Errorf("outsider: %d", c)
	}

	// Play: whoever starts picks a truth and must answer in text; the answer is shown under their alias.
	c, st := e.party("POST", "/games/party/"+status.RoomID+"/start", nil, p[0])
	if c != 200 || !st.Active {
		t.Fatalf("start: %d %+v", c, st)
	}
	e.party("POST", "/games/party/"+status.RoomID+"/pick", map[string]string{"kind": "truth"}, p[0])
	if c, _ := e.call("POST", room+"/finish", map[string]any{"text": ""}, p[0]); c != 422 {
		t.Errorf("empty truth answer: %d", c)
	}
	if c, _ := e.call("POST", room+"/finish", map[string]any{"text": "I once got lost in my own city"}, p[1]); c != 422 {
		t.Errorf("answer out of turn: %d", c)
	}
	if c, body := e.call("POST", room+"/finish", map[string]any{"text": "I once got lost in my own city"}, p[0]); c != 200 || !strings.Contains(string(body), "got lost") {
		t.Fatalf("answer: %d %s", c, body)
	}
	_, rb = e.call("GET", room, nil, p[2])
	var view struct {
		MyAlias string `json:"my_alias"`
		Answers []struct {
			ID, Alias, Body string
			Mine            bool
		} `json:"answers"`
	}
	_ = json.Unmarshal(rb, &view)
	if len(view.Answers) != 1 || view.Answers[0].Alias == "" || view.Answers[0].Mine {
		t.Fatalf("answers: %s", rb)
	}
	if c, _ := e.call("POST", room+"/answers/"+view.Answers[0].ID+"/report", map[string]string{"reason": "spam"}, p[2]); c != 204 {
		t.Errorf("report: %d", c)
	}
	if c, _ := e.call("POST", room+"/answers/"+view.Answers[0].ID+"/report", map[string]string{"reason": "spam"}, p[0]); c != 404 {
		t.Errorf("reporting yourself: %d", c)
	}

	// Leaving ends your access.
	e.call("POST", "/games/masked/leave", nil, p[3])
	if c, _ := e.call("GET", room, nil, p[3]); c != 404 {
		t.Errorf("after leaving: %d", c)
	}

	// Leaving and queueing again straight away is held back for a couple of minutes.
	if c, _ := e.call("POST", "/games/masked/join", nil, p[3]); c != 429 {
		t.Errorf("rejoin right after leaving: %d, want 429", c)
	}
	e.exec(`UPDATE masked_members SET left_at = NOW() - INTERVAL '1 hour' WHERE user_id=$1::uuid`, p[3])
	if c, _ := e.call("POST", "/games/masked/join", nil, p[3]); c != 200 {
		t.Errorf("rejoin after the cooldown: %d", c)
	}

	// The report keeps the reported line and the room around it, because the
	// room itself is deleted soon after it ends.
	if n := e.count(`SELECT COUNT(*) FROM reports WHERE reported_id=$1::uuid AND details='masked room answer'
	                  AND evidence::text LIKE '%REPORTED:%' AND evidence::text LIKE '%got lost%'`, p[0]); n != 1 {
		t.Errorf("report evidence missing context: %d", n)
	}
	// Two different people reporting someone in a day keeps them out of new rooms.
	e.call("POST", room+"/answers/"+view.Answers[0].ID+"/report", map[string]string{"reason": "harassment"}, p[1])
	e.exec(`UPDATE masked_members SET left_at = NOW() - INTERVAL '1 hour' WHERE user_id=$1::uuid`, p[0])
	if c, _ := e.call("POST", "/games/masked/join", nil, p[0]); c != 429 {
		t.Errorf("twice-reported member joining: %d, want 429", c)
	}
}
