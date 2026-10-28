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
	steps := map[string]VoiceStep{}
	anon := map[string]string{}
	for _, r := range recs {
		if c, ok := cards[r.other]; ok && c.Anonymous {
			anon[r.view.MatchID] = r.other
		}
	}
	timers := map[string]RevealTimer{}
	if len(anon) > 0 {
		if st, err := s.voiceSteps(ctx, userID, anon); err == nil {
			steps = st
		}
		if rt, err := s.revealTimers(ctx, userID, anon); err == nil {
			timers = rt
		}
	}
	out := []MatchView{}
	for _, r := range recs {
		if rt, ok := timers[r.view.MatchID]; ok {
			rt := rt
			r.view.RevealTimer = &rt
		}
		if st, ok := steps[r.view.MatchID]; ok {
			st := st
			r.view.Voice = &st
		}
		card, ok := cards[r.other]
		if !ok {
			continue // blocked, banned or unverified counterpart
		}
		r.view.Profile = &card
		if !card.Anonymous {
			r.view.TheirComment = s.visibleComment(ctx, userID, r.other)
			r.view.MyComment = s.ownComment(ctx, userID, r.other)
		}
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
	// Comments maps a profile ref to the note that person sent with their like
	// (only for people visible in full).
	Comments map[string]string `json:"comments,omitempty"`
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
		if !card.Anonymous {
			if c := s.visibleComment(ctx, userID, id); c != "" {
				if out.Comments == nil {
					out.Comments = map[string]string{}
				}
				out.Comments[card.Ref] = c
			}
		}
	}
	return out, nil
}

// rewindWindow is how long after a pass it can still be undone.
const rewindWindow = 10 * time.Minute

// Rewind undoes the caller's most recent swipe if it was a pass made in the
// last ten minutes, so that person can appear again. It is a Gold perk. Likes
// are not rewound: a like may already have created a match or told the other
// person, and unsending it would make both worse.
func (s *Service) Rewind(ctx context.Context, userID string) error {
	tier, err := entitlements.TierOf(ctx, s.db, userID)
	if err != nil {
		return err
	}
	if !entitlements.PerksFor(tier).Rewind {
		return ErrNeedsGold
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var target, action string
	var at time.Time
	err = tx.QueryRow(ctx, `SELECT target_id::text, action, created_at FROM swipes WHERE actor_id=$1::uuid ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, userID).
		Scan(&target, &action, &at)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (Action(action) != ActionPass || time.Since(at) > rewindWindow)) {
		return users.ValidationError{Msg: "there is no recent pass to undo"}
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM swipes WHERE actor_id=$1::uuid AND target_id=$2::uuid`, userID, target); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ownComment is the note the caller sent with their own like, shown back to
// them in the match so both sides see how the conversation began.
func (s *Service) ownComment(ctx context.Context, me, other string) string {
	var c *string
	if s.db.QueryRow(ctx, `SELECT comment FROM swipes WHERE actor_id=$1::uuid AND target_id=$2::uuid`, me, other).Scan(&c) != nil || c == nil {
		return ""
	}
	return *c
}
