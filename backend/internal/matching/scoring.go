package matching

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Scorer computes a compatibility score. The AI service implements it; when
// it is absent or down, matches simply have no score (nothing depends on one).
type Scorer interface {
	Score(ctx context.Context, in ScoreInput) (*ScoreResult, error)
}

// HistoryProvider supplies conversation *metadata* (message counts per
// direction). Chats are end-to-end encrypted, so content is never available.
type HistoryProvider interface {
	Counts(ctx context.Context, matchID, userA, userB string) (aToB, bToA int, err error)
}

type ScorePerson struct {
	Tags        []string
	Age         *int
	ActiveHours []int
}

type ScoreInput struct {
	A, B ScorePerson
	AToB int
	BToA int
}

type ScoreReason struct{ Code, Text string }

type ScoreResult struct {
	Score   int
	Reasons []ScoreReason
}

// Compat is the score as returned to participants.
type Compat struct {
	Score   int      `json:"compat_score"`
	Reasons []string `json:"compat_reasons"`
}

// SetScorer enables compatibility scoring (history may be nil).
func (s *Service) SetScorer(sc Scorer, h HistoryProvider) {
	s.scorer, s.history = sc, h
}

// scoreAsync scores a new match in the background. It never blocks or fails
// the swipe that created the match.
func (s *Service) scoreAsync(matchID string) {
	if s.scorer == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("matching: scoring panicked", "recover", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := s.score(ctx, matchID); err != nil {
			slog.Warn("matching: scoring skipped", "err", err)
		}
	}()
}

// Rescore recomputes a match's score (e.g. after conversation has started).
// Only participants may ask.
func (s *Service) Rescore(ctx context.Context, userID, matchID string) (*Compat, error) {
	if s.scorer == nil {
		return nil, ErrNotFound
	}
	if _, err := uuid.Parse(matchID); err != nil {
		return nil, ErrNotFound
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM matches WHERE id=$1::uuid AND status='matched' AND $2::uuid IN (user1_id, user2_id))`,
		matchID, userID).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return s.score(ctx, matchID)
}

func (s *Service) score(ctx context.Context, matchID string) (*Compat, error) {
	var (
		u1, u2, mode string
		t1, t2       []string
		a1, a2       *int
	)
	err := s.db.QueryRow(ctx, `
		SELECT m.user1_id::text, m.user2_id::text, m.mode,
		       p1.tags, date_part('year', age(x1.date_of_birth))::int,
		       p2.tags, date_part('year', age(x2.date_of_birth))::int
		  FROM matches m
		  JOIN profiles p1 ON p1.user_id = m.user1_id JOIN users x1 ON x1.id = m.user1_id
		  JOIN profiles p2 ON p2.user_id = m.user2_id JOIN users x2 ON x2.id = m.user2_id
		 WHERE m.id = $1::uuid AND m.status = 'matched'`, matchID).Scan(&u1, &u2, &mode, &t1, &a1, &t2, &a2)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	h1, err := s.activeHours(ctx, u1)
	if err != nil {
		return nil, err
	}
	h2, err := s.activeHours(ctx, u2)
	if err != nil {
		return nil, err
	}
	in := ScoreInput{A: ScorePerson{t1, a1, h1}, B: ScorePerson{t2, a2, h2}}
	if s.history != nil {
		if in.AToB, in.BToA, err = s.history.Counts(ctx, matchID, u1, u2); err != nil {
			slog.Warn("matching: history unavailable", "err", err)
			in.AToB, in.BToA = 0, 0
		}
	}
	res, err := s.scorer.Score(ctx, in)
	if err != nil {
		return nil, err
	}
	c := &Compat{Score: res.Score, Reasons: []string{}}
	for _, r := range res.Reasons {
		// Age proximity would reveal approximate age before a reveal.
		if r.Code == "similar_age" && users.Mode(mode) != users.ModeOpen {
			continue
		}
		c.Reasons = append(c.Reasons, r.Text)
	}
	rs, _ := json.Marshal(c.Reasons)
	_, err = s.db.Exec(ctx,
		`UPDATE matches SET compat_score=$2, compat_reasons=$3, scored_at=NOW() WHERE id=$1::uuid`,
		matchID, c.Score, rs)
	return c, err
}

// activeHours returns the UTC hours in which a user has been swiping at
// least twice in the last 30 days: a crude "when are they online" signal
// built only from timestamps we already hold.
func (s *Service) activeHours(ctx context.Context, userID string) ([]int, error) {
	rows, err := s.db.Query(ctx, `
		SELECT h FROM (
		  SELECT EXTRACT(hour FROM created_at AT TIME ZONE 'UTC')::int AS h
		    FROM swipes WHERE actor_id = $1 AND created_at > NOW() - INTERVAL '30 days') t
		 GROUP BY h HAVING COUNT(*) >= 2 ORDER BY h`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var h int
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
