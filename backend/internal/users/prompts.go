package users

import (
	"context"
	"strings"

	"connectme/pkg/utils"
)

// PromptBank is the list of conversation-starter questions a member can answer
// on their profile. Keys are stable; the app shows the text (and may translate
// it). Every question invites a light, personal answer and none asks for
// contact details, money, a workplace or anything about other people.
var PromptBank = []PromptDef{
	{"perfect_sunday", "My perfect Sunday is"},
	{"two_truths", "Two truths and a lie"},
	{"unpopular_opinion", "My most unpopular opinion is"},
	{"dont_hate_me", "Don't hate me, but I"},
	{"best_trip", "The best trip I ever took was"},
	{"cant_live_without", "I can't live without"},
	{"green_flag", "A green flag I look for is"},
	{"geek_out", "I could talk for hours about"},
	{"simple_pleasure", "A simple pleasure I love is"},
	{"won_over_by", "You'll win me over if you"},
	{"weekend_plan", "On a free weekend you'll find me"},
	{"hidden_talent", "A talent nobody expects from me is"},
	{"comfort_food", "My comfort food is"},
	{"dream_trip", "The next place I want to see is"},
	{"song_on_repeat", "The song I have on repeat is"},
	{"fun_fact", "A fun fact about me is"},
	{"looking_for", "What I'm really looking for is"},
	{"date_idea", "My ideal first date is"},
	{"little_thing", "The little thing that makes my day is"},
	{"learning", "Something I'm learning right now is"},
	{"teach_me", "Teach me how to"},
	{"morning_person", "Morning person or night owl? I'm"},
	{"guilty_pleasure", "My guilty pleasure is"},
	{"life_goal", "A goal I'm working towards is"},
}

type PromptDef struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// PromptAnswer is one answered prompt on a profile.
type PromptAnswer struct {
	Key    string `json:"key"`
	Prompt string `json:"prompt"`
	Answer string `json:"answer"`
}

func promptText(key string) (string, bool) {
	for _, p := range PromptBank {
		if p.Key == key {
			return p.Text, true
		}
	}
	return "", false
}

// PromptIn is what the member submits: the question key and their answer.
type PromptIn struct {
	Key    string `json:"key"`
	Answer string `json:"answer"`
}

// GetPrompts returns the member's own answers in slot order.
func (s *Service) GetPrompts(ctx context.Context, userID string) ([]PromptAnswer, error) {
	rows, err := s.db.Query(ctx, `SELECT prompt_key, answer FROM profile_prompts WHERE user_id=$1::uuid ORDER BY slot`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PromptAnswer{}
	for rows.Next() {
		var a PromptAnswer
		if err := rows.Scan(&a.Key, &a.Answer); err != nil {
			return nil, err
		}
		a.Prompt, _ = promptText(a.Key)
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetPrompts replaces the member's answers (0 to 3, each question once).
// Answers are single-line text, screened like a bio.
func (s *Service) SetPrompts(ctx context.Context, userID string, in []PromptIn) ([]PromptAnswer, error) {
	if len(in) > 3 {
		return nil, ValidationError{"answer at most 3 prompts"}
	}
	seen := map[string]bool{}
	clean := make([]PromptIn, 0, len(in))
	for _, p := range in {
		if _, ok := promptText(p.Key); !ok {
			return nil, ValidationError{"unknown prompt"}
		}
		if seen[p.Key] {
			return nil, ValidationError{"each prompt can be answered once"}
		}
		seen[p.Key] = true
		a := utils.StripTags(p.Answer)
		if n := len([]rune(a)); n < 3 || n > 200 || strings.ContainsAny(a, "\r\n") {
			return nil, ValidationError{"each answer must be one line of 3-200 characters"}
		}
		if err := s.VetText(ctx, userID, "bio", a); err != nil {
			return nil, err
		}
		clean = append(clean, PromptIn{p.Key, a})
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `DELETE FROM profile_prompts WHERE user_id=$1::uuid`, userID); err != nil {
		return nil, err
	}
	for i, p := range clean {
		if _, err := tx.Exec(ctx, `INSERT INTO profile_prompts (user_id, slot, prompt_key, answer) VALUES ($1::uuid,$2,$3,$4)`, userID, i+1, p.Key, p.Answer); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetPrompts(ctx, userID)
}

// attachPrompts adds prompt answers to profiles shown in full. Anonymous
// profiles never get them: free text is the easiest way to give yourself away.
func (s *Service) attachPrompts(ctx context.Context, ps []PublicProfile, idx map[string][]int) {
	var refs []string
	for ref, is := range idx {
		for _, i := range is {
			if !ps[i].Anonymous {
				refs = append(refs, ref)
				break
			}
		}
	}
	if len(refs) == 0 {
		return
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.public_ref::text, q.prompt_key, q.answer FROM profile_prompts q JOIN profiles p ON p.user_id = q.user_id
		 WHERE p.public_ref = ANY($1::uuid[]) ORDER BY q.slot`, refs)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var ref, key, ans string
		if rows.Scan(&ref, &key, &ans) != nil {
			continue
		}
		text, _ := promptText(key)
		for _, i := range idx[ref] {
			if !ps[i].Anonymous {
				ps[i].Prompts = append(ps[i].Prompts, PromptAnswer{Key: key, Prompt: text, Answer: ans})
			}
		}
	}
}
