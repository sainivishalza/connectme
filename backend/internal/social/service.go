// Package social implements posts: Nearby Moments, the anonymous Little
// Universe, and Missed Connections at venues. See migrations/019_posts.sql for
// the privacy model.
package social

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"math/big"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/push"
	"connectme/internal/users"
	"connectme/pkg/metrics"
	"connectme/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Kinds of post.
const (
	KindMoment   = "moment"
	KindUniverse = "universe"
	KindMissed   = "missed"
	// KindCommunity is a post in a members-only community.
	KindCommunity = "community"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrForbidden  = errors.New("forbidden")
	ErrNeedsPlace = errors.New("share your location first")
	ErrNeedsStay  = errors.New("check in at a venue first")
	ErrLimit      = errors.New("you have reached today's limit for this kind of post")
)

// dailyLimit is how many posts of each kind one person may make per day.
var dailyLimit = map[string]int{KindMoment: 20, KindUniverse: 10, KindMissed: 5, KindCommunity: 30}

// lifetime is how long a post stays visible.
var lifetime = map[string]time.Duration{KindMoment: 24 * time.Hour, KindUniverse: 48 * time.Hour, KindMissed: 72 * time.Hour, KindCommunity: 14 * 24 * time.Hour}

const (
	// snapDeg and snapMarginM mirror the nearby search: positions are snapped to
	// a ~1.1 km grid and a margin covers the error that adds.
	snapDeg     = "0.01"
	snapMarginM = 2000
	maxBody     = 280
)

// TextVetter screens public text (satisfied by *users.Service).
type TextVetter interface {
	VetText(ctx context.Context, userID, kind, text string) error
}

type Service struct {
	db       *pgxpool.Pool
	users    *users.Service
	vetter   TextVetter
	notifier push.Notifier
}

// SetNotifier enables push notifications (nil keeps them off).
func (s *Service) SetNotifier(n push.Notifier) { s.notifier = n }

func NewService(db *pgxpool.Pool, u *users.Service) *Service {
	return &Service{db: db, users: u, vetter: u}
}

// Post is a post as one viewer may see it.
type Post struct {
	ID          string               `json:"id"`
	Kind        string               `json:"kind"`
	CommunityID string               `json:"community_id,omitempty"`
	Body        string               `json:"body"`
	Alias       string               `json:"alias,omitempty"`
	Author      *users.PublicProfile `json:"author,omitempty"`
	Distance    string               `json:"distance,omitempty"`
	HotelName   string               `json:"hotel_name,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	ExpiresAt   time.Time            `json:"expires_at"`
	Mine        bool                 `json:"mine"`
	Likes       int                  `json:"likes"`
	Liked       bool                 `json:"liked"`
	Comments    int                  `json:"comments"`
	Claims      int                  `json:"claims,omitempty"`  // pending claims, on your own missed post
	Claimed     bool                 `json:"claimed,omitempty"` // you already said "that's me"
}

var (
	adjectives = []string{"Quiet", "Curious", "Golden", "Gentle", "Brave", "Sleepy", "Bright", "Calm", "Clever", "Lucky", "Mellow", "Wandering", "Cosy", "Witty", "Sunny", "Midnight"}
	nouns      = []string{"Fox", "Owl", "Otter", "Heron", "Panda", "Falcon", "Lynx", "Robin", "Koala", "Badger", "Swan", "Wren", "Dolphin", "Hare", "Finch", "Moth"}
)

func pickOne(list []string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		return list[0]
	}
	return list[n.Int64()]
}

func cleanBody(s string) (string, error) {
	s = utils.StripTags(s)
	n := utf8.RuneCountInString(s)
	if n < 3 || n > maxBody {
		return "", users.ValidationError{Msg: "a post is 3 to 280 characters"}
	}
	for _, c := range s {
		if unicode.IsControl(c) && c != '\n' {
			return "", users.ValidationError{Msg: "the post contains invalid characters"}
		}
	}
	return s, nil
}

// Create publishes a post of the given kind.
func (s *Service) Create(ctx context.Context, userID, kind, body string) (*Post, error) {
	life, ok := lifetime[kind]
	if !ok {
		return nil, users.ValidationError{Msg: "kind must be moment, universe or missed"}
	}
	body, err := cleanBody(body)
	if err != nil {
		return nil, err
	}
	var okUser bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, userID).Scan(&okUser); err != nil || !okUser {
		return nil, ErrForbidden
	}
	var today int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM posts WHERE author_id=$1 AND kind=$2 AND created_at > NOW() - INTERVAL '24 hours'`,
		userID, kind).Scan(&today); err != nil {
		return nil, err
	}
	if today >= dailyLimit[kind] {
		return nil, ErrLimit
	}
	if s.vetter != nil {
		if err := s.vetter.VetText(ctx, userID, "post", body); err != nil {
			return nil, err
		}
	}

	var id string
	switch kind {
	case KindMoment, KindUniverse:
		alias := ""
		if kind == KindUniverse {
			alias = pickOne(adjectives) + " " + pickOne(nouns)
		}
		err = s.db.QueryRow(ctx, `
			INSERT INTO posts (author_id, kind, body, alias, location, expires_at)
			SELECT $1, $2, $3, NULLIF($4,''),
			       ST_SetSRID(ST_SnapToGrid(p.location::geometry, `+snapDeg+`), 4326)::geography, NOW() + $5::interval
			  FROM profiles p WHERE p.user_id=$1 AND p.location IS NOT NULL
			RETURNING id::text`, userID, kind, body, alias, life.String()).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNeedsPlace
		}
	case KindMissed:
		err = s.db.QueryRow(ctx, `
			INSERT INTO posts (author_id, kind, body, hotel_id, checkin_id, expires_at)
			SELECT $1, 'missed', $2, c.hotel_id, c.id, LEAST(NOW() + $3::interval, c.auto_delete_at)
			  FROM hotel_checkins c WHERE c.user_id=$1 AND c.auto_delete_at > NOW()
			 ORDER BY c.checked_in_at DESC LIMIT 1
			RETURNING id::text`, userID, body, life.String()).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNeedsStay
		}
	}
	if err != nil {
		return nil, err
	}
	metrics.Event("post_" + kind)
	return s.one(ctx, userID, id)
}

