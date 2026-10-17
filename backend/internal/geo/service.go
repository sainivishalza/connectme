package geo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db      *pgxpool.Pool
	users   *users.Service
	purgers []Purger
}

func NewService(db *pgxpool.Pool, u *users.Service, purgers ...Purger) *Service {
	return &Service{db: db, users: u, purgers: purgers}
}

// CreateHotel registers a venue geofence (admin only; checked by the router).
// The polygon must be a valid ring no larger than MaxFenceAreaM2.
func (s *Service) CreateHotel(ctx context.Context, req CreateHotelRequest) (*Hotel, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 255 {
		return nil, users.ValidationError{Msg: "name must be 1-255 characters"}
	}
	n := len(req.Polygon)
	if n < 4 || n > 500 || req.Polygon[0] != req.Polygon[n-1] {
		return nil, users.ValidationError{Msg: "polygon must be a closed ring of 4-500 points"}
	}
	var wkt strings.Builder
	wkt.WriteString("POLYGON((")
	for i, p := range req.Polygon {
		if p[0] < -180 || p[0] > 180 || p[1] < -90 || p[1] > 90 {
			return nil, users.ValidationError{Msg: "polygon coordinates out of range ([lng, lat])"}
		}
		if i > 0 {
			wkt.WriteString(",")
		}
		fmt.Fprintf(&wkt, "%.7f %.7f", p[0], p[1])
	}
	wkt.WriteString("))")

	var valid bool
	var area float64
	err := s.db.QueryRow(ctx, `
		SELECT ST_IsValid(g), ST_Area(g::geography)
		  FROM (SELECT ST_GeomFromText($1, 4326) AS g) t`, wkt.String()).Scan(&valid, &area)
	if err != nil || !valid {
		return nil, users.ValidationError{Msg: "polygon is not a valid shape"}
	}
	if area < 100 || area > MaxFenceAreaM2 {
		return nil, users.ValidationError{Msg: "polygon area must be between 100 m² and 5 km²"}
	}
	h := Hotel{Name: name, Address: strings.TrimSpace(req.Address)}
	err = s.db.QueryRow(ctx, `
		INSERT INTO hotels (name, address, geofence)
		VALUES ($1, $2, ST_GeogFromText('SRID=4326;' || $3)) RETURNING id::text`,
		h.Name, h.Address, wkt.String()).Scan(&h.ID)
	return &h, err
}

