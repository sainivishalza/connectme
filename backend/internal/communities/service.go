// Package communities implements nearby groups: interest communities that
// people near a place can join, and an automatic "guests at this hotel"
// community for every hotel. Members share a members-only feed (see package
// social; posts of kind 'community').
package communities

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"connectme/internal/users"
	"connectme/pkg/metrics"
	"connectme/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	snapDeg     = "0.01"
	snapMarginM = 2000
	// MaxOwned is how many communities one person may own.
	MaxOwned = 3
	// MaxJoined caps how many interest communities one person may be in.
	MaxJoined = 20
)

var (
	ErrNotFound   = errors.New("not found")
	ErrForbidden  = errors.New("forbidden")
	ErrNeedsPlace = errors.New("share your location first")
	ErrTooFar     = errors.New("this community is too far from you")
	ErrNeedsStay  = errors.New("you must be checked in at this hotel")
	ErrLimit      = errors.New("you have reached the limit for communities")
)

// TextVetter screens public text (satisfied by *users.Service).
type TextVetter interface {
	VetText(ctx context.Context, userID, kind, text string) error
}

type Service struct {
	db     *pgxpool.Pool
	vetter TextVetter
}

func NewService(db *pgxpool.Pool, v TextVetter) *Service { return &Service{db: db, vetter: v} }

// Community is a community as one viewer sees it.
type Community struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"` // interest | hotel
	Tag         string `json:"tag,omitempty"`
	HotelName   string `json:"hotel_name,omitempty"`
	Members     int    `json:"members"`
	Joined      bool   `json:"joined"`
	Owner       bool   `json:"owner"`
	Distance    string `json:"distance,omitempty"`
}

// CreateRequest creates an interest community around the creator's location.
type CreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Tag         string `json:"tag"`
	RadiusKm    int    `json:"radius_km"`
}

func clean(s string, min, max int, what string) (string, error) {
	s = strings.TrimSpace(utils.StripTags(s))
	n := utf8.RuneCountInString(s)
	if n < min || n > max {
		return "", users.ValidationError{Msg: what}
	}
	return s, nil
}

