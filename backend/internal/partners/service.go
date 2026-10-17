// Package partners implements partner venues (hotels, restaurants, activity
// providers), their sponsored events (hotel mixers, concerts, hangouts) and
// booking requests.
//
// Money rules: event tickets are paid in coins through the existing ledger
// (idempotent, refunded on cancellation); bookings only record an agreed
// amount and the platform commission for settlement outside the app. Nothing
// here talks to a payment provider.
//
// Privacy rules: events limited to a hotel's guests are visible only to people
// currently checked in there, and their RSVPs hang off the stay so they are
// deleted with it. Attendance is shown as a count, never as a list of people.
package partners

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/push"
	"connectme/internal/users"
	"connectme/internal/wallet"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("verified members only")
	ErrNeedsPlace   = errors.New("share your location to see what is near you")
	ErrFull         = errors.New("this event is full")
	ErrStarted      = errors.New("this event has already started")
	ErrInsufficient = wallet.ErrInsufficient
)

type TextVetter interface {
	VetText(ctx context.Context, userID, kind, text string) error
}

type Service struct {
	db       *pgxpool.Pool
	vetter   TextVetter
	notifier push.Notifier
	now      func() time.Time
}

// SetNotifier enables push notifications (nil keeps them off).
func (s *Service) SetNotifier(n push.Notifier) { s.notifier = n }

func NewService(db *pgxpool.Pool, v TextVetter) *Service {
	return &Service{db: db, vetter: v, now: time.Now}
}

