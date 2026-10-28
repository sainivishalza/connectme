package groups

import (
	"context"
	"errors"
	"time"

	"connectme/pkg/middleware"
	"connectme/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// memberOfMatch reports whether the user belongs to either group of an
// active group match.
func (s *Service) memberOfMatch(ctx context.Context, q pgx.Tx, userID, gmID string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM group_matches gm
		                 JOIN group_members m ON m.group_id IN (gm.group_a, gm.group_b)
		                WHERE gm.id=$1::uuid AND gm.status='matched' AND m.user_id=$2)`, gmID, userID).Scan(&ok)
	return ok, err
}

// Schedule proposes a date for a matched pair of groups. It becomes
// "confirmed" once at least one member of EACH group has confirmed.
func (s *Service) Schedule(ctx context.Context, userID, groupID string, req ScheduleRequest) (*DateView, error) {
	venue := middleware.SanitizeText(utils.StripTags(req.Venue), 120)
	if venue == "" {
		return nil, ValidationError{Msg: "venue is required"}
	}
	addr := middleware.SanitizeText(utils.StripTags(req.Address), 200)
	now := time.Now()
	if !req.StartsAt.After(now.Add(30*time.Minute)) || req.StartsAt.After(now.Add(maxDateHorizon)) {
		return nil, ValidationError{Msg: "starts_at must be between 30 minutes and 90 days from now"}
	}
	if _, err := uuid.Parse(req.GroupMatchID); err != nil {
		return nil, ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var ok bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM group_matches gm JOIN group_members m ON m.group_id = $2::uuid AND m.user_id = $3
		                WHERE gm.id = $1::uuid AND gm.status='matched' AND $2::uuid IN (gm.group_a, gm.group_b))`,
		req.GroupMatchID, groupID, userID).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	var open int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM group_dates WHERE group_match_id=$1::uuid AND status='proposed'`, req.GroupMatchID).Scan(&open); err != nil {
		return nil, err
	}
	if open >= maxOpenProposals {
		return nil, ErrLimit
	}
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO group_dates (group_match_id, venue, address, starts_at, proposed_by)
		VALUES ($1::uuid,$2,NULLIF($3,''),$4,$5) RETURNING id::text`,
		req.GroupMatchID, venue, addr, req.StartsAt.UTC(), userID).Scan(&id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO group_date_confirmations (date_id, user_id) VALUES ($1::uuid,$2)`, id, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.date(ctx, userID, id)
}

// ConfirmDate adds the caller's confirmation and confirms the date once both
// groups are represented.
func (s *Service) ConfirmDate(ctx context.Context, userID, dateID string) (*DateView, error) {
	if _, err := uuid.Parse(dateID); err != nil {
		return nil, ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var gm, status string
	err = tx.QueryRow(ctx, `SELECT group_match_id::text, status FROM group_dates WHERE id=$1::uuid FOR UPDATE`, dateID).Scan(&gm, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if ok, err := s.memberOfMatch(ctx, tx, userID, gm); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	if status == "cancelled" {
		return nil, ErrConflict
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO group_date_confirmations (date_id, user_id) VALUES ($1::uuid,$2) ON CONFLICT DO NOTHING`, dateID, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE group_dates d SET status='confirmed'
		 WHERE d.id=$1::uuid AND d.status='proposed'
		   AND (SELECT COUNT(DISTINCT m.group_id)
		          FROM group_date_confirmations c
		          JOIN group_matches gm ON gm.id = d.group_match_id
		          JOIN group_members m ON m.user_id = c.user_id AND m.group_id IN (gm.group_a, gm.group_b)
		         WHERE c.date_id = d.id) = 2`, dateID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.date(ctx, userID, dateID)
}

// CancelDate cancels a proposed or confirmed date (any member of the match).
func (s *Service) CancelDate(ctx context.Context, userID, dateID string) error {
	if _, err := uuid.Parse(dateID); err != nil {
		return ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var gm string
	err = tx.QueryRow(ctx, `SELECT group_match_id::text FROM group_dates WHERE id=$1::uuid`, dateID).Scan(&gm)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if ok, err := s.memberOfMatch(ctx, tx, userID, gm); err != nil || !ok {
		if err != nil {
			return err
		}
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE group_dates SET status='cancelled' WHERE id=$1::uuid`, dateID); err != nil {
		return err
	}
	if err := s.refundLogistics(ctx, tx, dateID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Dates lists the dates of a group match the caller belongs to.
func (s *Service) Dates(ctx context.Context, userID, gmID string) ([]DateView, error) {
	if _, err := uuid.Parse(gmID); err != nil {
		return nil, ErrNotFound
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM group_matches gm JOIN group_members m ON m.group_id IN (gm.group_a, gm.group_b)
		                WHERE gm.id=$1::uuid AND gm.status='matched' AND m.user_id=$2)`, gmID, userID).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(ctx, `
		SELECT d.id::text, d.group_match_id::text, d.venue, COALESCE(d.address,''), d.starts_at, d.status,
		       (SELECT COUNT(*) FROM group_date_confirmations c WHERE c.date_id=d.id),
		       EXISTS (SELECT 1 FROM group_date_confirmations c WHERE c.date_id=d.id AND c.user_id=$2)
		  FROM group_dates d WHERE d.group_match_id=$1::uuid AND d.status <> 'cancelled' ORDER BY d.starts_at`, gmID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DateView{}
	for rows.Next() {
		var v DateView
		if err := rows.Scan(&v.ID, &v.GroupMatchID, &v.Venue, &v.Address, &v.StartsAt, &v.Status, &v.Confirmed, &v.IConfirmed); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Service) date(ctx context.Context, userID, dateID string) (*DateView, error) {
	var v DateView
	err := s.db.QueryRow(ctx, `
		SELECT d.id::text, d.group_match_id::text, d.venue, COALESCE(d.address,''), d.starts_at, d.status,
		       (SELECT COUNT(*) FROM group_date_confirmations c WHERE c.date_id=d.id),
		       EXISTS (SELECT 1 FROM group_date_confirmations c WHERE c.date_id=d.id AND c.user_id=$2)
		  FROM group_dates d WHERE d.id=$1::uuid`, dateID, userID).
		Scan(&v.ID, &v.GroupMatchID, &v.Venue, &v.Address, &v.StartsAt, &v.Status, &v.Confirmed, &v.IConfirmed)
	return &v, err
}