// Detect lists venues whose fence contains the caller's last stored location.
func (s *Service) Detect(ctx context.Context, userID string) ([]Hotel, error) {
	rows, err := s.db.Query(ctx, `
		SELECT h.id::text, h.name, COALESCE(h.address,'')
		  FROM hotels h JOIN profiles p ON p.user_id = $1
		 WHERE p.location IS NOT NULL AND ST_Covers(h.geofence, p.location)
		 LIMIT 5`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Hotel{}
	for rows.Next() {
		var h Hotel
		if err := rows.Scan(&h.ID, &h.Name, &h.Address); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// CheckIn puts the caller into hotel mode at a venue. The server verifies the
// stored location is fresh and inside the fence; the client's word is not enough.
// Mode becomes "hotel" (incognito forced) and the ref rotates; the previous
// mode is restored at checkout.
func (s *Service) CheckIn(ctx context.Context, userID, hotelID string) (*CheckIn, error) {
	if _, err := uuid.Parse(hotelID); err != nil {
		return nil, ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		verified bool
		mode     string
		located  *time.Time
		inside   *bool
		hotel    Hotel
	)
	err = tx.QueryRow(ctx, `
		SELECT u.is_verified, p.mode, p.location_updated_at,
		       CASE WHEN p.location IS NULL THEN NULL ELSE ST_Covers(h.geofence, p.location) END,
		       h.id::text, h.name, COALESCE(h.address,'')
		  FROM profiles p
		  JOIN users u ON u.id = p.user_id
		  JOIN hotels h ON h.id = $2::uuid
		 WHERE p.user_id = $1
		 FOR UPDATE OF p`, userID, hotelID).Scan(&verified, &mode, &located, &inside, &hotel.ID, &hotel.Name, &hotel.Address)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	switch {
	case !verified:
		return nil, ErrNeedsVerified
	case located == nil || time.Since(*located) > LocationFreshness || inside == nil:
		return nil, ErrStaleLocation
	case !*inside:
		return nil, ErrOutsideFence
	}

	ci := CheckIn{Hotel: hotel, CheckedInAt: time.Now().UTC()}
	ci.DeleteAt = ci.CheckedInAt.Add(MaxStay + RetentionAfterCheckout)
	err = tx.QueryRow(ctx, `
		INSERT INTO hotel_checkins (user_id, hotel_id, checked_in_at, auto_delete_at)
		VALUES ($1,$2,$3,$4) RETURNING id::text`, userID, hotelID, ci.CheckedInAt, ci.DeleteAt).Scan(&ci.ID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrAlreadyCheckedIn
	}
	if err != nil {
		return nil, err
	}
	// Only open/incognito are remembered; the ref rotates so the hotel card
	// cannot be linked to the person's earlier profile.
	if _, err := tx.Exec(ctx, `
		UPDATE profiles
		   SET prev_mode = CASE WHEN mode IN ('open','incognito') THEN mode END,
		       mode = 'hotel', public_ref = gen_random_uuid(), last_active = NOW()
		 WHERE user_id = $1`, userID); err != nil {
		return nil, err
	}
	return &ci, tx.Commit(ctx)
}

// CheckOut ends the stay. Data stays reachable for RetentionAfterCheckout,
// then the job deletes it.
func (s *Service) CheckOut(ctx context.Context, userID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `
		UPDATE hotel_checkins SET checked_out_at = NOW(), auto_delete_at = NOW() + $2::interval
		 WHERE user_id = $1 AND checked_out_at IS NULL`, userID, RetentionAfterCheckout.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotCheckedIn
	}
	if err := restoreModes(ctx, tx, []string{userID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// restoreModes returns hotel-mode users to the mode they had before check-in
// (incognito if unknown) with a fresh ref. Users who opted into open during
// the stay keep it.
func restoreModes(ctx context.Context, q interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	if _, err := q.Exec(ctx, `
		UPDATE profiles
		   SET mode = COALESCE(prev_mode, 'incognito'), prev_mode = NULL, public_ref = gen_random_uuid()
		 WHERE user_id = ANY($1::uuid[]) AND mode = 'hotel'`, userIDs); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE profiles SET prev_mode = NULL WHERE user_id = ANY($1::uuid[])`, userIDs)
	return err
}

// Current returns the caller's active check-in.
func (s *Service) Current(ctx context.Context, userID string) (*CheckIn, error) {
	var ci CheckIn
	err := s.db.QueryRow(ctx, `
		SELECT c.id::text, c.checked_in_at, c.auto_delete_at, h.id::text, h.name, COALESCE(h.address,''), COALESCE(c.traveler_mood,'')
		  FROM hotel_checkins c JOIN hotels h ON h.id = c.hotel_id
		 WHERE c.user_id = $1 AND c.checked_out_at IS NULL`, userID).
		Scan(&ci.ID, &ci.CheckedInAt, &ci.DeleteAt, &ci.Hotel.ID, &ci.Hotel.Name, &ci.Hotel.Address, &ci.Mood)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotCheckedIn
	}
	return &ci, err
}

// activeCheckin returns the caller's active check-in id at a specific hotel.
func (s *Service) activeCheckin(ctx context.Context, userID, hotelID string) (checkinID, hotelName string, err error) {
	if _, perr := uuid.Parse(hotelID); perr != nil {
		return "", "", ErrNotFound
	}
	err = s.db.QueryRow(ctx, `
		SELECT c.id::text, h.name FROM hotel_checkins c JOIN hotels h ON h.id = c.hotel_id
		 WHERE c.user_id = $1 AND c.hotel_id = $2::uuid AND c.checked_out_at IS NULL`, userID, hotelID).
		Scan(&checkinID, &hotelName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotCheckedIn
	}
	return checkinID, hotelName, err
}