func clean(s string, min, max int, what string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < min || n > max {
		return "", users.ValidationError{Msg: what + " must be " + itoa(min) + "-" + itoa(max) + " characters"}
	}
	for _, c := range s {
		if unicode.IsControl(c) && c != '\n' {
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

func parse(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	return nil
}

func (s *Service) member(ctx context.Context, userID string) error {
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1::uuid`, userID).Scan(&ok); err != nil || !ok {
		return ErrForbidden
	}
	return nil
}

// ---- partners ----

type Partner struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Address     string `json:"address"`
	Distance    string `json:"distance,omitempty"`
	Bps         int    `json:"commission_bps,omitempty"` // staff views only
	HotelID     string `json:"hotel_id,omitempty"`
}

type CreatePartner struct {
	Kind        string  `json:"kind"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Address     string  `json:"address"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	HotelID     string  `json:"hotel_id"`
	Bps         *int    `json:"commission_bps"`
}

// CreatePartner adds a partner (admin only; the handler enforces it).
func (s *Service) CreatePartner(ctx context.Context, in CreatePartner) (*Partner, error) {
	if in.Kind != "hotel" && in.Kind != "restaurant" && in.Kind != "activity" {
		return nil, users.ValidationError{Msg: "kind must be hotel, restaurant or activity"}
	}
	name, err := clean(in.Name, 2, 80, "name")
	if err != nil {
		return nil, err
	}
	desc, err := clean(in.Description, 0, 300, "description")
	if err != nil {
		return nil, err
	}
	addr, err := clean(in.Address, 0, 200, "address")
	if err != nil {
		return nil, err
	}
	if in.Lat < -90 || in.Lat > 90 || in.Lng < -180 || in.Lng > 180 || (in.Lat == 0 && in.Lng == 0) {
		return nil, users.ValidationError{Msg: "a valid location is required"}
	}
	bps := 1000
	if in.Bps != nil {
		bps = *in.Bps
	}
	if bps < 0 || bps > 3000 {
		return nil, users.ValidationError{Msg: "commission must be 0-30% (0-3000 basis points)"}
	}
	var hotel *string
	if in.HotelID != "" {
		if in.Kind != "hotel" {
			return nil, users.ValidationError{Msg: "only hotel partners can link a hotel"}
		}
		if err := parse(in.HotelID); err != nil {
			return nil, users.ValidationError{Msg: "unknown hotel"}
		}
		hotel = &in.HotelID
	}
	var p Partner
	var hid *string
	err = s.db.QueryRow(ctx, `
		INSERT INTO partners (kind, name, description, address, location, hotel_id, commission_bps)
		VALUES ($1,$2,$3,$4,ST_SetSRID(ST_MakePoint($6,$5),4326)::geography,$7::uuid,$8)
		RETURNING id::text, kind, name, description, address, commission_bps, hotel_id::text`,
		in.Kind, name, desc, addr, in.Lat, in.Lng, hotel, bps).Scan(&p.ID, &p.Kind, &p.Name, &p.Description, &p.Address, &p.Bps, &hid)
	if err != nil {
		return nil, users.ValidationError{Msg: "could not add the partner (is the hotel id valid?)"}
	}
	if hid != nil {
		p.HotelID = *hid
	}
	return &p, nil
}

// Nearby lists active partners within about 50 km of the viewer, plus the
// partner of any hotel they are checked in at.
func (s *Service) Nearby(ctx context.Context, viewerID string) ([]Partner, error) {
	if err := s.member(ctx, viewerID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.id::text, p.kind, p.name, p.description, p.address,
		       CASE WHEN me.location IS NULL THEN -1
		            ELSE ST_Distance(p.location, ST_SnapToGrid(me.location::geometry, 0.01)::geography) END
		  FROM partners p JOIN profiles me ON me.user_id = $1::uuid
		 WHERE p.active
		   AND ((me.location IS NOT NULL AND ST_DWithin(p.location, me.location, 52000))
		        OR EXISTS (SELECT 1 FROM hotel_checkins c WHERE c.user_id = $1::uuid AND c.checked_out_at IS NULL AND c.hotel_id = p.hotel_id))
		 ORDER BY 6 NULLS LAST, p.name LIMIT 50`, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Partner{}
	for rows.Next() {
		var p Partner
		var d float64
		if err := rows.Scan(&p.ID, &p.Kind, &p.Name, &p.Description, &p.Address, &d); err != nil {
			return nil, err
		}
		p.Distance = users.DistanceLabel(d)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- events ----

type Event struct {
	ID         string    `json:"id"`
	PartnerID  string    `json:"partner_id"`
	Partner    string    `json:"partner"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	About      string    `json:"description"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	Capacity   *int      `json:"capacity,omitempty"`
	Going      int       `json:"going"`
	PriceCoins int       `json:"price_coins"`
	GuestsOnly bool      `json:"guests_only"`
	Joined     bool      `json:"joined"`
	Distance   string    `json:"distance,omitempty"`
}

type CreateEvent struct {
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	Capacity    *int      `json:"capacity"`
	PriceCoins  int       `json:"price_coins"`
}

// CreateEvent schedules an event for a partner (admin only).
func (s *Service) CreateEvent(ctx context.Context, partnerID string, in CreateEvent) (*Event, error) {
	if err := parse(partnerID); err != nil {
		return nil, err
	}
	if in.Kind != "mixer" && in.Kind != "concert" && in.Kind != "hangout" {
		return nil, users.ValidationError{Msg: "kind must be mixer, concert or hangout"}
	}
	title, err := clean(in.Title, 3, 80, "title")
	if err != nil {
		return nil, err
	}
	desc, err := clean(in.Description, 0, 500, "description")
	if err != nil {
		return nil, err
	}
	now := s.now()
	switch {
	case !in.StartsAt.After(now):
		return nil, users.ValidationError{Msg: "an event must start in the future"}
	case in.StartsAt.After(now.Add(180 * 24 * time.Hour)):
		return nil, users.ValidationError{Msg: "events can be scheduled up to 180 days ahead"}
	case !in.EndsAt.After(in.StartsAt) || in.EndsAt.Sub(in.StartsAt) > 14*24*time.Hour:
		return nil, users.ValidationError{Msg: "an event lasts up to 14 days and must end after it starts"}
	case in.Capacity != nil && (*in.Capacity < 1 || *in.Capacity > 5000):
		return nil, users.ValidationError{Msg: "capacity must be 1-5000"}
	case in.PriceCoins < 0 || in.PriceCoins > 5000:
		return nil, users.ValidationError{Msg: "price must be 0-5000 coins"}
	}
	var hotel *string
	var active bool
	if err := s.db.QueryRow(ctx, `SELECT hotel_id::text, active FROM partners WHERE id=$1::uuid`, partnerID).Scan(&hotel, &active); err != nil || !active {
		return nil, ErrNotFound
	}
	// A mixer is a hotel's own event for its guests.
	guestsOnly := in.Kind == "mixer"
	if guestsOnly && hotel == nil {
		return nil, users.ValidationError{Msg: "a mixer needs a partner linked to a hotel"}
	}
	var id string
	if err := s.db.QueryRow(ctx, `
		INSERT INTO events (partner_id, kind, title, description, starts_at, ends_at, capacity, price_coins, hotel_guests_only)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id::text`,
		partnerID, in.Kind, title, desc, in.StartsAt, in.EndsAt, in.Capacity, in.PriceCoins, guestsOnly).Scan(&id); err != nil {
		return nil, err
	}
	return &Event{ID: id, PartnerID: partnerID, Kind: in.Kind, Title: title, About: desc, StartsAt: in.StartsAt, EndsAt: in.EndsAt,
		Capacity: in.Capacity, PriceCoins: in.PriceCoins, GuestsOnly: guestsOnly}, nil
}

// events lists what the viewer may see: public events near them and mixers of
// the hotel they are checked in at. eventID narrows to one; mine lists events
// they have joined regardless of where they are now.
func (s *Service) events(ctx context.Context, viewerID string, eventID *string, mine bool) ([]Event, error) {
	rows, err := s.db.Query(ctx, `
		SELECT e.id::text, e.partner_id::text, p.name, e.kind, e.title, e.description, e.starts_at, e.ends_at,
		       e.capacity, e.price_coins, e.hotel_guests_only,
		       (SELECT COUNT(*) FROM event_rsvps r WHERE r.event_id = e.id),
		       EXISTS (SELECT 1 FROM event_rsvps r WHERE r.event_id = e.id AND r.user_id = $1::uuid),
		       CASE WHEN me.location IS NULL OR e.hotel_guests_only THEN -1
		            ELSE ST_Distance(p.location, ST_SnapToGrid(me.location::geometry, 0.01)::geography) END
		  FROM events e
		  JOIN partners p ON p.id = e.partner_id
		  JOIN profiles me ON me.user_id = $1::uuid
		 WHERE e.status = 'scheduled' AND e.ends_at > NOW()
		   AND ($2::uuid IS NULL OR e.id = $2::uuid)
		   AND (
		        ($3 AND EXISTS (SELECT 1 FROM event_rsvps r WHERE r.event_id = e.id AND r.user_id = $1::uuid))
		     OR (p.active AND NOT $3 AND (
		          (NOT e.hotel_guests_only AND me.location IS NOT NULL AND ST_DWithin(p.location, me.location, 52000))
		       OR (e.hotel_guests_only AND EXISTS (SELECT 1 FROM hotel_checkins c
		             WHERE c.user_id = $1::uuid AND c.checked_out_at IS NULL AND c.hotel_id = p.hotel_id))))
		   )
		 ORDER BY e.starts_at LIMIT 50`, viewerID, eventID, mine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var d float64
		if err := rows.Scan(&e.ID, &e.PartnerID, &e.Partner, &e.Kind, &e.Title, &e.About, &e.StartsAt, &e.EndsAt,
			&e.Capacity, &e.PriceCoins, &e.GuestsOnly, &e.Going, &e.Joined, &d); err != nil {
			return nil, err
		}
		e.Distance = users.DistanceLabel(d)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Events lists upcoming events the viewer can see.
func (s *Service) Events(ctx context.Context, viewerID string) ([]Event, error) {
	if err := s.member(ctx, viewerID); err != nil {
		return nil, err
	}
	return s.events(ctx, viewerID, nil, false)
}

// MyEvents lists upcoming events the viewer has joined.
func (s *Service) MyEvents(ctx context.Context, viewerID string) ([]Event, error) {
	if err := s.member(ctx, viewerID); err != nil {
		return nil, err
	}
	return s.events(ctx, viewerID, nil, true)
}

// Join RSVPs the viewer, charging the ticket in coins. Joining twice is a
// no-op and never charges twice.
func (s *Service) Join(ctx context.Context, userID, eventID string) (*Event, error) {
	if err := parse(eventID); err != nil {
		return nil, err
	}
	if err := s.member(ctx, userID); err != nil {
		return nil, err
	}
	vis, err := s.events(ctx, userID, &eventID, false)
	if err != nil {
		return nil, err
	}
	if len(vis) == 0 {
		if mine, _ := s.events(ctx, userID, &eventID, true); len(mine) == 1 {
			return &mine[0], nil // already joined and out of range: idempotent
		}
		return nil, ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var capacity *int
	var price int
	var starts time.Time
	var guestsOnly bool
	var hotel *string
	if err := tx.QueryRow(ctx, `
		SELECT e.capacity, e.price_coins, e.starts_at, e.hotel_guests_only, p.hotel_id::text
		  FROM events e JOIN partners p ON p.id = e.partner_id
		 WHERE e.id = $1::uuid AND e.status = 'scheduled' FOR UPDATE OF e`, eventID).Scan(&capacity, &price, &starts, &guestsOnly, &hotel); err != nil {
		return nil, ErrNotFound
	}
	if !starts.After(s.now()) {
		return nil, ErrStarted
	}
	var going int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM event_rsvps WHERE event_id=$1::uuid`, eventID).Scan(&going); err != nil {
		return nil, err
	}
	var checkin *string
	if guestsOnly {
		if err := tx.QueryRow(ctx, `SELECT id::text FROM hotel_checkins WHERE user_id=$1::uuid AND checked_out_at IS NULL AND hotel_id=$2::uuid`, userID, hotel).Scan(&checkin); err != nil {
			return nil, ErrNotFound
		}
	}
	var ref string
	err = tx.QueryRow(ctx, `
		INSERT INTO event_rsvps (event_id, user_id, checkin_id, coins_paid) VALUES ($1::uuid,$2::uuid,$3::uuid,$4)
		ON CONFLICT DO NOTHING RETURNING ref::text`, eventID, userID, checkin, price).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.one(ctx, userID, eventID) // already joined
	}
	if err != nil {
		return nil, err
	}
	if capacity != nil && going >= *capacity {
		return nil, ErrFull
	}
	if price > 0 {
		if err := wallet.Spend(ctx, tx, userID, price, "event_ticket", "event:"+ref); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.one(ctx, userID, eventID)
}

func (s *Service) one(ctx context.Context, userID, eventID string) (*Event, error) {
	for _, mine := range []bool{false, true} {
		if es, err := s.events(ctx, userID, &eventID, mine); err != nil {
			return nil, err
		} else if len(es) == 1 {
			return &es[0], nil
		}
	}
	return nil, ErrNotFound
}

// Leave cancels the viewer's RSVP; a paid ticket is refunded while the event
// has not started.
func (s *Service) Leave(ctx context.Context, userID, eventID string) error {
	if err := parse(eventID); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var starts time.Time
	if err := tx.QueryRow(ctx, `SELECT starts_at FROM events WHERE id=$1::uuid FOR UPDATE`, eventID).Scan(&starts); err != nil {
		return ErrNotFound
	}
	var paid int
	var ref string
	err = tx.QueryRow(ctx, `DELETE FROM event_rsvps WHERE event_id=$1::uuid AND user_id=$2::uuid RETURNING coins_paid, ref::text`, eventID, userID).Scan(&paid, &ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if paid > 0 && starts.After(s.now()) {
		if _, err := wallet.Grant(ctx, tx, userID, paid, "event_refund", "event-refund:"+ref); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// CancelEvent cancels an event (admin) and refunds every ticket.
func (s *Service) CancelEvent(ctx context.Context, eventID string) error {
	if err := parse(eventID); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `UPDATE events SET status='cancelled' WHERE id=$1::uuid AND status='scheduled'`, eventID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	rows, err := tx.Query(ctx, `DELETE FROM event_rsvps WHERE event_id=$1::uuid RETURNING user_id::text, coins_paid, ref::text`, eventID)
	if err != nil {
		return err
	}
	type refund struct {
		user, ref string
		coins     int
	}
	var refunds []refund
	for rows.Next() {
		var r refund
		if err := rows.Scan(&r.user, &r.coins, &r.ref); err != nil {
			rows.Close()
			return err
		}
		refunds = append(refunds, r)
	}
	rows.Close()
	for _, r := range refunds {
		if r.coins > 0 {
			if _, err := wallet.Grant(ctx, tx, r.user, r.coins, "event_refund", "event-refund:"+r.ref); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
