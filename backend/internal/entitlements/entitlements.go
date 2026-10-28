// Package entitlements maps a user's subscription to what they may do. It is
// shared by payments (which grants tiers) and matching/users (which enforce
// perks) so there is exactly one definition of each tier.
package entitlements

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Tier string

const (
	None     Tier = ""
	Silver   Tier = "silver"
	Gold     Tier = "gold"
	Platinum Tier = "platinum"
)

// FreeLikesPerDay caps likes for people without a subscription (silver and
// above are unlimited). Passes are never capped.
const FreeLikesPerDay = 50

func (t Tier) Valid() bool { return t == Silver || t == Gold || t == Platinum }

var rank = map[Tier]int{None: 0, Silver: 1, Gold: 2, Platinum: 3}

// AtLeast reports whether t includes everything min does (tiers are cumulative).
func (t Tier) AtLeast(min Tier) bool { return rank[t] >= rank[min] }

// Perks are the concrete allowances of a tier.
type Perks struct {
	UnlimitedLikes    bool `json:"unlimited_likes"`
	NoAds             bool `json:"no_ads"`
	SuperLikesPerDay  int  `json:"super_likes_per_day"`
	FreeBoostsPerWeek int  `json:"free_boosts_per_week"` // -1 = unlimited
	SeeWhoLikedYou    bool `json:"see_who_liked_you"`
	Invisibility      bool `json:"invisibility"`
	PriorityMatching  bool `json:"priority_matching"`
	VIPBadge          bool `json:"vip_badge"`
	Rewind            bool `json:"rewind"` // undo the most recent pass
}

// PerksFor returns the cumulative perks of a tier.
func PerksFor(t Tier) Perks {
	p := Perks{}
	if t.AtLeast(Silver) {
		p.UnlimitedLikes, p.NoAds, p.SuperLikesPerDay = true, true, 5
	}
	if t.AtLeast(Gold) {
		p.SeeWhoLikedYou, p.Invisibility, p.FreeBoostsPerWeek, p.Rewind = true, true, 1, true
	}
	if t.AtLeast(Platinum) {
		p.PriorityMatching, p.VIPBadge, p.FreeBoostsPerWeek = true, true, -1
	}
	return p
}

// Querier is satisfied by *pgxpool.Pool and pgx.Tx.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ActiveTierSQL is a SQL expression (for a user-id column expression) that
// yields the highest currently-valid tier, or NULL. Single definition of
// "active": status active/trialing and not past its period end.
func ActiveTierSQL(userIDExpr string) string {
	return `(SELECT s.tier FROM subscriptions s
	          WHERE s.user_id = ` + userIDExpr + ` AND s.status IN ('active','trialing')
	            AND (s.expires_at IS NULL OR s.expires_at > NOW())
	          ORDER BY CASE s.tier WHEN 'platinum' THEN 3 WHEN 'gold' THEN 2 ELSE 1 END DESC LIMIT 1)`
}

// TierOf returns the user's current tier (None when not subscribed).
func TierOf(ctx context.Context, q Querier, userID string) (Tier, error) {
	var t *string
	err := q.QueryRow(ctx, `SELECT `+ActiveTierSQL("$1::uuid"), userID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) || t == nil {
		return None, nil
	}
	if err != nil {
		return None, err
	}
	return Tier(*t), nil
}

// StartOfDayUTC / WeekAgo are the windows used for daily and weekly perks.
func StartOfDayUTC(now time.Time) time.Time {
	n := now.UTC()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func WeekAgo(now time.Time) time.Time { return now.Add(-7 * 24 * time.Hour) }
