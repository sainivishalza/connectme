package games

import (
	"strings"
	"testing"
)

// banned words for any text in the bank: a tripwire for future edits, not a
// substitute for reading what you add.
var banned = []string{
	"alcohol", "drunk", "beer", "wine", "shots", "vodka", "drinks", "naked", "nude", "sex", "kiss", "strip", "lingerie",
	"address", "phone number", "salary", "how much do you earn", "money", "password", "send me", "photo of",
	"selfie", "where do you live", "full name", "workplace", "bank", "trespass", "steal",
}

func noBannedWords(t *testing.T, kind string, items []string) {
	t.Helper()
	for _, s := range items {
		low := strings.ToLower(s)
		for _, b := range banned {
			if strings.Contains(low, b) {
				t.Errorf("%s %q contains %q", kind, s, b)
			}
		}
	}
}

func TestPromptBankFollowsTheContentRules(t *testing.T) {
	for kind, items := range map[string][]string{"truth": truths, "dare": dares, "icebreaker": icebreakers} {
		noBannedWords(t, kind, items)
		seen := map[string]bool{}
		for _, s := range items {
			if seen[strings.ToLower(s)] {
				t.Errorf("duplicate %s: %q", kind, s)
			}
			seen[strings.ToLower(s)] = true
			if n := len([]rune(s)); n < 12 || n > 200 {
				t.Errorf("%s %q has an odd length (%d)", kind, s, n)
			}
		}
		if len(items) < 20 {
			t.Errorf("only %d %s prompts", len(items), kind)
		}
	}
}

func TestQuizBankFollowsTheContentRules(t *testing.T) {
	ids := map[string]bool{}
	for _, q := range Quizzes {
		if ids[q.ID] {
			t.Errorf("duplicate quiz id %s", q.ID)
		}
		ids[q.ID] = true
		if len(q.Questions) < 6 {
			t.Errorf("quiz %s has only %d questions", q.ID, len(q.Questions))
		}
		for _, qu := range q.Questions {
			if len(qu.Options) < 2 || len(qu.Options) > 4 {
				t.Errorf("%s %q has %d options", q.ID, qu.Text, len(qu.Options))
			}
			noBannedWords(t, "quiz "+q.ID, append([]string{qu.Text}, qu.Options...))
		}
	}
}

func TestPromptKinds(t *testing.T) {
	s := &Service{}
	for _, k := range []string{KindTruth, KindDare, KindIcebreaker} {
		p, err := s.Prompt(k)
		if err != nil || p.Text == "" || p.Kind != k {
			t.Errorf("%s: %+v %v", k, p, err)
		}
	}
	if _, err := s.Prompt("spicy"); err == nil {
		t.Error("unknown kind accepted")
	}
}