// filter selects posts. Unset fields do not restrict.
type filter struct {
	kind      string  // "" = any
	postID    string  // a single post
	mine      bool    // only the viewer's own posts
	radiusM   float64 // > 0: within this distance of the viewer (moments, universe)
	atVenue   bool    // only posts at venues where the viewer is checked in now
	community string  // only posts in this community (the viewer must be a member)
	before    string  // cursor: id of the last post of the previous page
	limit     int
}

// one loads a single post as the viewer may see it.
func (s *Service) one(ctx context.Context, viewerID, postID string) (*Post, error) {
	ps, err := s.query(ctx, viewerID, filter{postID: postID, limit: 1})
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		return nil, ErrNotFound
	}
	return &ps[0], nil
}

// query is the one read path for posts. Every condition is a fixed fragment
// with its own parameter, so no part of the SQL is built from input.
func (s *Service) query(ctx context.Context, viewerID string, f filter) ([]Post, error) {
	if f.limit < 1 || f.limit > 50 {
		f.limit = 30
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.id::text, p.kind, p.body, COALESCE(p.alias,''), p.author_id::text, p.created_at, p.expires_at, COALESCE(p.community_id::text,''),
		       p.author_id = $1::uuid,
		       (SELECT COUNT(*) FROM post_likes l WHERE l.post_id = p.id),
		       EXISTS (SELECT 1 FROM post_likes l WHERE l.post_id = p.id AND l.user_id = $1::uuid),
		       (SELECT COUNT(*) FROM post_comments pc WHERE pc.post_id = p.id AND `+users.NotBlockedSQL("pc.author_id", "$1::uuid")+`),
		       COALESCE(h.name, ''),
		       CASE WHEN p.location IS NULL OR me.location IS NULL THEN -1 ELSE
		         ST_Distance(p.location,
		                     ms.loc) END,
		       CASE WHEN p.kind = 'missed' AND p.author_id = $1::uuid
		            THEN (SELECT COUNT(*) FROM missed_claims c WHERE c.post_id = p.id AND c.status = 'pending') ELSE 0 END,
		       EXISTS (SELECT 1 FROM missed_claims c WHERE c.post_id = p.id AND c.claimant_id = $1::uuid)
		  FROM posts p
		  JOIN users au ON au.id = p.author_id AND NOT au.is_banned
		  LEFT JOIN hotels h ON h.id = p.hotel_id
		  LEFT JOIN profiles me ON me.user_id = $1::uuid
		  LEFT JOIN LATERAL (SELECT ST_SnapToGrid(me.location::geometry, `+snapDeg+`)::geography AS loc) ms ON TRUE
		 WHERE p.expires_at > NOW()
		   AND ($2::text IS NULL OR p.kind = $2::text)
		   AND ($3::uuid IS NULL OR p.id = $3::uuid)
		   AND (NOT $4::boolean OR p.author_id = $1::uuid)
		   AND ($5::float8 <= 0 OR (
		         me.location IS NOT NULL AND p.location IS NOT NULL
		         AND ST_DWithin(p.location, me.location, $5::float8 + `+itoa(snapMarginM)+`)
		         AND ST_Distance(p.location,
		                         ms.loc) <= $5::float8))
		   AND (NOT $6::boolean OR p.hotel_id IN (
		         SELECT c.hotel_id FROM hotel_checkins c
		          WHERE c.user_id = $1::uuid AND c.checked_out_at IS NULL AND c.auto_delete_at > NOW()))
		   AND ($7::uuid IS NULL OR (p.created_at, p.id) < (SELECT created_at, id FROM posts WHERE id = $7::uuid))
		   AND ($9::uuid IS NULL OR p.community_id = $9::uuid)
		   AND (p.kind <> 'community' OR EXISTS (SELECT 1 FROM community_members m WHERE m.community_id = p.community_id AND m.user_id = $1::uuid))
		   AND (p.author_id = $1::uuid OR `+users.NotBlockedSQL("p.author_id", "$1::uuid")+`)
		 ORDER BY p.created_at DESC, p.id DESC
		 LIMIT $8`,
		viewerID, nullIf(f.kind), nullIf(f.postID), f.mine, f.radiusM, f.atVenue, nullIf(f.before), f.limit, nullIf(f.community))
	if err != nil {
		return nil, err
	}
	type rec struct {
		p      Post
		author string
		dist   float64
	}
	var recs []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.p.ID, &r.p.Kind, &r.p.Body, &r.p.Alias, &r.author, &r.p.CreatedAt, &r.p.ExpiresAt, &r.p.CommunityID,
			&r.p.Mine, &r.p.Likes, &r.p.Liked, &r.p.Comments, &r.p.HotelName, &r.dist, &r.p.Claims, &r.p.Claimed); err != nil {
			rows.Close()
			return nil, err
		}
		recs = append(recs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var authors []string
	for _, r := range recs {
		if r.p.Kind == KindMoment || r.p.Kind == KindCommunity {
			authors = append(authors, r.author)
		}
	}
	cards, err := s.users.Cards(ctx, viewerID, authors, "")
	if err != nil {
		return nil, err
	}
	out := make([]Post, 0, len(recs))
	for _, r := range recs {
		if r.p.Kind == KindMoment {
			r.p.Distance = users.DistanceLabel(r.dist)
		}
		if r.p.Kind == KindMoment || r.p.Kind == KindCommunity {
			// The author appears as their own mode allows to this viewer.
			card, ok := cards[r.author]
			if !ok {
				continue // hidden from this viewer (blocked, banned or unverified)
			}
			r.p.Author = &card
		}
		// Universe and missed posts are never linked to a profile.
		out = append(out, r.p)
	}
	return out, nil
}

func nullIf(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func itoa(n int) string { return strconv.Itoa(n) }

// Feed lists posts of a kind the viewer may see, newest first. `before` is the
// id of the last post of the previous page.
func (s *Service) Feed(ctx context.Context, viewerID, kind string, radiusKm int, before string, limit int) ([]Post, error) {
	f := filter{kind: kind, before: before, limit: limit}
	if before != "" {
		if _, err := uuid.Parse(before); err != nil {
			return nil, users.ValidationError{Msg: "invalid cursor"}
		}
	}
	switch kind {
	case KindMoment, KindUniverse:
		if radiusKm < 1 || radiusKm > 100 {
			radiusKm = 10
		}
		if kind == KindUniverse && radiusKm < 25 {
			radiusKm = 25
		}
		var has bool
		if err := s.db.QueryRow(ctx, `SELECT location IS NOT NULL FROM profiles WHERE user_id=$1`, viewerID).Scan(&has); err != nil || !has {
			return nil, ErrNeedsPlace
		}
		f.radiusM = float64(radiusKm) * 1000
	case KindMissed:
		f.atVenue = true
	default:
		return nil, users.ValidationError{Msg: "kind must be moment, universe or missed"}
	}
	return s.query(ctx, viewerID, f)
}

// Mine lists the caller's own unexpired posts.
func (s *Service) Mine(ctx context.Context, userID string) ([]Post, error) {
	return s.query(ctx, userID, filter{mine: true, limit: 50})
}

// Delete removes the caller's own post.
func (s *Service) Delete(ctx context.Context, userID, postID string) error {
	if _, err := uuid.Parse(postID); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `
		DELETE FROM posts p WHERE p.id=$1::uuid AND (p.author_id=$2 OR (p.kind='community' AND EXISTS (
		  SELECT 1 FROM community_members m WHERE m.community_id = p.community_id AND m.user_id = $2 AND m.role = 'owner')))`,
		postID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Like toggles the caller's like on a post they can see, returning the new state.
func (s *Service) Like(ctx context.Context, userID, postID string) (liked bool, likes int, err error) {
	if _, perr := uuid.Parse(postID); perr != nil {
		return false, 0, ErrNotFound
	}
	if err := s.canSee(ctx, userID, postID); err != nil {
		return false, 0, err
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM post_likes WHERE post_id=$1::uuid AND user_id=$2`, postID, userID)
	if err != nil {
		return false, 0, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.db.Exec(ctx, `INSERT INTO post_likes (post_id, user_id) VALUES ($1::uuid,$2) ON CONFLICT DO NOTHING`, postID, userID); err != nil {
			return false, 0, err
		}
		liked = true
	}
	err = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM post_likes WHERE post_id=$1::uuid`, postID).Scan(&likes)
	return liked, likes, err
}

// canSee reports whether the viewer may interact with a post, using the same
// rules as the feeds (nearby for moments and universe, same venue for missed).
func (s *Service) canSee(ctx context.Context, viewerID, postID string) error {
	var ok bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM posts p
		    JOIN users au ON au.id = p.author_id AND NOT au.is_banned
		    LEFT JOIN profiles me ON me.user_id = $1::uuid
		   WHERE p.id = $2::uuid AND p.expires_at > NOW()
		     AND (p.author_id = $1::uuid OR `+users.NotBlockedSQL("p.author_id", "$1::uuid")+`)
		     AND (
		       (p.kind IN ('moment','universe') AND me.location IS NOT NULL
		          AND ST_DWithin(p.location, me.location, 100000 + `+itoa(snapMarginM)+`))
		       OR (p.kind = 'missed' AND p.hotel_id IN (
		            SELECT c.hotel_id FROM hotel_checkins c
		             WHERE c.user_id = $1::uuid AND c.checked_out_at IS NULL AND c.auto_delete_at > NOW()))
		       OR (p.kind = 'community' AND EXISTS (
		            SELECT 1 FROM community_members m WHERE m.community_id = p.community_id AND m.user_id = $1::uuid))
		       OR p.author_id = $1::uuid))`, viewerID, postID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// Report files a report against a post's author with the post text as
// evidence. Universe posts are anonymous to readers, but staff can still act
// on the report.
func (s *Service) Report(ctx context.Context, userID, postID, reason string) error {
	if _, err := uuid.Parse(postID); err != nil {
		return ErrNotFound
	}
	switch reason {
	case "spam", "harassment", "inappropriate_content", "scam", "underage", "other":
	default:
		return users.ValidationError{Msg: "unknown reason"}
	}
	if err := s.canSee(ctx, userID, postID); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason, details, evidence)
		SELECT $1, p.author_id, $3, 'post ' || p.kind, jsonb_build_array(p.body)
		  FROM posts p WHERE p.id = $2::uuid AND p.author_id <> $1`, userID, postID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Expire deletes posts past their time.
func (s *Service) Expire(ctx context.Context) (int, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM posts WHERE expires_at <= NOW()`)
	return int(tag.RowsAffected()), err
}

// StartJob deletes expired posts every interval until ctx is cancelled.
func (s *Service) StartJob(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			started := time.Now()
			n, err := s.Expire(ctx)
			metrics.Job("post_expiry", time.Since(started), err)
			metrics.JobItems("post_expiry", "deleted", n)
			if err != nil {
				slog.Error("social: expiry job", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
