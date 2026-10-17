package groups

import (
	"context"
	"errors"

	"connectme/internal/users"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"
)

const snapDeg = "0.01" // same ~1.1 km grid as user search: never finer than this

// candidateSQL selects groups that `me` ($1) may see and match. One predicate
// serves both browsing and consent so they can never disagree:
//   - same type, open, full (complete groups only), not mine, nobody in common;
//   - within 100 km (the largest browse radius), so a group id from outside
//     the browsable area cannot be matched directly;
//   - no block in either direction between ANY pair of members;
//   - not already matched with me.
const candidateSQL = `
  FROM groups me
  JOIN groups g ON g.id <> me.id AND g.type = me.type AND g.status = 'open'
 WHERE me.id = $1::uuid
   AND g.location IS NOT NULL AND me.location IS NOT NULL
   AND ST_DWithin(g.location, me.location, 102000)
   AND (SELECT COUNT(*) FROM group_members x WHERE x.group_id = g.id) = g.target_size
   AND NOT EXISTS (SELECT 1 FROM group_members a JOIN group_members b ON a.user_id = b.user_id
                    WHERE a.group_id = me.id AND b.group_id = g.id)
   AND NOT EXISTS (SELECT 1 FROM group_members a JOIN group_members b ON TRUE
                    JOIN blocks bl ON (bl.blocker_id = a.user_id AND bl.blocked_id = b.user_id)
                                   OR (bl.blocker_id = b.user_id AND bl.blocked_id = a.user_id)
                    WHERE a.group_id = me.id AND b.group_id = g.id)
   AND NOT EXISTS (SELECT 1 FROM group_members m JOIN users u ON u.id = m.user_id
                    WHERE m.group_id = g.id AND (u.is_banned OR NOT u.is_verified))
   AND NOT EXISTS (SELECT 1 FROM group_matches gm
                    WHERE gm.status = 'matched' AND gm.group_a = LEAST(me.id, g.id) AND gm.group_b = GREATEST(me.id, g.id))`

