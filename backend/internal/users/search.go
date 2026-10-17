package users

import (
	"context"
	"errors"

	"connectme/internal/entitlements"

	"github.com/jackc/pgx/v5"
)

// ErrNeedsLocation / ErrNeedsVerification are returned by Nearby.
var (
	ErrNeedsLocation     = errors.New("set your location first")
	ErrNeedsVerification = errors.New("verify your email first")
)

// snapDeg is the grid (~1.1 km) both points are snapped to before any
// distance is computed or compared, so neither the displayed bucket nor a
// radius filter can be used to narrow someone's position below ~1 km.
const snapDeg = "0.01"

// snapMarginM bounds the error added by snapping (two points, ~0.8 km each),
// used only to let the spatial index prune before the exact snapped test.
const snapMarginM = 2000

// Nearby returns anonymous-or-full cards for people around the caller.
// Excluded: self, blocked (either way), banned, unverified, already swiped,
// and anyone in hotel mode (they appear only in their hotel's private feed).
func (s *Service) Nearby(ctx context.Context, viewerID string, f NearbyFilter) ([]PublicProfile, error) {
	var verified, hasLoc bool
	err := s.db.QueryRow(ctx, `
		SELECT u.is_verified, p.location IS NOT NULL
		  FROM users u JOIN profiles p ON p.user_id = u.id WHERE u.id = $1`, viewerID).Scan(&verified, &hasLoc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !verified {
		return nil, ErrNeedsVerification
	}
	if !hasLoc {
		return nil, ErrNeedsLocation
	}

	var mode *string
	if f.Mode != "" {
		m := string(f.Mode)
		mode = &m
	}
	var gender *string
	if f.Gender != "" {
		gender = &f.Gender
	}
	rows, err := s.db.Query(ctx, `
		WITH me AS (SELECT location FROM profiles WHERE user_id = $1),
		cand AS (
		  SELECT p.user_id, p.public_ref::text AS ref, p.display_name, p.bio, p.intro, p.mood, p.mode,
		         p.tags, p.photos, p.last_active, COALESCE(p.boosted_until > NOW(), FALSE) AS boosted,
		         date_part('year', age(u.date_of_birth))::int AS age,
		         COALESCE(`+entitlements.ActiveTierSQL("p.user_id")+` = 'platinum', FALSE) AS vip,
		         ST_Distance(ST_SnapToGrid(p.location::geometry, `+snapDeg+`)::geography,
		                     ST_SnapToGrid(me.location::geometry, `+snapDeg+`)::geography) AS dist
		    FROM profiles p
		    JOIN users u ON u.id = p.user_id
		    CROSS JOIN me
		   WHERE p.user_id <> $1 AND u.is_verified AND NOT u.is_banned
		     AND p.location IS NOT NULL AND p.mode <> 'hotel' AND NOT p.invisible
		     AND ST_DWithin(p.location, me.location, $2::float8 + `+itoa(snapMarginM)+`)
		     AND `+notBlocked("p.user_id", "$1")+`
		     AND NOT EXISTS (SELECT 1 FROM swipes sw WHERE sw.actor_id = $1 AND sw.target_id = p.user_id)
		     AND ($3::text IS NULL OR p.mode = $3)
		     AND ($4::text IS NULL OR p.gender = $4)
		     AND (cardinality($5::text[]) = 0 OR p.tags && $5::text[])
		)
		SELECT ref, display_name, bio, intro, mood, mode, tags, photos, age, dist, vip
		  FROM cand
		 WHERE dist <= $2::float8 AND age BETWEEN $6 AND $7
		 ORDER BY boosted DESC, dist, vip DESC, last_active DESC
		 LIMIT $8 OFFSET $9`,
		viewerID, float64(f.RadiusKm)*1000, mode, gender, nonNil(f.Tags), f.AgeMin, f.AgeMax, f.Limit, f.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PublicProfile{}
	for rows.Next() {
		var (
			r                     ProfileRow
			name, bio, intro, mod *string
			photosRaw             []byte
		)
		if err := rows.Scan(&r.Ref, &name, &bio, &intro, &mod, &r.Mode, &r.Tags, &photosRaw, &r.Age, &r.SnappedDistanceM, &r.VIP); err != nil {
			return nil, err
		}
		r.DisplayName, r.Bio, r.Intro, r.Mood = deref(name), deref(bio), deref(intro), deref(mod)
		if r.Photos, err = parsePhotos(photosRaw); err != nil {
			return nil, err
		}
		// Not matched (excluded: swiped), so only the owner's own mode applies.
		out = append(out, Project(r, ResolveVisibility(r.Mode, Relation{})))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	s.attachVoice(ctx, out)
	return out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
