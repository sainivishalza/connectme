// Package random implements anonymous random matching: a person taps "start",
// is paired with another waiting person they have never been matched with and
// have no block with, and gets a short-lived, end-to-end encrypted chat in
// which both appear only as an anonymous incognito profile.
//
// The pairing is a normal row in `matches` (origin='random'), so chat,
// reveal, block, report, erasure and retention all reuse existing, tested
// paths. It expires after SessionLength unless both people keep it.
package random

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectme/internal/push"
	"connectme/internal/users"
	"connectme/pkg/metrics"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// SessionLength is how long a random chat lives unless both people keep it.
	SessionLength = 10 * time.Minute
	// WaitTTL is how long someone waits in the queue before it is dropped.
	WaitTTL = 90 * time.Second
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("only verified members can use random chat")
)

type Service struct {
	db       *pgxpool.Pool
	users    *users.Service
	notifier push.Notifier
	now      func() time.Time
}

// SetNotifier enables push notifications (nil keeps them off).
func (s *Service) SetNotifier(n push.Notifier) { s.notifier = n }

func NewService(db *pgxpool.Pool, u *users.Service) *Service {
	return &Service{db: db, users: u, now: time.Now}
}

// Session is what a client needs to show a random chat.
type Session struct {
	MatchID   string               `json:"match_id"`
	ExpiresAt *time.Time           `json:"expires_at"` // nil once both people kept it
	KeptByMe  bool                 `json:"kept_by_me"`
	Kept      bool                 `json:"kept"` // both kept: now a normal match
	Peer      *users.PublicProfile `json:"peer"`
	Style     string               `json:"style"` // chat | truth (Masked Truth)
}

// Status is the state of the caller's random-chat attempt.
type Status struct {
	State   string   `json:"state"` // idle | waiting | matched
	Session *Session `json:"session,omitempty"`
}

func (s *Service) requireVerified(ctx context.Context, tx pgx.Tx, userID string) error {
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, userID).Scan(&ok); err != nil || !ok {
		return ErrForbidden
	}
	return nil
}

// Join enters the queue, or pairs immediately with someone already waiting.
// Joining again while already paired or waiting is idempotent.
func (s *Service) Join(ctx context.Context, userID, style string) (*Status, error) {
	if style == "" {
		style = "chat"
	}
	if style != "chat" && style != "truth" {
		return nil, users.ValidationError{Msg: "style must be chat or truth"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := s.requireVerified(ctx, tx, userID); err != nil {
		return nil, err
	}
	// Pairing is serialised: two people can never be handed the same partner.
	// This is fine at modest scale; a busy deployment would shard the queue.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('random_pairing'))`); err != nil {
		return nil, err
	}
	if active, err := s.activeMatch(ctx, tx, userID); err != nil {
		return nil, err
	} else if active != "" {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return s.Status(ctx, userID)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM random_waiting WHERE joined_at < $1`, s.now().Add(-WaitTTL)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM random_waiting WHERE user_id=$1`, userID); err != nil {
		return nil, err
	}

	// Prefer the longest-waiting candidate who shares the most interests.
	var partner string
	err = tx.QueryRow(ctx, `
		SELECT w.user_id::text
		  FROM random_waiting w
		  JOIN users u ON u.id = w.user_id AND u.is_verified AND NOT u.is_banned
		  JOIN profiles pw ON pw.user_id = w.user_id
		  JOIN profiles pm ON pm.user_id = $1
		 WHERE w.user_id <> $1 AND w.style = $2
		   AND `+users.NotBlockedSQL("w.user_id", "$1::uuid")+`
		   AND NOT EXISTS (SELECT 1 FROM matches m
		                    WHERE m.user1_id = LEAST(w.user_id, $1::uuid) AND m.user2_id = GREATEST(w.user_id, $1::uuid))
		 ORDER BY cardinality(ARRAY(SELECT unnest(pw.tags) INTERSECT SELECT unnest(pm.tags))) DESC, w.joined_at
		 LIMIT 1
		 FOR UPDATE OF w SKIP LOCKED`, userID, style).Scan(&partner)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := tx.Exec(ctx, `INSERT INTO random_waiting (user_id, style) VALUES ($1,$2)`, userID, style); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		metrics.Event("random_waiting")
		return &Status{State: "waiting"}, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM random_waiting WHERE user_id = $1`, partner); err != nil {
		return nil, err
	}
	u1, u2 := userID, partner
	if u2 < u1 {
		u1, u2 = u2, u1
	}
	var pairID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO matches (user1_id, user2_id, status, mode, origin, expires_at, style)
		VALUES ($1,$2,'matched','incognito','random',$3,$4) RETURNING id::text`, u1, u2, s.now().Add(SessionLength), style).Scan(&pairID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	metrics.Event("random_paired")
	if s.notifier != nil {
		s.notifier.Notify(ctx, partner, push.KindMatch, pairID) // they were waiting; the joiner sees it at once
	}
	return s.Status(ctx, userID)
}

// activeMatch returns the caller's live random match id, or "".
func (s *Service) activeMatch(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, userID string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		SELECT id::text FROM matches
		 WHERE origin='random' AND status='matched' AND $1::uuid IN (user1_id, user2_id)
		   AND (expires_at IS NULL OR expires_at > $2) AND cardinality(kept_by) < 2
		 ORDER BY created_at DESC LIMIT 1`, userID, s.now()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// Status reports whether the caller is idle, waiting, or in a random chat.
func (s *Service) Status(ctx context.Context, userID string) (*Status, error) {
	id, err := s.activeMatch(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	if id != "" {
		sess, err := s.session(ctx, userID, id)
		if errors.Is(err, users.ErrNotFound) {
			return &Status{State: "idle"}, nil // the peer was banned or blocked meanwhile
		}
		if err != nil {
			return nil, err
		}
		return &Status{State: "matched", Session: sess}, nil
	}
	var waiting bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM random_waiting WHERE user_id=$1 AND joined_at >= $2)`,
		userID, s.now().Add(-WaitTTL)).Scan(&waiting); err != nil {
		return nil, err
	}
	if waiting {
		return &Status{State: "waiting"}, nil
	}
	return &Status{State: "idle"}, nil
}

