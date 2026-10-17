package users

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// relation loads the match/reveal state between a viewer and a profile owner.
func (s *Service) relation(ctx context.Context, viewerID, ownerID string) (Relation, error) {
	if viewerID == ownerID {
		return Relation{Self: true}, nil
	}
	u1, u2 := viewerID, ownerID
	if u2 < u1 {
		u1, u2 = u2, u1
	}
	var (
		mode              string
		viewerRev, ownRev bool
	)
	err := s.db.QueryRow(ctx, `
		SELECT m.mode,
		       EXISTS (SELECT 1 FROM reveal_consents r WHERE r.match_id=m.id AND r.user_id=$3),
		       EXISTS (SELECT 1 FROM reveal_consents r WHERE r.match_id=m.id AND r.user_id=$4)
		  FROM matches m
		 WHERE m.user1_id=$1 AND m.user2_id=$2 AND m.status='matched'`,
		u1, u2, viewerID, ownerID).Scan(&mode, &viewerRev, &ownRev)
	if errors.Is(err, pgx.ErrNoRows) {
		return Relation{}, nil
	}
	if err != nil {
		return Relation{}, err
	}
	return Relation{Matched: true, MatchMode: Mode(mode), ViewerRevealed: viewerRev, TargetRevealed: ownRev}, nil
}

// notBlocked is a SQL predicate: no block in either direction between two user-id expressions.
// It is two separate NOT EXISTS (not one with OR) so each can use the
// (blocker_id, blocked_id) primary key; the single-OR form made Postgres scan
// the whole blocks table once per candidate row in feeds.
func notBlocked(a, b string) string {
	return `(NOT EXISTS (SELECT 1 FROM blocks bl WHERE bl.blocker_id = ` + a + ` AND bl.blocked_id = ` + b + `)
	     AND NOT EXISTS (SELECT 1 FROM blocks bl WHERE bl.blocker_id = ` + b + ` AND bl.blocked_id = ` + a + `))`
}

func parsePhotos(raw []byte) ([]PhotoMeta, error) {
	out := []PhotoMeta{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func itoa(n int) string { return strconv.Itoa(n) }

// NotBlockedSQL exposes the block predicate to other modules' queries.
func NotBlockedSQL(a, b string) string { return notBlocked(a, b) }

// ResolveRef maps a public ref to its owner's user id with no policy applied
// (used by moderation, which must act on blocked/banned accounts too).
func (s *Service) ResolveRef(ctx context.Context, ref string) (string, error) {
	if _, err := uuid.Parse(ref); err != nil {
		return "", ErrNotFound
	}
	var id string
	err := s.db.QueryRow(ctx, `SELECT user_id::text FROM profiles WHERE public_ref=$1::uuid`, ref).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// ResolveVisibleRef is ResolveRef for interactions (swipe, drink): the owner
// must be verified, not banned and not blocked in either direction.
func (s *Service) ResolveVisibleRef(ctx context.Context, viewerID, ref string) (string, error) {
	if _, err := uuid.Parse(ref); err != nil {
		return "", ErrNotFound
	}
	var id string
	err := s.db.QueryRow(ctx, `
		SELECT p.user_id::text FROM profiles p JOIN users u ON u.id = p.user_id
		 WHERE p.public_ref = $1::uuid AND u.is_verified AND NOT u.is_banned
		   AND `+notBlocked("p.user_id", "$2::uuid"), ref, viewerID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}