// Browse lists groups the caller's group could match with.
func (s *Service) Browse(ctx context.Context, userID string, f BrowseFilter) ([]Group, error) {
	ok, err := isMember(ctx, s.db, userID, f.ForGroup)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	var ready bool
	if err := s.db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM group_members WHERE group_id=g.id) = g.target_size
		  FROM groups g WHERE g.id=$1::uuid`, f.ForGroup).Scan(&ready); err != nil {
		return nil, err
	}
	if !ready {
		return nil, ErrNotReady
	}
	var activity *string
	if f.Activity != "" {
		activity = &f.Activity
	}
	rows, err := s.db.Query(ctx, `
		SELECT g.id::text,
		       ST_Distance(ST_SnapToGrid(g.location::geometry, `+snapDeg+`)::geography,
		                   ST_SnapToGrid(me.location::geometry, `+snapDeg+`)::geography) AS dist `+candidateSQL+`
		   AND ST_DWithin(g.location, me.location, $2::float8 + 2000)
		   AND ST_Distance(ST_SnapToGrid(g.location::geometry, `+snapDeg+`)::geography,
		                   ST_SnapToGrid(me.location::geometry, `+snapDeg+`)::geography) <= $2::float8
		   AND ($3::text IS NULL OR g.activity = $3)
		   AND g.target_size BETWEEN $4 AND $5
		 ORDER BY dist, g.created_at DESC LIMIT $6 OFFSET $7`,
		f.ForGroup, float64(f.RadiusKm)*1000, activity, f.MinSize, f.MaxSize, f.Limit, f.Offset)
	if err != nil {
		return nil, err
	}
	type hit struct {
		id   string
		dist float64
	}
	var hits []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.dist); err != nil {
			rows.Close()
			return nil, err
		}
		hits = append(hits, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Group{}
	for _, h := range hits {
		g, err := s.load(ctx, h.id)
		if err != nil {
			return nil, err
		}
		if g, err = s.withMembers(ctx, userID, g); err != nil {
			return nil, err
		}
		g.Distance = users.DistanceLabel(h.dist)
		out = append(out, *g)
	}
	return out, nil
}

// Match records the caller's consent to match with another group. A group
// "likes" another only when EVERY current member has consented; the match
// forms when both groups do. Either group changing members voids stale
// consent automatically, because the count is taken over current members.
func (s *Service) Match(ctx context.Context, userID, groupID, targetID string) (*MatchResult, error) {
	if _, err := uuid.Parse(targetID); err != nil {
		return nil, ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := s.requireVerified(ctx, tx, userID); err != nil {
		return nil, err
	}
	if ok, err := isMember(ctx, tx, userID, groupID); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	a, b := groupID, targetID
	if b < a {
		a, b = b, a
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, a+"|"+b); err != nil {
		return nil, err
	}
	var members int
	var full bool
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) = MAX(g.target_size)
		  FROM group_members m JOIN groups g ON g.id = m.group_id WHERE m.group_id = $1::uuid`, groupID).Scan(&members, &full)
	if err != nil {
		return nil, err
	}
	if !full {
		return nil, ErrNotReady
	}
	var eligible bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 `+candidateSQL+` AND g.id = $2::uuid)`, groupID, targetID).Scan(&eligible); err != nil {
		return nil, err
	}
	if !eligible {
		return nil, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO group_match_consents (group_id, target_group_id, user_id) VALUES ($1::uuid,$2::uuid,$3)
		ON CONFLICT DO NOTHING`, groupID, targetID, userID); err != nil {
		return nil, err
	}
	likes := func(from, to string) (bool, error) {
		var all bool
		err := tx.QueryRow(ctx, `
			SELECT COUNT(*) > 0 AND COUNT(*) = COUNT(c.user_id)
			  FROM group_members m
			  LEFT JOIN group_match_consents c
			    ON c.group_id = m.group_id AND c.user_id = m.user_id AND c.target_group_id = $2::uuid
			 WHERE m.group_id = $1::uuid`, from, to).Scan(&all)
		return all, err
	}
	mine, err := likes(groupID, targetID)
	if err != nil {
		return nil, err
	}
	theirs, err := likes(targetID, groupID)
	if err != nil {
		return nil, err
	}
	if !(mine && theirs) {
		return &MatchResult{}, tx.Commit(ctx)
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO group_matches (group_a, group_b) VALUES ($1::uuid,$2::uuid)
		ON CONFLICT (group_a, group_b) DO UPDATE SET status='matched', created_at=NOW()
		  WHERE group_matches.status = 'ended'
		RETURNING id::text`, a, b).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) { // already matched
		err = tx.QueryRow(ctx,
			`SELECT id::text FROM group_matches WHERE group_a=$1::uuid AND group_b=$2::uuid`, a, b).Scan(&id)
	}
	if err != nil {
		return nil, err
	}
	return &MatchResult{Matched: true, GroupMatchID: id}, tx.Commit(ctx)
}

// Matches lists the caller's active group matches.
func (s *Service) Matches(ctx context.Context, userID string) ([]GroupMatch, error) {
	rows, err := s.db.Query(ctx, `
		SELECT gm.id::text, mine.group_id::text,
		       CASE WHEN gm.group_a = mine.group_id THEN gm.group_b ELSE gm.group_a END::text, gm.created_at
		  FROM group_matches gm
		  JOIN group_members mine ON mine.user_id = $1 AND mine.group_id IN (gm.group_a, gm.group_b)
		 WHERE gm.status = 'matched' ORDER BY gm.created_at DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	var recs []GroupMatch
	var others []string
	for rows.Next() {
		var m GroupMatch
		var other string
		if err := rows.Scan(&m.ID, &m.MyGroupID, &other, &m.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		recs = append(recs, m)
		others = append(others, other)
	}
	rows.Close()
	out := []GroupMatch{}
	for i, m := range recs {
		g, err := s.load(ctx, others[i])
		if err != nil {
			return nil, err
		}
		if g, err = s.withMembers(ctx, userID, g); err != nil {
			return nil, err
		}
		m.Other = *g
		out = append(out, m)
	}
	return out, nil
}
