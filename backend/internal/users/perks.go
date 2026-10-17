package users

import (
	"context"

	"connectme/internal/entitlements"
)

// ErrNeedsTier is returned when a perk is used without the required tier.
type ErrNeedsTier struct{ Tier entitlements.Tier }

func (e ErrNeedsTier) Error() string {
	return "requires a " + string(e.Tier) + " subscription or higher"
}

// SetInvisible hides the caller from nearby and hotel discovery (existing
// matches are unaffected). Turning it off is always allowed; turning it on
// requires the Gold perk, and it switches itself off if the perk lapses.
func (s *Service) SetInvisible(ctx context.Context, userID string, enabled bool) (*OwnProfile, error) {
	if enabled {
		tier, err := entitlements.TierOf(ctx, s.db, userID)
		if err != nil {
			return nil, err
		}
		if !entitlements.PerksFor(tier).Invisibility {
			return nil, ErrNeedsTier{Tier: entitlements.Gold}
		}
	}
	if _, err := s.db.Exec(ctx, `UPDATE profiles SET invisible=$1 WHERE user_id=$2`, enabled, userID); err != nil {
		return nil, err
	}
	return s.GetMe(ctx, userID)
}
