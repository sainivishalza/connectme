package users

import (
	"context"

	"connectme/internal/entitlements"
)

// Cards is Card for many owners at once: the same visibility rules and the
// same projection, but a constant number of queries instead of several per
// person. Owners the viewer may not see (blocked, banned, unverified, missing)
// are simply absent from the result. hotelName has the meaning it has for Card.
//
// Feeds that show a card per row (posts, matches, hotel guests) must use this:
// calling Card in a loop was the dominant cost of those endpoints under load.
func (s *Service) Cards(ctx context.Context, viewerID string, ownerIDs []string, hotelName string) (map[string]PublicProfile, error) {
	out := map[string]PublicProfile{}
	if len(ownerIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.user_id::text, p.public_ref::text, p.display_name, p.bio, p.intro, p.mood, p.mode, p.tags, p.photos,
		       date_part('year', age(u.date_of_birth))::int,
		       ST_Distance(ST_SnapToGrid(p.location::geometry, 0.01)::geography,
		                   ST_SnapToGrid(me.location::geometry, 0.01)::geography),
		       COALESCE(`+entitlements.ActiveTierSQL("p.user_id")+` = 'platinum', FALSE)
		  FROM profiles p
		  JOIN users u ON u.id = p.user_id
		  LEFT JOIN profiles me ON me.user_id = $2
		 WHERE p.user_id = ANY($1::uuid[]) AND u.is_verified AND NOT u.is_banned
		   AND `+notBlocked("p.user_id", "$2"), ownerIDs, viewerID)
	if err != nil {
		return nil, err
	}
	type rec struct {
		owner string
		row   ProfileRow
	}
	var recs []rec
	for rows.Next() {
		var (
			r                          rec
			photosRaw                  []byte
			nameP, bioP, introP, moodP *string
			dist                       *float64
		)
		if err := rows.Scan(&r.owner, &r.row.Ref, &nameP, &bioP, &introP, &moodP, &r.row.Mode, &r.row.Tags, &photosRaw, &r.row.Age, &dist, &r.row.VIP); err != nil {
			rows.Close()
			return nil, err
		}
		r.row.DisplayName, r.row.Bio, r.row.Intro, r.row.Mood = deref(nameP), deref(bioP), deref(introP), deref(moodP)
		r.row.SnappedDistanceM = -1
		if dist != nil && hotelName == "" {
			r.row.SnappedDistanceM = *dist
		}
		r.row.HotelName = hotelName
		if r.row.Photos, err = parsePhotos(photosRaw); err != nil {
			rows.Close()
			return nil, err
		}
		recs = append(recs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return out, nil
	}

	// Matches between the viewer and any of these people, with who revealed.
	rels := map[string]Relation{}
	mrows, err := s.db.Query(ctx, `
		SELECT o.other::text, m.mode,
		       EXISTS (SELECT 1 FROM reveal_consents r WHERE r.match_id = m.id AND r.user_id = $1::uuid),
		       EXISTS (SELECT 1 FROM reveal_consents r WHERE r.match_id = m.id AND r.user_id = o.other)
		  FROM matches m
		  CROSS JOIN LATERAL (SELECT CASE WHEN m.user1_id = $1::uuid THEN m.user2_id ELSE m.user1_id END AS other) o
		 WHERE m.status = 'matched'
		   AND ((m.user1_id = $1::uuid AND m.user2_id = ANY($2::uuid[])) OR (m.user2_id = $1::uuid AND m.user1_id = ANY($2::uuid[])))`,
		viewerID, ownerIDs)
	if err != nil {
		return nil, err
	}
	for mrows.Next() {
		var other, mode string
		var viewerRev, ownRev bool
		if err := mrows.Scan(&other, &mode, &viewerRev, &ownRev); err != nil {
			mrows.Close()
			return nil, err
		}
		rels[other] = Relation{Matched: true, MatchMode: Mode(mode), ViewerRevealed: viewerRev, TargetRevealed: ownRev}
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}

	ps := make([]PublicProfile, len(recs))
	for i, r := range recs {
		rel := rels[r.owner]
		if r.owner == viewerID {
			rel = Relation{Self: true}
		}
		ps[i] = Project(r.row, ResolveVisibility(r.row.Mode, rel))
	}
	s.attachVoice(ctx, ps)
	s.attachDetails(ctx, viewerID, ps)
	for i, r := range recs {
		out[r.owner] = ps[i]
	}
	return out, nil
}
