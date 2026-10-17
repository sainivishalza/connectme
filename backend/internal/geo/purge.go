package geo

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectme/pkg/metrics"
)

// JobResult reports what one pass of the retention job did.
type JobResult struct {
	AutoCheckedOut int // max stay reached or left the fence
	Purged         int // check-ins (and everything bound to them) deleted
}

// RunOnce is one pass of the retention job. Every step is idempotent, so it
// is safe to run on several replicas or to re-run after a crash.
//
//  1. Check out people past MaxStay, or whose fresh location is outside the
//     fence by more than ExitGraceMeters.
//  2. Delete check-ins whose auto_delete_at has passed. Rows bound to a
//     check-in (matches, reveal consents, swipes, board posts, drinks) go with
//     it via ON DELETE CASCADE; external stores are purged first via Purgers,
//     so a failure there leaves the row in place and the next pass retries.
//  3. Safety net: nobody stays in hotel mode without an active check-in.
func (s *Service) RunOnce(ctx context.Context) (JobResult, error) {
	var res JobResult

	rows, err := s.db.Query(ctx, `
		UPDATE hotel_checkins c
		   SET checked_out_at = NOW(), auto_delete_at = NOW() + $1::interval
		  FROM hotels h, profiles p
		 WHERE c.checked_out_at IS NULL AND h.id = c.hotel_id AND p.user_id = c.user_id
		   AND ( c.checked_in_at + $2::interval <= NOW()
		      OR ( p.location IS NOT NULL
		           AND p.location_updated_at > c.checked_in_at + INTERVAL '2 minutes'
		           AND p.location_updated_at > NOW() - INTERVAL '15 minutes'
		           AND NOT ST_DWithin(h.geofence, p.location, $3::float8) ) )
		RETURNING c.user_id::text`,
		RetentionAfterCheckout.String(), MaxStay.String(), float64(ExitGraceMeters))
	if err != nil {
		return res, fmt.Errorf("auto checkout: %w", err)
	}
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return res, err
		}
		out = append(out, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	if err := restoreModes(ctx, s.db, out); err != nil {
		return res, fmt.Errorf("restore modes: %w", err)
	}
	res.AutoCheckedOut = len(out)

	due, err := s.db.Query(ctx,
		`SELECT id::text FROM hotel_checkins WHERE auto_delete_at <= NOW() ORDER BY auto_delete_at LIMIT 200`)
	if err != nil {
		return res, err
	}
	var ids []string
	for due.Next() {
		var id string
		if err := due.Scan(&id); err != nil {
			due.Close()
			return res, err
		}
		ids = append(ids, id)
	}
	due.Close()
	if err := due.Err(); err != nil {
		return res, err
	}
	for _, id := range ids {
		purged := true
		for _, p := range s.purgers {
			if err := p.PurgeCheckin(ctx, id); err != nil {
				slog.Error("geo: external purge failed; will retry", "checkin", id, "err", err)
				purged = false
				break
			}
		}
		if !purged {
			continue
		}
		if _, err := s.db.Exec(ctx, `DELETE FROM hotel_checkins WHERE id = $1`, id); err != nil {
			return res, fmt.Errorf("delete checkin: %w", err)
		}
		res.Purged++
	}

	_, err = s.db.Exec(ctx, `
		UPDATE profiles p
		   SET mode = COALESCE(prev_mode,'incognito'), prev_mode = NULL, public_ref = gen_random_uuid()
		 WHERE p.mode = 'hotel'
		   AND NOT EXISTS (SELECT 1 FROM hotel_checkins c WHERE c.user_id = p.user_id AND c.checked_out_at IS NULL)`)
	return res, err
}

// StartJob runs RunOnce every interval until ctx is cancelled.
func (s *Service) StartJob(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			started := time.Now()
			res, err := s.RunOnce(ctx)
			metrics.Job("geo_retention", time.Since(started), err)
			metrics.JobItems("geo_retention", "purged_stays", res.Purged)
			metrics.JobItems("geo_retention", "auto_checkouts", res.AutoCheckedOut)
			if err != nil {
				slog.Error("geo: retention job", "err", err)
			} else if res.AutoCheckedOut+res.Purged > 0 {
				slog.Info("geo: retention job", "auto_checked_out", res.AutoCheckedOut, "purged", res.Purged)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
