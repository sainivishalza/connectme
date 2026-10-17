package social

import (
	"context"
	"errors"

	"connectme/internal/users"
	"connectme/pkg/metrics"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateCommunityPost publishes a post in a community the caller belongs to.
// A post in a hotel community is bound to the author's stay and is deleted
// with it.
func (s *Service) CreateCommunityPost(ctx context.Context, userID, communityID, body string) (*Post, error) {
	if _, err := uuid.Parse(communityID); err != nil {
		return nil, ErrNotFound
	}
	body, err := cleanBody(body)
	if err != nil {
		return nil, err
	}
	var okUser bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, userID).Scan(&okUser); err != nil || !okUser {
		return nil, ErrForbidden
	}
	var member bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM community_members WHERE community_id=$1::uuid AND user_id=$2)`,
		communityID, userID).Scan(&member); err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrNotFound
	}
	var today int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM posts WHERE author_id=$1 AND kind='community' AND created_at > NOW() - INTERVAL '24 hours'`,
		userID).Scan(&today); err != nil {
		return nil, err
	}
	if today >= dailyLimit[KindCommunity] {
		return nil, ErrLimit
	}
	if s.vetter != nil {
		if err := s.vetter.VetText(ctx, userID, "post", body); err != nil {
			return nil, err
		}
	}
	var id string
	err = s.db.QueryRow(ctx, `
		INSERT INTO posts (author_id, kind, body, community_id, checkin_id, expires_at)
		SELECT $1, 'community', $2, c.id, m.checkin_id, NOW() + $3::interval
		  FROM communities c JOIN community_members m ON m.community_id = c.id AND m.user_id = $1
		 WHERE c.id = $4::uuid
		RETURNING id::text`, userID, body, lifetime[KindCommunity].String(), communityID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	metrics.Event("post_community")
	return s.one(ctx, userID, id)
}

// CommunityFeed lists a community's posts for a member, newest first.
func (s *Service) CommunityFeed(ctx context.Context, viewerID, communityID, before string, limit int) ([]Post, error) {
	if _, err := uuid.Parse(communityID); err != nil {
		return nil, ErrNotFound
	}
	if before != "" {
		if _, err := uuid.Parse(before); err != nil {
			return nil, users.ValidationError{Msg: "invalid cursor"}
		}
	}
	var member bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM community_members WHERE community_id=$1::uuid AND user_id=$2)`,
		communityID, viewerID).Scan(&member); err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrNotFound
	}
	return s.query(ctx, viewerID, filter{kind: KindCommunity, community: communityID, before: before, limit: limit})
}