// Create makes an interest community centred (to a ~1 km grid) on the
// creator's location and makes them its owner and first member.
func (s *Service) Create(ctx context.Context, userID string, req CreateRequest) (*Community, error) {
	name, err := clean(req.Name, 3, 60, "a name is 3 to 60 characters")
	if err != nil {
		return nil, err
	}
	desc, err := clean(req.Description, 0, 200, "a description is at most 200 characters")
	if err != nil {
		return nil, err
	}
	tag := strings.ToLower(strings.TrimSpace(utils.StripTags(req.Tag)))
	if n := utf8.RuneCountInString(tag); n > 30 {
		return nil, users.ValidationError{Msg: "a tag is at most 30 characters"}
	}
	radius := req.RadiusKm
	if radius == 0 {
		radius = 25
	}
	if radius < 1 || radius > 100 {
		return nil, users.ValidationError{Msg: "radius must be 1 to 100 km"}
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, userID).Scan(&ok); err != nil || !ok {
		return nil, ErrForbidden
	}
	if s.vetter != nil {
		for _, t := range []string{name, desc} {
			if t == "" {
				continue
			}
			if err := s.vetter.VetText(ctx, userID, "group", t); err != nil {
				return nil, err
			}
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var owned int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM communities WHERE created_by=$1`, userID).Scan(&owned); err != nil {
		return nil, err
	}
	if owned >= MaxOwned {
		return nil, ErrLimit
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO communities (name, description, kind, tag, center, radius_km, created_by)
		SELECT $2, $3, 'interest', NULLIF($4,''),
		       ST_SetSRID(ST_SnapToGrid(p.location::geometry, `+snapDeg+`), 4326)::geography, $5, $1
		  FROM profiles p WHERE p.user_id=$1 AND p.location IS NOT NULL
		RETURNING id::text`, userID, name, desc, tag, radius).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNeedsPlace
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO community_members (community_id, user_id, role) VALUES ($1::uuid,$2,'owner')`, id, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	metrics.Event("community_created")
	return s.one(ctx, userID, id)
}

const selectCommunity = `
	SELECT c.id::text, c.name, c.description, c.kind, COALESCE(c.tag,''), COALESCE(h.name,''),
	       (SELECT COUNT(*) FROM community_members m WHERE m.community_id = c.id),
	       EXISTS (SELECT 1 FROM community_members m WHERE m.community_id = c.id AND m.user_id = $1::uuid),
	       EXISTS (SELECT 1 FROM community_members m WHERE m.community_id = c.id AND m.user_id = $1::uuid AND m.role = 'owner'),
	       CASE WHEN c.center IS NULL OR me.location IS NULL THEN -1 ELSE
	         ST_Distance(c.center, ST_SnapToGrid(me.location::geometry, ` + snapDeg + `)::geography) END
	  FROM communities c
	  LEFT JOIN hotels h ON h.id = c.hotel_id
	  LEFT JOIN profiles me ON me.user_id = $1::uuid`

func (s *Service) scan(rows pgx.Rows) ([]Community, error) {
	defer rows.Close()
	out := []Community{}
	for rows.Next() {
		var c Community
		var dist float64
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.Kind, &c.Tag, &c.HotelName, &c.Members, &c.Joined, &c.Owner, &dist); err != nil {
			return nil, err
		}
		if c.Kind == "interest" {
			c.Distance = users.DistanceLabel(dist)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) one(ctx context.Context, viewerID, id string) (*Community, error) {
	rows, err := s.db.Query(ctx, selectCommunity+` WHERE c.id = $2::uuid`, viewerID, id)
	if err != nil {
		return nil, err
	}
	cs, err := s.scan(rows)
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, ErrNotFound
	}
	return &cs[0], nil
}

// ensureHotelCommunities creates the guests' community for every hotel the
// viewer is currently checked in at, and adds them to it.
func (s *Service) ensureHotelCommunities(ctx context.Context, userID string) error {
	// Two statements: rows created by a CTE are not visible to the statement
	// that reads them, so the community must exist before the membership is added.
	if _, err := s.db.Exec(ctx, `
		INSERT INTO communities (name, description, kind, hotel_id)
		SELECT 'Guests at ' || h.name, 'Everyone checked in here right now. It disappears with your stay.', 'hotel', h.id
		  FROM hotel_checkins c JOIN hotels h ON h.id = c.hotel_id
		 WHERE c.user_id = $1 AND c.checked_out_at IS NULL AND c.auto_delete_at > NOW()
		ON CONFLICT (hotel_id) WHERE kind = 'hotel' DO NOTHING`, userID); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO community_members (community_id, user_id, role, checkin_id)
		SELECT cm.id, $1, 'member', c.id
		  FROM hotel_checkins c JOIN communities cm ON cm.hotel_id = c.hotel_id AND cm.kind = 'hotel'
		 WHERE c.user_id = $1 AND c.checked_out_at IS NULL AND c.auto_delete_at > NOW()
		ON CONFLICT (community_id, user_id) DO UPDATE SET checkin_id = EXCLUDED.checkin_id`, userID)
	return err
}

// Nearby lists communities the viewer can join or has joined: interest
// communities whose area contains the viewer, plus the community of every
// hotel they are checked in at (created and joined automatically).
func (s *Service) Nearby(ctx context.Context, viewerID string) ([]Community, error) {
	if err := s.ensureHotelCommunities(ctx, viewerID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, selectCommunity+`
		 WHERE (c.kind = 'hotel' AND EXISTS (SELECT 1 FROM community_members m WHERE m.community_id = c.id AND m.user_id = $1::uuid))
		    OR (c.kind = 'interest' AND me.location IS NOT NULL
		        AND ST_DWithin(c.center, me.location, c.radius_km * 1000.0 + `+itoa(snapMarginM)+`)
		        AND ST_Distance(c.center, ST_SnapToGrid(me.location::geometry, `+snapDeg+`)::geography) <= c.radius_km * 1000.0)
		 ORDER BY c.kind DESC, 7 DESC, c.created_at DESC
		 LIMIT 50`, viewerID)
	if err != nil {
		return nil, err
	}
	return s.scan(rows)
}

// Mine lists the communities the viewer belongs to.
func (s *Service) Mine(ctx context.Context, viewerID string) ([]Community, error) {
	rows, err := s.db.Query(ctx, selectCommunity+`
		 WHERE EXISTS (SELECT 1 FROM community_members m WHERE m.community_id = c.id AND m.user_id = $1::uuid)
		 ORDER BY c.created_at DESC LIMIT 50`, viewerID)
	if err != nil {
		return nil, err
	}
	return s.scan(rows)
}

