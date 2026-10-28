// Package follows lets members follow people whose profiles are shown in full.
//
// A follow is a one-way, low-stakes bookmark: it gives the follower "is live"
// alerts and a Following filter, and gives the person followed a public
// follower count. The list of who follows you is private to you. Anonymous
// (Incognito and Hotel) members can be neither followed nor listed, and a
// block in either direction removes the follow.
package follows

import (
	"context"
	"errors"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
	ErrSelf     = errors.New("you cannot follow yourself")
	ErrLimit    = users.ValidationError{Msg: "you are following the maximum number of people; unfollow someone first"}
)

// MaxFollowing caps how many people one member can follow.
const MaxFollowing = 500

type Service struct {
	db    *pgxpool.Pool
	cards func(ctx context.Context, viewerID string, ids []string) (map[string]users.PublicProfile, error)
}

func NewService(db *pgxpool.Pool, u *users.Service) *Service {
	return &Service{db: db, cards: func(ctx context.Context, v string, ids []string) (map[string]users.PublicProfile, error) {
		return u.Cards(ctx, v, ids, "")
	}}
}

// target resolves a public ref to a user the caller may follow: shown in full
// (Open mode, not invisible), verified, not banned, no block either way.
func (s *Service) target(ctx context.Context, userID, ref string) (string, error) {
	if _, err := uuid.Parse(ref); err != nil {
		return "", ErrNotFound
	}
	var id string
	err := s.db.QueryRow(ctx, `
		SELECT p.user_id::text FROM profiles p JOIN users u ON u.id = p.user_id
		 WHERE p.public_ref = $1::uuid AND p.mode = 'open' AND NOT p.invisible AND u.is_verified AND NOT u.is_banned
		   AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker_id = $2::uuid AND b.blocked_id = p.user_id)
		                                            OR (b.blocker_id = p.user_id AND b.blocked_id = $2::uuid))`, ref, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if id == userID {
		return "", ErrSelf
	}
	return id, nil
}

// Follow is idempotent.
func (s *Service) Follow(ctx context.Context, userID, ref string) error {
	id, err := s.target(ctx, userID, ref)
	if err != nil {
		return err
	}
	var n int
	var already bool
	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*)::int, COALESCE(BOOL_OR(followee_id = $2::uuid), FALSE)
		  FROM follows WHERE follower_id = $1::uuid`, userID, id).Scan(&n, &already); err != nil {
		return err
	}
	if n >= MaxFollowing && !already {
		return ErrLimit
	}
	_, err = s.db.Exec(ctx, `INSERT INTO follows (follower_id, followee_id) VALUES ($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, userID, id)
	return err
}

// Unfollow is idempotent and works even if the person is no longer visible.
func (s *Service) Unfollow(ctx context.Context, userID, ref string) error {
	if _, err := uuid.Parse(ref); err != nil {
		return ErrNotFound
	}
	_, err := s.db.Exec(ctx, `
		DELETE FROM follows WHERE follower_id = $1::uuid
		   AND followee_id = (SELECT user_id FROM profiles WHERE public_ref = $2::uuid)`, userID, ref)
	return err
}

// List returns who the caller follows, or who follows the caller, newest first.
// Only people still shown in full are listed; everyone else is skipped, so a
// member who went Incognito simply drops off the list.
func (s *Service) List(ctx context.Context, userID, direction string) ([]users.PublicProfile, error) {
	var q string
	switch direction {
	case "following":
		q = `SELECT f.followee_id::text FROM follows f JOIN profiles p ON p.user_id = f.followee_id
		      WHERE f.follower_id = $1::uuid AND p.mode='open' AND NOT p.invisible ORDER BY f.created_at DESC LIMIT 200`
	case "followers":
		q = `SELECT f.follower_id::text FROM follows f JOIN profiles p ON p.user_id = f.follower_id
		      WHERE f.followee_id = $1::uuid AND p.mode='open' AND NOT p.invisible ORDER BY f.created_at DESC LIMIT 200`
	default:
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(ctx, q, userID)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cards, err := s.cards(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]users.PublicProfile, 0, len(ids))
	for _, id := range ids {
		if c, ok := cards[id]; ok && !c.Anonymous {
			out = append(out, c)
		}
	}
	return out, nil
}

// FollowerIDs returns who to alert when userID goes live (verified followers
// with no block between them and the host).
func (s *Service) FollowerIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `
		SELECT f.follower_id::text FROM follows f
		  JOIN users u ON u.id = f.follower_id AND u.is_verified AND NOT u.is_banned
		 WHERE f.followee_id = $1::uuid
		   AND NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.blocker_id = f.follower_id AND b.blocked_id = f.followee_id)
		                                            OR (b.blocker_id = f.followee_id AND b.blocked_id = f.follower_id))
		 LIMIT 5000`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
