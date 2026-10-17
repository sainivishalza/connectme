package users

import (
	"context"
	"encoding/json"
	"log/slog"

	"connectme/pkg/metrics"
)

// Decision is a content-moderation outcome.
type Decision struct {
	Action     string // allow | review | block
	Reason     string
	Categories []string
}

// ContentChecker screens public user-generated content. Implementations call
// the AI service; chats are end-to-end encrypted and are never sent here.
type ContentChecker interface {
	CheckText(ctx context.Context, kind, text string) (Decision, error)
	CheckImage(ctx context.Context, data []byte) (Decision, error)
}

// SetChecker installs content screening (nil disables it).
func (s *Service) SetChecker(c ContentChecker) { s.checker = c }

// VetText applies moderation policy to public text:
//
//   - block  -> the request is refused with a safe, generic message;
//   - review -> the content is accepted but queued for a human (it appears in
//     the moderators' report queue with no reporter);
//   - AI service unavailable -> fail OPEN. Screening is a best-effort layer
//     on top of reports and human review; an outage must not stop people
//     from using the app. The failure is logged.
func (s *Service) VetText(ctx context.Context, userID, kind, text string) error {
	if s.checker == nil || text == "" {
		return nil
	}
	d, err := s.checker.CheckText(ctx, kind, text)
	if err != nil {
		slog.Warn("moderation: text check unavailable, allowing", "kind", kind, "err", err)
		return nil
	}
	return s.apply(ctx, userID, kind, text, d)
}

// VetImage is VetText for uploaded images (kind "photo").
func (s *Service) VetImage(ctx context.Context, userID string, data []byte) error {
	if s.checker == nil {
		return nil
	}
	d, err := s.checker.CheckImage(ctx, data)
	if err != nil {
		slog.Warn("moderation: image check unavailable, allowing", "err", err)
		return nil
	}
	metrics.Moderation("photo", d.Action)
	if d.Action == "block" {
		return ValidationError{Msg: orDefault(d.Reason, "This image is not allowed.")}
	}
	if d.Action == "review" {
		s.flag(ctx, userID, "photo", "(image held for review)", d.Categories)
	}
	return nil
}

func (s *Service) apply(ctx context.Context, userID, kind, text string, d Decision) error {
	metrics.Moderation(kind, d.Action)
	switch d.Action {
	case "block":
		slog.Info("moderation: content blocked", "kind", kind, "categories", d.Categories)
		return ValidationError{Msg: orDefault(d.Reason, "This content breaks our community rules.")}
	case "review":
		s.flag(ctx, userID, kind, text, d.Categories)
	}
	return nil
}

// flag records auto-flagged content in the moderators' queue.
func (s *Service) flag(ctx context.Context, userID, kind, text string, cats []string) {
	ev, _ := json.Marshal([]string{text})
	_, err := s.db.Exec(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason, details, evidence)
		VALUES (NULL, $1, 'auto_review', $2, $3)`,
		userID, kind+": "+joinCats(cats), ev)
	if err != nil {
		slog.Error("moderation: could not queue flagged content", "err", err)
	}
}

func joinCats(c []string) string {
	out := ""
	for i, s := range c {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
