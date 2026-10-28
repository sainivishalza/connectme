package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

type mlt struct {
	Active     bool   `json:"active"`
	PollID     string `json:"poll_id"`
	Prompt     string `json:"prompt"`
	Closed     bool   `json:"closed"`
	Members    int    `json:"members"`
	Voted      int    `json:"voted"`
	MyVote     string `json:"my_vote"`
	Candidates []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Count *int   `json:"count"`
	} `json:"candidates"`
}

func (e *env) mltCall(method, path string, body any, uid string) (int, mlt, string) {
	e.t.Helper()
	c, b := e.call(method, path, body, uid)
	var m mlt
	if c == 200 {
		if err := json.Unmarshal(b, &m); err != nil {
			e.t.Fatal(err)
		}
	}
	return c, m, string(b)
}

func TestGroupMostLikelyToShowsTotalsPerMemberNeverWhoVotedAndSharesTheOneRoundRule(t *testing.T) {
	e := setup(t)
	a1, a2, b1, gm := e.matchedDoubles(t)
	stranger := e.user("Outsider")
	base := "/games/group/" + gm + "/mlt"

	if c, _, _ := e.mltCall("GET", base, nil, stranger); c != 404 {
		t.Errorf("stranger: %d", c)
	}
	c, st, raw := e.mltCall("POST", base+"/start", nil, a1)
	if c != 200 || !st.Active || !strings.HasPrefix(st.Prompt, "Most likely to") || len(st.Candidates) != 4 {
		t.Fatalf("start: %d %s", c, raw)
	}
	// Before voting there are no counts; one round at a time, across games.
	for _, cd := range st.Candidates {
		if cd.Count != nil {
			t.Error("totals shown before voting")
		}
	}
	if c, _, _ := e.mltCall("POST", base+"/start", nil, b1); c != 422 {
		t.Errorf("second start: %d", c)
	}
	if c, _, _ := e.wyrCall("POST", "/games/group/"+gm+"/start", nil, b1); c != 422 {
		t.Errorf("a Would you rather round while this runs: %d", c)
	}

	// Validation: a stranger's id, garbage, and the wrong poll.
	if c, _, _ := e.mltCall("POST", base+"/vote", map[string]string{"poll_id": st.PollID, "candidate": e.refOf(stranger)}, a1); c != 422 {
		t.Errorf("voting for someone outside the group: %d", c)
	}
	if c, _, _ := e.mltCall("POST", base+"/vote", map[string]string{"poll_id": st.PollID, "candidate": "x"}, a1); c != 422 {
		t.Errorf("garbage candidate: %d", c)
	}
	target := st.Candidates[0].ID
	other := st.Candidates[1].ID
	// a1 votes for target; the first vote stands.
	_, v, _ := e.mltCall("POST", base+"/vote", map[string]string{"poll_id": st.PollID, "candidate": target}, a1)
	if v.MyVote != target || v.Voted != 1 {
		t.Fatalf("vote: %+v", v)
	}
	_, v, _ = e.mltCall("POST", base+"/vote", map[string]string{"poll_id": st.PollID, "candidate": other}, a1)
	if v.MyVote != target {
		t.Errorf("vote was changed: %+v", v)
	}
	// a1 now sees totals; a2 who has not voted does not.
	total := 0
	for _, cd := range v.Candidates {
		if cd.Count == nil {
			t.Fatal("voter does not see totals")
		}
		total += *cd.Count
	}
	if total != 1 {
		t.Errorf("total: %d", total)
	}
	_, hidden, _ := e.mltCall("GET", base, nil, a2)
	for _, cd := range hidden.Candidates {
		if cd.Count != nil {
			t.Error("non-voter sees totals")
		}
	}
	// Nothing says who voted for whom; everyone votes and the round closes.
	e.mltCall("POST", base+"/vote", map[string]string{"poll_id": st.PollID, "candidate": target}, a2)
	e.mltCall("POST", base+"/vote", map[string]string{"poll_id": st.PollID, "candidate": other}, b1)
	_, raw2, rawText := e.mltCall("GET", base, nil, a1)
	_ = raw2
	if strings.Contains(rawText, "voted_by") || strings.Contains(rawText, `"user_id"`) {
		t.Errorf("who voted leaked: %s", rawText)
	}
}
