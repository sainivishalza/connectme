package groups

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"connectme/internal/users"
	"connectme/internal/wallet"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LogisticsCoins is the per-person price of the coordinated plan for one
// date: 100 coins, about $1.80 at the standard coin packs (the PRD range is
// $1-5 per person).
const LogisticsCoins = 100

// ErrInsufficient means the caller cannot afford the fee.
var ErrInsufficient = wallet.ErrInsufficient

// Logistics is what a member sees for a date: the price, whether they have
// paid and, only if they have, the plan.
type Logistics struct {
	PriceCoins int          `json:"price_coins"`
	Paid       bool         `json:"paid"`
	Plan       *Suggestions `json:"plan,omitempty"`
}

// dateAccess loads a date that has not been cancelled or started yet and
// proves the caller belongs to either group of its match.
func (s *Service) dateAccess(ctx context.Context, userID, dateID string) (startsAt time.Time, groupMatch string, err error) {
	if _, perr := uuid.Parse(dateID); perr != nil {
		return time.Time{}, "", ErrNotFound
	}
	var status string
	err = s.db.QueryRow(ctx, `
		SELECT d.starts_at, d.group_match_id::text, d.status
		  FROM group_dates d
		  JOIN group_matches gm ON gm.id = d.group_match_id AND gm.status = 'matched'
		  JOIN group_members m ON m.group_id IN (gm.group_a, gm.group_b)
		 WHERE d.id = $1::uuid AND m.user_id = $2::uuid LIMIT 1`, dateID, userID).Scan(&startsAt, &groupMatch, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, "", ErrNotFound
	}
	if err != nil {
		return time.Time{}, "", err
	}
	if status == "cancelled" {
		return time.Time{}, "", ErrNotFound
	}
	return startsAt, groupMatch, nil
}

// LogisticsFor returns the price and, for people who paid, the plan.
func (s *Service) LogisticsFor(ctx context.Context, userID, dateID string) (*Logistics, error) {
	if _, _, err := s.dateAccess(ctx, userID, dateID); err != nil {
		return nil, err
	}
	out := &Logistics{PriceCoins: LogisticsCoins}
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM group_date_logistics WHERE date_id=$1::uuid AND user_id=$2::uuid)`, dateID, userID).Scan(&out.Paid); err != nil {
		return nil, err
	}
	if out.Paid {
		plan, err := s.storedPlan(ctx, dateID)
		if err != nil {
			return nil, err
		}
		out.Plan = plan
	}
	return out, nil
}

func (s *Service) storedPlan(ctx context.Context, dateID string) (*Suggestions, error) {
	var raw []byte
	err := s.db.QueryRow(ctx, `SELECT plan FROM group_date_plans WHERE date_id=$1::uuid`, dateID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p Suggestions
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// PayLogistics charges the caller once for the date's plan (idempotent: a
// second call returns the plan without charging) and returns it. The plan is
// generated before the charge, so a failure to plan never costs anything, and
// is stored once per date so every payer sees the same one.
func (s *Service) PayLogistics(ctx context.Context, userID, dateID string) (*Logistics, error) {
	startsAt, gm, err := s.dateAccess(ctx, userID, dateID)
	if err != nil {
		return nil, err
	}
	if !startsAt.After(time.Now()) {
		return nil, users.ValidationError{Msg: "this date has already started"}
	}
	var paid bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM group_date_logistics WHERE date_id=$1::uuid AND user_id=$2::uuid)`, dateID, userID).Scan(&paid); err != nil {
		return nil, err
	}
	if !paid {
		if err := s.ensurePlan(ctx, userID, dateID, gm); err != nil {
			return nil, err
		}
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		tag, err := tx.Exec(ctx, `
			INSERT INTO group_date_logistics (date_id, user_id, coins) VALUES ($1::uuid,$2::uuid,$3) ON CONFLICT DO NOTHING`,
			dateID, userID, LogisticsCoins)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 1 {
			if err := wallet.Spend(ctx, tx, userID, LogisticsCoins, "group_logistics", "logistics:"+dateID+":"+userID); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
	}
	return s.LogisticsFor(ctx, userID, dateID)
}

// ensurePlan generates and stores the date's plan the first time anyone pays.
func (s *Service) ensurePlan(ctx context.Context, userID, dateID, gm string) error {
	if p, err := s.storedPlan(ctx, dateID); err != nil || p != nil {
		return err
	}
	var in SuggestInput
	err := s.db.QueryRow(ctx, `
		SELECT g.activity,
		       (SELECT COUNT(*) FROM group_members m WHERE m.group_id IN (gm.group_a, gm.group_b)),
		       COALESCE((SELECT array_agg(t) FROM (
		           SELECT t FROM group_members m JOIN profiles p ON p.user_id = m.user_id, unnest(p.tags) t
		            WHERE m.group_id IN (gm.group_a, gm.group_b) GROUP BY t ORDER BY COUNT(*) DESC, t LIMIT 5) x), '{}')
		  FROM group_matches gm JOIN groups g ON g.id = gm.group_a WHERE gm.id = $1::uuid`, gm).Scan(&in.Activity, &in.GroupSize, &in.SharedTags)
	if err != nil {
		return err
	}
	out, err := s.suggester.Suggest(ctx, in)
	if err != nil {
		out, _ = FallbackSuggester{}.Suggest(ctx, in)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO group_date_plans (date_id, plan) VALUES ($1::uuid,$2) ON CONFLICT DO NOTHING`, dateID, raw)
	return err
}

// refundLogistics gives everyone who paid for a date's plan their coins back
// (used when the date is cancelled). Idempotent per payer.
func (s *Service) refundLogistics(ctx context.Context, tx pgx.Tx, dateID string) error {
	rows, err := tx.Query(ctx, `SELECT user_id::text, coins FROM group_date_logistics WHERE date_id=$1::uuid`, dateID)
	if err != nil {
		return err
	}
	type pay struct {
		user  string
		coins int
	}
	var pays []pay
	for rows.Next() {
		var p pay
		if err := rows.Scan(&p.user, &p.coins); err != nil {
			rows.Close()
			return err
		}
		pays = append(pays, p)
	}
	rows.Close()
	for _, p := range pays {
		if _, err := wallet.Grant(ctx, tx, p.user, p.coins, "group_refund", "logistics-refund:"+dateID+":"+p.user); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM group_date_logistics WHERE date_id=$1::uuid`, dateID)
	return err
}
