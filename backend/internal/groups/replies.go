package groups

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"connectme/internal/users"
)

// ChatLine is one message a member chose to share with the AI wingman. There
// are no names or ids: only whether the member wrote it ("me") or somebody
// else did ("other"). Chats are end-to-end encrypted, so the server never
// sees chat text except what a member explicitly sends here; it is passed on
// and neither stored nor logged.
type ChatLine struct {
	Who  string `json:"who"`
	Text string `json:"text"`
}

type ReplyIdeas struct {
	Replies []string `json:"replies"`
	Source  string   `json:"source"` // "llm", "ai" (templates) or "fallback"
}

// Replier turns shared chat lines into reply ideas.
type Replier interface {
	Replies(ctx context.Context, lines []ChatLine) (ReplyIdeas, error)
}

var replyFallback = []string{
	"That sounds fun, what made you pick it?",
	"Ha, I love that. Tell me more!",
	"Count me in, when were you thinking?",
}

func cleanLines(in []ChatLine) ([]ChatLine, error) {
	if len(in) < 1 || len(in) > 8 {
		return nil, users.ValidationError{Msg: "share between 1 and 8 messages"}
	}
	out := make([]ChatLine, 0, len(in))
	for _, l := range in {
		if l.Who != "me" && l.Who != "other" {
			return nil, users.ValidationError{Msg: "each message needs a sender of me or other"}
		}
		t := strings.Join(strings.Fields(l.Text), " ")
		if n := utf8.RuneCountInString(t); n < 1 || n > 300 {
			return nil, users.ValidationError{Msg: "each message must be 1-300 characters"}
		}
		for _, c := range t {
			if unicode.IsControl(c) {
				return nil, users.ValidationError{Msg: "a message contains invalid characters"}
			}
		}
		out = append(out, ChatLine{Who: l.Who, Text: t})
	}
	return out, nil
}

// ReplyIdeas checks that the caller belongs to one of the two groups of a
// matched group chat, then asks the wingman for reply ideas.
func (s *Service) ReplyIdeas(ctx context.Context, userID, groupMatchID string, lines []ChatLine) (*ReplyIdeas, error) {
	clean, err := cleanLines(lines)
	if err != nil {
		return nil, err
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM group_matches gm JOIN group_members m ON m.group_id IN (gm.group_a, gm.group_b)
		   WHERE gm.id = $1::uuid AND gm.status = 'matched' AND m.user_id = $2::uuid)`, groupMatchID, userID).Scan(&ok); err != nil {
		return nil, ErrNotFound // a malformed id is the same as a stranger's
	}
	if !ok {
		return nil, ErrNotFound
	}
	if s.replier == nil {
		return &ReplyIdeas{Replies: replyFallback, Source: "fallback"}, nil
	}
	out, err := s.replier.Replies(ctx, clean)
	if err != nil || len(out.Replies) == 0 {
		return &ReplyIdeas{Replies: replyFallback, Source: "fallback"}, nil
	}
	return &out, nil
}

// Replies calls the AI service's /wingman/reply.
func (h *HTTPSuggester) Replies(ctx context.Context, lines []ChatLine) (ReplyIdeas, error) {
	body, _ := json.Marshal(map[string]any{"lines": lines})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.BaseURL+"/wingman/reply", bytes.NewReader(body))
	if err != nil {
		return ReplyIdeas{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.Key != "" {
		req.Header.Set("X-Internal-Key", h.Key)
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return ReplyIdeas{}, err
	}
	defer resp.Body.Close()
	var out ReplyIdeas
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&out) != nil {
		return ReplyIdeas{}, io.ErrUnexpectedEOF
	}
	if len(out.Replies) > 3 {
		out.Replies = out.Replies[:3]
	}
	if out.Source != "llm" {
		out.Source = "ai"
	}
	return out, nil
}
