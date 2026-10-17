package users

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// SeesFull reports whether viewerID may see ownerID's full, non-anonymous
// profile (the rule that gates photos and voice clips). Banned or unverified
// owners and blocked pairs never qualify. Features that attach personal media
// to a profile use this so they cannot drift from the photo rule.
func (s *Service) SeesFull(ctx context.Context, viewerID, ownerID string) (bool, error) {
	var mode Mode
	err := s.db.QueryRow(ctx, `
		SELECT p.mode FROM profiles p JOIN users u ON u.id = p.user_id
		 WHERE p.user_id = $1::uuid AND u.is_verified AND NOT u.is_banned
		   AND `+notBlocked("p.user_id", "$2::uuid"), ownerID, viewerID).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	rel, err := s.relation(ctx, viewerID, ownerID)
	if err != nil {
		return false, err
	}
	return ResolveVisibility(mode, rel) == VisFull, nil
}
