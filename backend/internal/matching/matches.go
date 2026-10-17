package matching

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"connectme/internal/entitlements"
	"connectme/internal/users"
	"connectme/internal/wallet"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Boost spends coins and lifts the caller to the top of nearby results for 30 minutes.
func (s *Service) Boost(ctx context.Context, userID string) (time.Time, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return time.Time{}, err
	}
	tier, err := entitlements.TierOf(ctx, tx, userID)
	if err != nil {
		return time.Time{}, err
	}
	free := entitlements.PerksFor(tier).FreeBoostsPerWeek
	source := "coins"
	if free < 0 {
		source = "perk"
	} else if free > 0 {
		var used int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM boost_log WHERE user_id=$1 AND source='perk' AND created_at >= $2`,
			userID, entitlements.WeekAgo(time.Now())).Scan(&used); err != nil {
			return time.Time{}, err
		}
		if used < free {
			source = "perk"
		}
	}
	if source == "coins" {
		if err := wallet.Spend(ctx, tx, userID, BoostCoins, "boost", ""); err != nil {
			if errors.Is(err, wallet.ErrInsufficient) {
				return time.Time{}, ErrInsufficient
			}
			return time.Time{}, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO boost_log (user_id, source) VALUES ($1,$2)`, userID, source); err != nil {
		return time.Time{}, err
	}
	var until time.Time
	err = tx.QueryRow(ctx, `
		UPDATE profiles SET boosted_until = GREATEST(COALESCE(boosted_until, NOW()), NOW()) + $2::interval
		 WHERE user_id=$1 RETURNING boosted_until`, userID, BoostDuration.String()).Scan(&until)
	if err != nil {
		return time.Time{}, err
	}
	return until, tx.Commit(ctx)
}

// Matches lists active matches, newest first, each with the counterpart as
// the viewer is allowed to see them.
func (s *Service) Matches(ctx context.Context, userID string, limit, offset int) ([]MatchView, error) {
	rows, err := s.db.Query(ctx, `
		SELECT m.id::text, m.mode, m.created_at,
		       CASE WHEN m.user1_id=$1 THEN m.user2_id ELSE m.user1_id END::text,
		       EXISTS (SELECT 1 FROM reveal_consents r WHERE r.match_id=m.id AND r.user_id=$1),
		       m.compat_score, m.compat_reasons,
		       m.origin, m.expires_at, $1::uuid = ANY(m.kept_by)
		  FROM matches m
		 WHERE (m.user1_id=$1 OR m.user2_id=$1) AND m.status='matched'
		   AND (m.expires_at IS NULL OR m.expires_at > NOW())
		 ORDER BY m.created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	type rec struct {
		view  MatchView
		other string
	}
	var recs []rec
	for rows.Next() {
		var r rec
		var mode string
		var reasons []byte
		var origin string
		var expires *time.Time
		var keptMe bool
		if err := rows.Scan(&r.view.MatchID, &mode, &r.view.CreatedAt, &r.other, &r.view.RevealedByMe,
			&r.view.CompatScore, &reasons, &origin, &expires, &keptMe); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal(reasons, &r.view.CompatReasons)
		if origin == "random" && expires != nil {
			r.view.Random = &RandomInfo{ExpiresAt: expires, KeptByMe: keptMe}
		}
		r.view.Mode = users.Mode(mode)
		recs = append(recs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	others := make([]string, len(recs))
	for i, r := range recs {
		others[i] = r.other
	}
	cards, err := s.users.Cards(ctx, userID, others, "")
	if err != nil {
		return nil, err
	}
	out := []MatchView{}
	for _, r := range recs {
		card, ok := cards[r.other]
		if !ok {
			continue // blocked, banned or unverified counterpart
		}
		r.view.Profile = &card
		out = append(out, r.view)
	}
	return out, nil
}

// Unmatch ends a match. Swipes are kept so the pair is never shown again.
func (s *Service) Unmatch(ctx context.Context, userID, matchID string) error {
	if _, err := uuid.Parse(matchID); err != nil {
		return ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `
		UPDATE matches SET status='rejected'
		 WHERE id=$1 AND (user1_id=$2 OR user2_id=$2) AND status='matched'`, matchID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM reveal_consents WHERE match_id=$1`, matchID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Reveal records the caller's consent to unmask. Photos and name unlock only
// when BOTH participants have consented; the response says only whether that
// has happened, not whether the other side is waiting.
func (s *Service) Reveal(ctx context.Context, userID, matchID string) (*RevealResult, error) {
	if _, err := uuid.Parse(matchID); err != nil {
		return nil, ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var mode string
	err = tx.QueryRow(ctx, `
		SELECT mode FROM matches WHERE id=$1 AND (user1_id=$2 OR user2_id=$2) AND status='matched'`,
		matchID, userID).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO reveal_consents (match_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		matchID, userID); err != nil {
		return nil, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM reveal_consents WHERE match_id=$1`, matchID).Scan(&n); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &RevealResult{Mutual: n >= 2 || mode == string(users.ModeOpen)}, nil
}

// LikesYou is the "see who liked you" perk: everyone sees how many people
// liked them; Gold and above also get the cards (still subject to each
// person's own visibility, so anonymous likers stay anonymous).
type LikesYou struct {
	Count    int                   `json:"count"`
	Locked   bool                  `json:"locked"`
	Profiles []users.PublicProfile `json:"profiles,omitempty"`
}

func (s *Service) LikesYou(ctx context.Context, userID string, limit int) (*LikesYou, error) {
	tier, err := entitlements.TierOf(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	canSee := entitlements.PerksFor(tier).SeeWhoLikedYou
	rows, err := s.db.Query(ctx, `
		SELECT sw.actor_id::text
		  FROM swipes sw JOIN users u ON u.id = sw.actor_id
		 WHERE sw.target_id = $1 AND sw.action IN ('like','superlike')
		   AND u.is_verified AND NOT u.is_banned
		   AND NOT EXISTS (SELECT 1 FROM swipes mine WHERE mine.actor_id = $1 AND mine.target_id = sw.actor_id)
		   AND `+users.NotBlockedSQL("sw.actor_id", "$1::uuid")+`
		 ORDER BY (sw.action = 'superlike') DESC, sw.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := &LikesYou{Count: len(ids), Locked: !canSee}
	if !canSee {
		return out, nil
	}
	for i, id := range ids {
		if i >= limit {
			break
		}
		card, err := s.users.Card(ctx, userID, id, "")
		if errors.Is(err, users.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out.Profiles = append(out.Profiles, *card)
	}
	return out, nil
}
