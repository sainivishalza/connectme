package integration

import (
	"strings"
	"testing"
)

func (e *env) answerQuiz(uid string, a map[string]int) int {
	e.t.Helper()
	c, _ := e.call("PUT", "/quiz/answers", map[string]any{"answers": a}, uid)
	return c
}

var quizIDs = []string{"weekend", "morning", "plans", "social", "conflict", "money", "travel", "pets", "fitness", "food"}

func TestQuizMatchIsRoundedNeedsEnoughSharedAnswersAndShowsOnAnonymousCards(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	me, twin, half, few := e.user("Quiz Me"), e.user("Quiz Twin"), e.user("Quiz Half"), e.user("Quiz Few")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, twin)
	for _, u := range []string{me, twin, half, few} {
		e.locate(u, lat, lng)
	}

	// Validation.
	if c := e.answerQuiz(me, map[string]int{"nope": 1}); c != 422 {
		t.Errorf("unknown question: %d", c)
	}
	if c := e.answerQuiz(me, map[string]int{"weekend": 9}); c != 422 {
		t.Errorf("bad option: %d", c)
	}
	all, halfA, fewA := map[string]int{}, map[string]int{}, map[string]int{}
	for i, id := range quizIDs {
		all[id] = 1
		halfA[id] = 1
		if i%2 == 0 {
			halfA[id] = 2
		}
		if i < 5 {
			fewA[id] = 1
		}
	}
	for u, a := range map[string]map[string]int{me: all, twin: all, half: halfA, few: fewA} {
		if c := e.answerQuiz(u, a); c != 204 {
			t.Fatalf("answers: %d", c)
		}
	}

	_, b := e.call("GET", "/users/"+e.refOf(twin), nil, me)
	if !strings.Contains(string(b), `"quiz_match":100`) || !strings.Contains(string(b), `"anonymous":true`) {
		t.Errorf("identical answers on an anonymous card: %s", b)
	}
	_, b = e.call("GET", "/users/"+e.refOf(half), nil, me)
	if !strings.Contains(string(b), `"quiz_match":50`) {
		t.Errorf("half match: %s", b)
	}
	_, b = e.call("GET", "/users/"+e.refOf(few), nil, me)
	if strings.Contains(string(b), "quiz_match") {
		t.Errorf("only 5 shared answers must show nothing: %s", b)
	}
	// Answers themselves are never exposed.
	if strings.Contains(string(b), `"answers"`) || strings.Contains(string(b), "weekend") {
		t.Errorf("answers leaked: %s", b)
	}

	// Removing an answer works, and the quiz screen lists the bank.
	if c := e.answerQuiz(twin, map[string]int{"weekend": -1}); c != 204 {
		t.Errorf("remove: %d", c)
	}
	c, b := e.call("GET", "/quiz/", nil, twin)
	if c != 200 || !strings.Contains(string(b), `"questions"`) || strings.Contains(string(b), `"weekend":`) {
		t.Errorf("quiz get: %d %.200s", c, b)
	}
}

// Flipping answers one at a time is how someone would read another person's
// answers off their match percentage, so changes are capped per day.
func TestQuizChangesAreCappedButFirstAnswersAndRepeatsAreFree(t *testing.T) {
	e := setup(t)
	u := e.user("Quiz Prober")
	first := map[string]int{}
	for _, id := range quizIDs {
		first[id] = 0
	}
	if c := e.answerQuiz(u, first); c != 204 {
		t.Fatalf("first answers: %d", c)
	}
	if c := e.answerQuiz(u, first); c != 204 { // saving the same answers is not a change
		t.Errorf("repeat: %d", c)
	}
	for i := 0; i < 6; i++ {
		if c := e.answerQuiz(u, map[string]int{quizIDs[i]: 1}); c != 204 {
			t.Fatalf("change %d: %d", i+1, c)
		}
	}
	if c := e.answerQuiz(u, map[string]int{quizIDs[6]: 1}); c != 429 {
		t.Errorf("seventh change in a day: %d, want 429", c)
	}
	if c := e.answerQuiz(u, map[string]int{quizIDs[7]: -1}); c != 429 {
		t.Errorf("removing counts as a change: %d, want 429", c)
	}
	// A different member is not affected, and the refused change was not saved.
	other := e.user("Quiz Other")
	if c := e.answerQuiz(other, map[string]int{"weekend": 2}); c != 204 {
		t.Errorf("other member: %d", c)
	}
	if n := e.count(`SELECT COUNT(*) FROM compat_answers WHERE user_id=$1::uuid AND qid=$2 AND choice=0`, u, quizIDs[6]); n != 1 {
		t.Error("refused change was applied")
	}
}
