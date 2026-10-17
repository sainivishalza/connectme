package integration

import (
	"strings"
	"testing"
)

type quizView struct {
	ID            string `json:"id"`
	MatchID       string `json:"match_id"`
	Complete      bool   `json:"complete"`
	MyAnswered    int    `json:"my_answered"`
	OtherAnswered int    `json:"other_answered"`
	Score         *int   `json:"score"`
	Questions     []struct {
		Text    string   `json:"text"`
		Options []string `json:"options"`
		Mine    int      `json:"mine"`
		Theirs  *int     `json:"theirs"`
		Same    *bool    `json:"same"`
	} `json:"questions"`
}

func (e *env) quiz(method, path string, body any, uid string) (int, quizView) {
	e.t.Helper()
	c, b := e.call(method, path, body, uid)
	var q quizView
	if c == 200 {
		// "current" wraps the quiz in {"quiz": ...}
		var wrap struct {
			Quiz *quizView `json:"quiz"`
		}
		if strings.HasPrefix(path, "/games/quiz/match/") {
			decodeInto(e.t, b, &wrap)
			if wrap.Quiz != nil {
				q = *wrap.Quiz
			}
		} else {
			decodeInto(e.t, b, &q)
		}
	}
	return c, q
}

func (e *env) answerAll(uid, quizID string, n int, choice func(i int) int) {
	e.t.Helper()
	for i := 0; i < n; i++ {
		if c, b := e.call("POST", "/games/quiz/"+quizID+"/answer", map[string]int{"question": i, "choice": choice(i)}, uid); c != 200 {
			e.t.Fatalf("answer %d: %d %s", i, c, b)
		}
	}
}

func TestGamePromptsAndQuizSets(t *testing.T) {
	e := setup(t)
	u := e.user("Player")
	for _, k := range []string{"truth", "dare", "icebreaker"} {
		c, b := e.call("GET", "/games/prompt?kind="+k, nil, u)
		if c != 200 || !strings.Contains(string(b), `"kind":"`+k+`"`) || !strings.Contains(string(b), `"text":"`) {
			t.Errorf("%s: %d %s", k, c, b)
		}
	}
	if c, _ := e.call("GET", "/games/prompt?kind=spicy", nil, u); c != 422 {
		t.Errorf("unknown kind: %d", c)
	}
	c, b := e.call("GET", "/games/quiz/sets", nil, u)
	if c != 200 || !strings.Contains(string(b), `"id":"everyday"`) {
		t.Errorf("sets: %d %s", c, b)
	}
}

func TestCoupleQuizRevealsAnswersOnlyWhenBothAreDone(t *testing.T) {
	e := setup(t)
	a, b, m := e.matched()

	c, qa := e.quiz("POST", "/games/quiz", map[string]string{"match_id": m, "set_id": "everyday"}, a)
	if c != 200 || len(qa.Questions) < 6 || qa.Complete {
		t.Fatalf("start: %d %+v", c, qa)
	}
	n := len(qa.Questions)
	// The other person joins the same session; starting again does not replace it.
	_, qb := e.quiz("POST", "/games/quiz", map[string]string{"match_id": m, "set_id": "dating"}, b)
	if qb.ID != qa.ID {
		t.Fatal("a second start replaced the quiz in progress")
	}

	e.answerAll(a, qa.ID, n, func(i int) int { return 0 })
	_, mid := e.quiz("GET", "/games/quiz/match/"+m, nil, b)
	if mid.OtherAnswered != n || mid.MyAnswered != 0 || mid.Complete {
		t.Errorf("b's view while a is done: %+v", mid)
	}
	for _, q := range mid.Questions {
		if q.Theirs != nil || q.Same != nil {
			t.Fatal("a's answers were revealed before b finished")
		}
	}

	// b agrees on the even questions and differs on the odd ones.
	e.answerAll(b, qa.ID, n, func(i int) int { return i % 2 })
	_, done := e.quiz("GET", "/games/quiz/match/"+m, nil, a)
	if !done.Complete || done.Score == nil {
		t.Fatalf("not complete: %+v", done)
	}
	want := (n + 1) / 2
	if *done.Score != want {
		t.Errorf("score %d, want %d", *done.Score, want)
	}
	for i, q := range done.Questions {
		if q.Theirs == nil || q.Same == nil || *q.Same != (i%2 == 0) {
			t.Errorf("question %d comparison wrong: %+v", i, q)
		}
	}
	// A finished quiz cannot be edited; a new one can be started.
	if c, _ := e.call("POST", "/games/quiz/"+qa.ID+"/answer", map[string]int{"question": 0, "choice": 1}, a); c != 422 {
		t.Errorf("edited a finished quiz: %d", c)
	}
	_, next := e.quiz("POST", "/games/quiz", map[string]string{"match_id": m, "set_id": "weekend"}, a)
	if next.ID == qa.ID || next.Complete {
		t.Errorf("did not start a fresh quiz: %+v", next)
	}
}

