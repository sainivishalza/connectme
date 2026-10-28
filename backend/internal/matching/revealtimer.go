package matching

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectme/internal/users"
	"connectme/pkg/metrics"

	"github.com/jackc/pgx/v5"
)

// RevealTimer is where an anonymous match stands on a timed reveal.
type RevealTimer struct {
	At         time.Time `json:"at"`
	ByMe       bool      `json:"by_me"`       // I proposed it
	Accepted   bool      `json:"accepted"`    // both agreed; it will happen at At
	NeedsReply bool      `json:"needs_reply"` // they proposed and I have not answered
}

const (
	minRevealHours = 1
	maxRevealHours = 72
	proposalLapse  = 24 * time.Hour
)

// ProposeReveal offers to reveal both people at a set time. If the other
// person already proposed, this agrees to theirs instead. Only anonymous
// matches have a reveal to schedule.
func (s *Service) ProposeReveal(ctx context.Context, userID, matchID string, hours int) (*RevealTimer, error) {
	if hours < minRevealHours || hours > maxRevealHours {
		return nil, users.ValidationError{Msg: "choose between 1 and 72 hours"}
	}
	if _, err := s.participant(ctx, userID, matchID); err != nil {
		return nil, err
	}
	var anonymous bool
	if err := s.db.QueryRow(ctx, `SELECT mode <> 'open' FROM matches WHERE id=$1::uuid`, matchID).Scan(&anonymous); err != nil || !anonymous {
		return nil, users.ValidationError{Msg: "this match is not anonymous"}
	}
	var ownerOfProposal string
	var acceptedBy *string
	err := s.db.QueryRow(ctx, `SELECT proposed_by::text, accepted_by::text FROM reveal_schedules WHERE match_id=$1::uuid AND created_at > $2`,
		matchID, time.Now().Add(-proposalLapse)).Scan(&ownerOfProposal, &acceptedBy)
	switch {
	case err == nil && acceptedBy == nil && ownerOfProposal != userID:
		// They proposed first: agreeing makes it final.
		if _, err := s.db.Exec(ctx, `UPDATE reveal_schedules SET accepted_by=$2::uuid WHERE match_id=$1::uuid`, matchID, userID); err != nil {
			return nil, err
		}
	case err == nil && acceptedBy != nil:
		return nil, users.ValidationError{Msg: "a reveal is already agreed; cancel it to change the time"}
	case err == nil || errors.Is(err, pgx.ErrNoRows):
		if _, err := s.db.Exec(ctx, `
			INSERT INTO reveal_schedules (match_id, proposed_by, reveal_at) VALUES ($1::uuid,$2::uuid,NOW() + make_interval(hours => $3))
			ON CONFLICT (match_id) DO UPDATE SET proposed_by=$2::uuid, reveal_at=NOW() + make_interval(hours => $3), accepted_by=NULL, created_at=NOW()`,
			matchID, userID, hours); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	st, err := s.revealTimers(ctx, userID, map[string]string{matchID: ""})
	if err != nil {
		return nil, err
	}
	t := st[matchID]
	return &t, nil
}

// CancelReveal withdraws a proposal or an agreed timer; either person may.
func (s *Service) CancelReveal(ctx context.Context, userID, matchID string) error {
	if _, err := s.participant(ctx, userID, matchID); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `DELETE FROM reveal_schedules WHERE match_id=$1::uuid`, matchID)
	return err
}

// revealTimers returns the timer for each match that has a live one.
func (s *Service) revealTimers(ctx context.Context, userID string, matches map[string]string) (map[string]RevealTimer, error) {
	ids := make([]string, 0, len(matches))
	for id := range matches {
		ids = append(ids, id)
	}
	rows, err := s.db.Query(ctx, `
		SELECT match_id::text, reveal_at, proposed_by = $1::uuid, accepted_by IS NOT NULL
		  FROM reveal_schedules
		 WHERE match_id = ANY($2::uuid[]) AND (accepted_by IS NOT NULL OR created_at > $3)`, userID, ids, time.Now().Add(-proposalLapse))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]RevealTimer{}
	for rows.Next() {
		var id string
		var t RevealTimer
		if err := rows.Scan(&id, &t.At, &t.ByMe, &t.Accepted); err != nil {
			return nil, err
		}
		t.NeedsReply = !t.Accepted && !t.ByMe
		out[id] = t
	}
	return out, rows.Err()
}

// RunDueReveals completes every agreed timer that has come due: both people's
// consents are recorded, which is exactly what each pressing "reveal" does. A
// match that has since ended is skipped. Stale proposals are removed.
func (s *Service) RunDueReveals(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `
		DELETE FROM reveal_schedules r USING matches m
		 WHERE m.id = r.match_id AND r.accepted_by IS NOT NULL AND r.reveal_at <= NOW()
		RETURNING r.match_id::text, r.proposed_by::text, r.accepted_by::text, m.status`)
	if err != nil {
		return 0, err
	}
	type due struct{ match, a, b, status string }
	var list []due
	for rows.Next() {
		var d due
		if rows.Scan(&d.match, &d.a, &d.b, &d.status) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	done := 0
	for _, d := range list {
		if d.status != "matched" {
			continue
		}
		if _, err := s.db.Exec(ctx, `
			INSERT INTO reveal_consents (match_id, user_id) VALUES ($1::uuid,$2::uuid), ($1::uuid,$3::uuid) ON CONFLICT DO NOTHING`,
			d.match, d.a, d.b); err != nil {
			return done, err
		}
		done++
		metrics.Event("reveal_timer_fired")
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM reveal_schedules WHERE accepted_by IS NULL AND created_at <= $1`, time.Now().Add(-proposalLapse)); err != nil {
		return done, err
	}
	return done, nil
}

// StartRevealJob runs RunDueReveals every interval until ctx ends.
func (s *Service) StartRevealJob(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			started := time.Now()
			_, err := s.RunDueReveals(ctx)
			metrics.Job("reveal_timer", time.Since(started), err)
			if err != nil && ctx.Err() == nil {
				slog.Error("matching: reveal timer job", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
