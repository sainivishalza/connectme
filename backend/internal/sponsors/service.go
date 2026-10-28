// Package sponsors serves sponsored placements in the feed.
//
// Rules: members on Silver or above never see one; a member sees at most one
// every few minutes (a Redis cap, so a feed refresh loop cannot be used to
// pile impressions onto an advertiser); a placement with a place only reaches
// people whose coarse (grid-snapped) location is inside its radius; links must
// be plain https; text passes the same screening as other public text. Nothing
// records who saw or clicked a placement, only totals.
package sponsors

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/entitlements"
	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var ErrNotFound = errors.New("not found")

const seenWindow = 3 * time.Minute

type TextVetter interface {
	VetText(ctx context.Context, userID, kind, text string) error
}

type Service struct {
	db     *pgxpool.Pool
	rdb    *redis.Client
	vetter TextVetter
	now    func() time.Time
}

func NewService(db *pgxpool.Pool, rdb *redis.Client, v TextVetter) *Service {
	return &Service{db: db, rdb: rdb, vetter: v, now: time.Now}
}

// Placement is what a member sees. Note what is absent: no counters.
type Placement struct {
	ID         string `json:"id"`
	Advertiser string `json:"advertiser"`
	Headline   string `json:"headline"`
	Body       string `json:"body,omitempty"`
	LinkURL    string `json:"link_url,omitempty"`
}

// Admin is the staff view with performance totals.
type Admin struct {
	Placement
	StartsAt       time.Time `json:"starts_at"`
	EndsAt         time.Time `json:"ends_at"`
	RadiusKm       *int      `json:"radius_km,omitempty"`
	MaxImpressions *int      `json:"max_impressions,omitempty"`
	Active         bool      `json:"active"`
	Impressions    int       `json:"impressions"`
	Clicks         int       `json:"clicks"`
}

type CreateRequest struct {
	Advertiser     string    `json:"advertiser"`
	Headline       string    `json:"headline"`
	Body           string    `json:"body"`
	LinkURL        string    `json:"link_url"`
	StartsAt       time.Time `json:"starts_at"`
	EndsAt         time.Time `json:"ends_at"`
	Lat            *float64  `json:"lat"`
	Lng            *float64  `json:"lng"`
	RadiusKm       *int      `json:"radius_km"`
	MaxImpressions *int      `json:"max_impressions"`
}

func clean(s string, min, max int, what string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < min || n > max {
		return "", users.ValidationError{Msg: what + " must be " + itoa(min) + "-" + itoa(max) + " characters"}
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return "", users.ValidationError{Msg: what + " contains invalid characters"}
		}
	}
	return s, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// validLink accepts only plain https links to a named host (no credentials,
// no IP literals, no localhost): a placement must not be a way to point
// members at internal addresses or phishing-style userinfo tricks.
func validLink(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || len(raw) > 300 {
		return "", users.ValidationError{Msg: "the link must be a plain https address"}
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || !strings.Contains(h, ".") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") {
		return "", users.ValidationError{Msg: "the link must point to a public website"}
	}
	last := h[strings.LastIndexByte(h, '.')+1:]
	if last == "" || (last[0] >= '0' && last[0] <= '9') {
		return "", users.ValidationError{Msg: "the link must use a domain name, not an IP address"}
	}
	return u.String(), nil
}