func (s *Service) session(ctx context.Context, userID, matchID string) (*Session, error) {
	var other string
	var expires *time.Time
	var keptMe bool
	var keptN int
	var style string
	err := s.db.QueryRow(ctx, `
		SELECT CASE WHEN user1_id=$1 THEN user2_id ELSE user1_id END::text, expires_at,
		       $1::uuid = ANY(kept_by), cardinality(kept_by), style
		  FROM matches WHERE id=$2::uuid AND origin='random' AND status='matched'`, userID, matchID).
		Scan(&other, &expires, &keptMe, &keptN, &style)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	card, err := s.users.Card(ctx, userID, other, "")
	if err != nil {
		return nil, err
	}
	return &Session{MatchID: matchID, ExpiresAt: expires, KeptByMe: keptMe, Kept: keptN >= 2, Peer: card, Style: style}, nil
}

// Cancel leaves the queue.
func (s *Service) Cancel(ctx context.Context, userID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM random_waiting WHERE user_id=$1`, userID)
	return err
}

// Keep records that the caller wants to continue. When both people have, the
// session stops expiring and is a normal incognito match from then on.
func (s *Service) Keep(ctx context.Context, userID, matchID string) (*Session, error) {
	if _, err := uuid.Parse(matchID); err != nil {
		return nil, ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE matches
		   SET kept_by = CASE WHEN $1::uuid = ANY(kept_by) THEN kept_by ELSE array_append(kept_by, $1::uuid) END
		 WHERE id=$2::uuid AND origin='random' AND status='matched' AND $1::uuid IN (user1_id, user2_id)
		   AND (expires_at IS NULL OR expires_at > $3)`, userID, matchID, s.now())
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	if _, err := s.db.Exec(ctx, `UPDATE matches SET expires_at = NULL WHERE id=$1::uuid AND cardinality(kept_by) >= 2`, matchID); err != nil {
		return nil, err
	}
	return s.session(ctx, userID, matchID)
}

// Expire ends random chats whose time ran out without both people keeping
// them, and drops stale queue entries. Ending is a normal "rejected" match, so
// the chat retention sweeper then purges the messages, and the pair is never
// matched with each other again.
func (s *Service) Expire(ctx context.Context) (ended int, err error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE matches SET status='rejected'
		 WHERE origin='random' AND status='matched' AND expires_at IS NOT NULL AND expires_at <= $1`, s.now())
	if err != nil {
		return 0, err
	}
	if _, err := s.db.Exec(ctx, `
		DELETE FROM reveal_consents WHERE match_id IN
		  (SELECT id FROM matches WHERE origin='random' AND status='rejected')`); err != nil {
		return int(tag.RowsAffected()), err
	}
	_, err = s.db.Exec(ctx, `DELETE FROM random_waiting WHERE joined_at < $1`, s.now().Add(-WaitTTL))
	return int(tag.RowsAffected()), err
}

// StartJob runs Expire every interval until ctx is cancelled.
func (s *Service) StartJob(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			started := time.Now()
			n, err := s.Expire(ctx)
			metrics.Job("random_expiry", time.Since(started), err)
			metrics.JobItems("random_expiry", "ended", n)
			if err != nil {
				slog.Error("random: expiry job", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