// Join adds the viewer. Interest communities are joinable by people inside
// their area; hotel communities only by guests checked in at that hotel.
func (s *Service) Join(ctx context.Context, userID, communityID string) (*Community, error) {
	if _, err := uuid.Parse(communityID); err != nil {
		return nil, ErrNotFound
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, userID).Scan(&ok); err != nil || !ok {
		return nil, ErrForbidden
	}
	var kind string
	var hotel *string
	err := s.db.QueryRow(ctx, `SELECT kind, hotel_id::text FROM communities WHERE id=$1::uuid`, communityID).Scan(&kind, &hotel)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	switch kind {
	case "hotel":
		if err := s.ensureHotelCommunities(ctx, userID); err != nil {
			return nil, err
		}
		var in bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM community_members WHERE community_id=$1::uuid AND user_id=$2)`,
			communityID, userID).Scan(&in); err != nil {
			return nil, err
		}
		if !in {
			return nil, ErrNeedsStay
		}
	default:
		var has, near bool
		if err := s.db.QueryRow(ctx, `
			SELECT me.location IS NOT NULL,
			       COALESCE(ST_DWithin(c.center, me.location, c.radius_km * 1000.0 + `+itoa(snapMarginM)+`)
			                AND ST_Distance(c.center, ST_SnapToGrid(me.location::geometry, `+snapDeg+`)::geography) <= c.radius_km * 1000.0, FALSE)
			  FROM communities c LEFT JOIN profiles me ON me.user_id = $2
			 WHERE c.id = $1::uuid`, communityID, userID).Scan(&has, &near); err != nil {
			return nil, err
		}
		if !has {
			return nil, ErrNeedsPlace
		}
		if !near {
			return nil, ErrTooFar
		}
		var joined int
		if err := s.db.QueryRow(ctx, `
			SELECT COUNT(*) FROM community_members m JOIN communities c ON c.id = m.community_id
			 WHERE m.user_id = $1 AND c.kind = 'interest'`, userID).Scan(&joined); err != nil {
			return nil, err
		}
		if joined >= MaxJoined {
			return nil, ErrLimit
		}
		if _, err := s.db.Exec(ctx, `INSERT INTO community_members (community_id, user_id) VALUES ($1::uuid,$2) ON CONFLICT DO NOTHING`,
			communityID, userID); err != nil {
			return nil, err
		}
	}
	return s.one(ctx, userID, communityID)
}

// Leave removes the viewer. A hotel community cannot be left while you are
// still checked in (it follows your stay); the last owner leaving an interest
// community hands it to the longest-standing member, or deletes it if empty.
func (s *Service) Leave(ctx context.Context, userID, communityID string) error {
	if _, err := uuid.Parse(communityID); err != nil {
		return ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var role, kind string
	err = tx.QueryRow(ctx, `
		SELECT m.role, c.kind FROM community_members m JOIN communities c ON c.id = m.community_id
		 WHERE m.community_id = $1::uuid AND m.user_id = $2 FOR UPDATE OF m`, communityID, userID).Scan(&role, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if kind == "hotel" {
		return ErrForbidden
	}
	if _, err := tx.Exec(ctx, `DELETE FROM community_members WHERE community_id=$1::uuid AND user_id=$2`, communityID, userID); err != nil {
		return err
	}
	if role == "owner" {
		tag, err := tx.Exec(ctx, `
			UPDATE community_members SET role='owner'
			 WHERE (community_id, user_id) = (SELECT community_id, user_id FROM community_members
			                                   WHERE community_id=$1::uuid ORDER BY joined_at LIMIT 1)`, communityID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM communities WHERE id=$1::uuid`, communityID); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

// Delete removes a community (its owner only), with all its posts.
func (s *Service) Delete(ctx context.Context, userID, communityID string) error {
	if _, err := uuid.Parse(communityID); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `
		DELETE FROM communities c WHERE c.id=$1::uuid AND c.kind='interest' AND EXISTS (
		  SELECT 1 FROM community_members m WHERE m.community_id = c.id AND m.user_id = $2 AND m.role = 'owner')`,
		communityID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
