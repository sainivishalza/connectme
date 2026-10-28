package users

import (
	"context"
	"errors"

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
	// People are ordered by boost, then snapped distance. With nobody boosted
	// in range the first page always comes from the nearest ring, so search
	// small circles first and widen only if they hold too few people: in a
	// dense city that skips scoring thousands of far-away candidates.
	radii := []float64{float64(f.RadiusKm) * 1000}
	if boosted, err := s.anyBoostedInRange(ctx, viewerID, radii[0]); err == nil && !boosted {
		radii = ringLadder(f.RadiusKm)
	}
	for i, r := range radii {
		out, err := s.nearbyIn(ctx, viewerID, f, r, mode, gender)
		if err != nil {
			return nil, err
		}
		if i == len(radii)-1 || len(out) >= f.Limit {
			s.attachVoice(ctx, out)
			s.attachDetails(ctx, viewerID, out)
			return out, nil
		}
	}
	return []PublicProfile{}, nil
}

// ringLadder lists the search radii in metres, smallest first, ending at the
// requested one.
func ringLadder(maxKm int) []float64 {
	var out []float64
	for _, km := range []int{2, 5, 10, 25, 50} {
		if km < maxKm {
			out = append(out, float64(km)*1000)
		}
	}
	return append(out, float64(maxKm)*1000)
}

// anyBoostedInRange reports whether any boosted profile could rank ahead of
// the nearest people (boosts sort first whatever the distance).
func (s *Service) anyBoostedInRange(ctx context.Context, viewerID string, radiusM float64) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM profiles p, (SELECT location FROM profiles WHERE user_id = $1) me
		   WHERE p.boosted_until > NOW() AND p.user_id <> $1 AND p.location IS NOT NULL
		     AND ST_DWithin(p.location, me.location, $2::float8 + `+itoa(snapMarginM)+`))`, viewerID, radiusM).Scan(&ok)
	return ok, err
}

// nearbyIn returns one page of cards within radiusM metres of the viewer.
func (s *Service) nearbyIn(ctx context.Context, viewerID string, f NearbyFilter, radiusM float64, mode, gender *string) ([]PublicProfile, error) {
	// Everything per-candidate is a set-based join, and the wide columns (bio,
	// photos, tags) are fetched only for the page that is returned: in a dense
	// area the old correlated sub-queries cost about a second per request
	// (measured with 5,000 people in range).
	rows, err := s.db.Query(ctx, `
		WITH me AS (SELECT location FROM profiles WHERE user_id = $1),
		excl AS (
		  SELECT blocked_id AS id FROM blocks WHERE blocker_id = $1
		  UNION SELECT blocker_id FROM blocks WHERE blocked_id = $1
		  UNION SELECT target_id FROM swipes WHERE actor_id = $1),
		vips AS (
		  SELECT DISTINCT user_id FROM subscriptions
		   WHERE tier = 'platinum' AND status IN ('active','trialing') AND (expires_at IS NULL OR expires_at > NOW())),
		cand AS (
		  SELECT p.user_id, COALESCE(p.boosted_until > NOW(), FALSE) AS boosted, p.last_active,
		         date_part('year', age(u.date_of_birth))::int AS age,
		         (v.user_id IS NOT NULL) AS vip,
		         ST_Distance(ST_SnapToGrid(p.location::geometry, `+snapDeg+`)::geography,
		                     ST_SnapToGrid(me.location::geometry, `+snapDeg+`)::geography) AS dist
		    FROM profiles p
		    JOIN users u ON u.id = p.user_id
		    CROSS JOIN me
		    LEFT JOIN vips v ON v.user_id = p.user_id
		    LEFT JOIN profile_details d ON d.user_id = p.user_id
		   WHERE p.user_id <> $1 AND u.is_verified AND NOT u.is_banned
		     AND p.location IS NOT NULL AND p.mode <> 'hotel' AND NOT p.invisible
		     AND ST_DWithin(p.location, me.location, $2::float8 + `+itoa(snapMarginM)+`)
		     AND NOT EXISTS (SELECT 1 FROM excl e WHERE e.id = p.user_id)
		     AND ($3::text IS NULL OR p.mode = $3)
		     AND ($4::text IS NULL OR p.gender = $4)
		     AND (cardinality($5::text[]) = 0 OR p.tags && $5::text[])
		     AND ($10::text = '' OR d.goal = $10) AND ($11::text = '' OR d.kids = $11)
		     AND ($12::text = '' OR d.drinking = $12) AND ($13::text = '' OR d.smoking = $13)
		     AND ($14::text = '' OR d.exercise = $14) AND ($15::text = '' OR $15 = ANY(d.languages))
		     AND (NOT $16::bool OR u.id_verified_at IS NOT NULL)
		),
		page AS (
		  SELECT * FROM cand
		   WHERE dist <= $2::float8 AND age BETWEEN $6 AND $7
		   ORDER BY boosted DESC, dist, vip DESC, last_active DESC
		   LIMIT $8 OFFSET $9)
		SELECT p.public_ref::text, p.display_name, p.bio, p.intro, p.mood, p.mode, p.tags, p.photos, g.age, g.dist, g.vip
		  FROM page g JOIN profiles p ON p.user_id = g.user_id
		 ORDER BY g.boosted DESC, g.dist, g.vip DESC, g.last_active DESC`,
		viewerID, radiusM, mode, gender, nonNil(f.Tags), f.AgeMin, f.AgeMax, f.Limit, f.Offset,
		f.Goal, f.Kids, f.Drinking, f.Smoking, f.Exercise, f.Language, f.VerifiedOnly)
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
	return out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
