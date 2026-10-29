package users

import (
	"context"
	"encoding/json"
	"errors"
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

// SetChecker installs content screening.
func (s *Service) SetChecker(c ContentChecker) { s.checker = c }

// SetDevBypass lets content through unscreened when screening cannot run.
// It exists for development and tests only: config refuses it in production.
func (s *Service) SetDevBypass(on bool) { s.devBypass = on }

// ErrModerationUnavailable is returned when public content cannot be screened
// (no checker configured, the AI service errored or timed out, or the circuit
// breaker is open). Handlers answer 503 with Retry-After: the content is
// refused, never let through unchecked.
var ErrModerationUnavailable = errors.New("moderation_unavailable")

// unavailable is the one place that decides what "cannot screen" means.
func (s *Service) unavailable(what string, err error) error {
	if s.devBypass {
		slog.Warn("moderation: unavailable, letting content through (development bypass)", "what", what, "err", err)
		return nil
	}
	slog.Warn("moderation: unavailable, refusing content", "what", what, "err", err)
	metrics.Moderation(what, "unavailable")
	return ErrModerationUnavailable
}

// VetText applies moderation policy to public text:
//
//   - block  -> the request is refused with a safe, generic message;
//   - review -> the content is accepted but queued for a human (it appears in
//     the moderators' report queue with no reporter);
//   - screening unavailable -> the request is refused with
//     ErrModerationUnavailable (fail closed).
func (s *Service) VetText(ctx context.Context, userID, kind, text string) error {
	if text == "" {
		return nil
	}
	if s.checker == nil {
		return s.unavailable(kind, errors.New("no content checker configured"))
	}
	d, err := s.checker.CheckText(ctx, kind, text)
	if err != nil {
		return s.unavailable(kind, err)
	}
	return s.apply(ctx, userID, kind, text, d)
}

// VetImage is VetText for uploaded images (kind "photo").
func (s *Service) VetImage(ctx context.Context, userID string, data []byte) error {
	if s.checker == nil {
		return s.unavailable("photo", errors.New("no content checker configured"))
	}
	d, err := s.checker.CheckImage(ctx, data)
	if err != nil {
		return s.unavailable("photo", err)
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

// scanFailed refuses an upload whose CSAM scan could not run. Unlike content
// screening there is no development bypass: the development scanner (csam.Stub)
// answers "clean" itself.
func (s *Service) scanFailed(what string, err error) error {
	slog.Warn("csam: scan unavailable, upload refused", "what", what, "err", err)
	metrics.Moderation(what, "unavailable")
	return ErrModerationUnavailable
}