func TestQuizValidationAndAccess(t *testing.T) {
	e := setup(t)
	a, b, m := e.matched()
	outsider := e.user("Outsider")

	if c, _ := e.quiz("POST", "/games/quiz", map[string]string{"match_id": m, "set_id": "nope"}, a); c != 422 {
		t.Errorf("unknown set: %d", c)
	}
	if c, _ := e.quiz("POST", "/games/quiz", map[string]string{"match_id": m, "set_id": "everyday"}, outsider); c != 404 {
		t.Errorf("outsider start: %d", c)
	}
	_, q := e.quiz("POST", "/games/quiz", map[string]string{"match_id": m, "set_id": "everyday"}, a)
	for name, body := range map[string]map[string]int{
		"negative question": {"question": -1, "choice": 0},
		"question too big":  {"question": 99, "choice": 0},
		"choice too big":    {"question": 0, "choice": 9},
	} {
		if c, _ := e.call("POST", "/games/quiz/"+q.ID+"/answer", body, a); c != 422 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if c, _ := e.call("POST", "/games/quiz/"+q.ID+"/answer", map[string]int{"question": 0, "choice": 0}, outsider); c != 404 {
		t.Errorf("outsider answered: %d", c)
	}
	if c, _ := e.call("GET", "/games/quiz/match/"+m, nil, outsider); c != 404 {
		t.Errorf("outsider read: %d", c)
	}
	// Changing an answer before the end is allowed.
	e.call("POST", "/games/quiz/"+q.ID+"/answer", map[string]int{"question": 0, "choice": 1}, a)
	_, again := e.quiz("GET", "/games/quiz/match/"+m, nil, a)
	if again.MyAnswered != 1 || again.Questions[0].Mine != 1 {
		t.Errorf("answer not updated: %+v", again)
	}
	// Blocking ends access.
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(b)}, a)
	if c, _ := e.quiz("GET", "/games/quiz/match/"+m, nil, b); c != 404 {
		t.Errorf("a blocked person kept playing: %d", c)
	}
}

func TestQuizViewsCarryNoIdentity(t *testing.T) {
	e := setup(t)
	a, _, m := e.matched()
	_, raw := e.call("POST", "/games/quiz", map[string]string{"match_id": m, "set_id": "everyday"}, a)
	s := strings.ToLower(string(raw))
	for _, leak := range []string{"display_name", "email", "photo", "date_of_birth", "latitude"} {
		if strings.Contains(s, leak) {
			t.Errorf("quiz view leaks %q: %s", leak, s)
		}
	}
}

func TestGamesWorkInAnAnonymousRandomChat(t *testing.T) {
	e := setup(t)
	e.freshQueue()
	a, b := e.user("A"), e.user("B")
	e.randJoin(a)
	m := e.randJoin(b).Session.MatchID
	if c, _ := e.quiz("POST", "/games/quiz", map[string]string{"match_id": m, "set_id": "weekend"}, a); c != 200 {
		t.Errorf("quiz in a random chat: %d", c)
	}
	// ...but not once that chat has ended.
	e.exec(`UPDATE matches SET expires_at = NOW() - INTERVAL '1 second' WHERE id=$1::uuid`, m)
	if c, _ := e.quiz("GET", "/games/quiz/match/"+m, nil, b); c != 404 {
		t.Errorf("quiz after the chat expired: %d", c)
	}
}