// Create adds a placement (admin only; the handler enforces it).
func (s *Service) Create(ctx context.Context, adminID string, in CreateRequest) (*Admin, error) {
	adv, err := clean(in.Advertiser, 2, 60, "advertiser")
	if err != nil {
		return nil, err
	}
	head, err := clean(in.Headline, 3, 80, "headline")
	if err != nil {
		return nil, err
	}
	body, err := clean(in.Body, 0, 200, "text")
	if err != nil {
		return nil, err
	}
	link, err := validLink(in.LinkURL)
	if err != nil {
		return nil, err
	}
	now := s.now()
	switch {
	case !in.EndsAt.After(in.StartsAt):
		return nil, users.ValidationError{Msg: "the end must be after the start"}
	case in.EndsAt.Before(now):
		return nil, users.ValidationError{Msg: "the end is in the past"}
	case in.EndsAt.Sub(in.StartsAt) > 366*24*time.Hour:
		return nil, users.ValidationError{Msg: "a placement can run for a year at most"}
	case (in.Lat == nil) != (in.Lng == nil) || (in.Lat == nil) != (in.RadiusKm == nil):
		return nil, users.ValidationError{Msg: "lat, lng and radius_km go together"}
	case in.RadiusKm != nil && (*in.RadiusKm < 1 || *in.RadiusKm > 200):
		return nil, users.ValidationError{Msg: "radius must be 1-200 km"}
	case in.Lat != nil && (*in.Lat < -90 || *in.Lat > 90 || *in.Lng < -180 || *in.Lng > 180):
		return nil, users.ValidationError{Msg: "a valid location is required"}
	case in.MaxImpressions != nil && *in.MaxImpressions < 1:
		return nil, users.ValidationError{Msg: "max impressions must be positive"}
	}
	if s.vetter != nil {
		for _, t := range []string{adv, head, body} {
			if t == "" {
				continue
			}
			if err := s.vetter.VetText(ctx, adminID, "post", t); err != nil {
				return nil, err
			}
		}
	}
	var id string
	err = s.db.QueryRow(ctx, `
		INSERT INTO sponsor_placements (advertiser, headline, body, link_url, starts_at, ends_at, location, radius_km, max_impressions, created_by)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,
		        CASE WHEN $7::float8 IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint($8::float8,$7::float8),4326)::geography END,
		        $9,$10,$11::uuid) RETURNING id::text`,
		adv, head, body, link, in.StartsAt, in.EndsAt, in.Lat, in.Lng, in.RadiusKm, in.MaxImpressions, adminID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.adminOne(ctx, id)
}

func (s *Service) adminOne(ctx context.Context, id string) (*Admin, error) {
	var a Admin
	var link *string
	err := s.db.QueryRow(ctx, `
		SELECT id::text, advertiser, headline, body, link_url, starts_at, ends_at, radius_km, max_impressions, active, impressions, clicks
		  FROM sponsor_placements WHERE id=$1::uuid`, id).
		Scan(&a.ID, &a.Advertiser, &a.Headline, &a.Body, &link, &a.StartsAt, &a.EndsAt, &a.RadiusKm, &a.MaxImpressions, &a.Active, &a.Impressions, &a.Clicks)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if link != nil {
		a.LinkURL = *link
	}
	return &a, err
}

// List returns every placement with totals (admin only).
func (s *Service) List(ctx context.Context) ([]Admin, error) {
	rows, err := s.db.Query(ctx, `SELECT id::text FROM sponsor_placements ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []Admin{}
	for _, id := range ids {
		a, err := s.adminOne(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, nil
}

// SetActive pauses or resumes a placement (admin only).
func (s *Service) SetActive(ctx context.Context, id string, active bool) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `UPDATE sponsor_placements SET active=$2 WHERE id=$1::uuid`, id, active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ForFeed returns at most one placement for this member, or nil: nothing for
// Silver and above, nothing if they saw one in the last few minutes, and
// otherwise the eligible placement shown least so far (fair rotation).
func (s *Service) ForFeed(ctx context.Context, userID string) (*Placement, error) {
	tier, err := entitlements.TierOf(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	if entitlements.PerksFor(tier).NoAds {
		return nil, nil
	}
	if s.rdb != nil {
		ok, err := s.rdb.SetNX(ctx, "sponsor:seen:"+userID, 1, seenWindow).Result()
		if err == nil && !ok {
			return nil, nil
		}
	}
	var p Placement
	var link *string
	err = s.db.QueryRow(ctx, `
		WITH me AS (
		  SELECT pr.location FROM profiles pr JOIN users u ON u.id = pr.user_id
		   WHERE pr.user_id = $1::uuid AND u.is_verified AND NOT u.is_banned)
		UPDATE sponsor_placements sp SET impressions = impressions + 1
		 WHERE sp.id = (
		   SELECT x.id FROM sponsor_placements x, me
		    WHERE x.active AND NOW() BETWEEN x.starts_at AND x.ends_at
		      AND (x.max_impressions IS NULL OR x.impressions < x.max_impressions)
		      AND (x.location IS NULL OR (me.location IS NOT NULL AND ST_DWithin(x.location, me.location, x.radius_km * 1000.0 + 2000)
		           AND ST_Distance(x.location, ST_SnapToGrid(me.location::geometry, 0.01)::geography) <= x.radius_km * 1000.0))
		    ORDER BY x.impressions, x.created_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING sp.id::text, sp.advertiser, sp.headline, sp.body, sp.link_url`, userID).
		Scan(&p.ID, &p.Advertiser, &p.Headline, &p.Body, &link)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if link != nil {
		p.LinkURL = *link
	}
	return &p, nil
}

// Click counts a click and returns the link to open.
func (s *Service) Click(ctx context.Context, id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", ErrNotFound
	}
	var link *string
	err := s.db.QueryRow(ctx, `
		UPDATE sponsor_placements SET clicks = clicks + 1
		 WHERE id=$1::uuid AND active AND NOW() BETWEEN starts_at AND ends_at AND link_url IS NOT NULL
		RETURNING link_url`, id).Scan(&link)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil || link == nil {
		return "", err
	}
	return *link, nil
}
