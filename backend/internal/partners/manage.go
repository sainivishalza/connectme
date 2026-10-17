package partners

import (
	"context"
	"errors"
	"strings"

	"connectme/internal/users"

	"github.com/jackc/pgx/v5"
)

// The partner portal. A manager is a member linked to a partner by an admin;
// every call here first proves that link, so a manager can never touch another
// partner's events or bookings, and ids from other partners look like
// "not found".
//
// Privacy: a venue needs to know who is coming, but a member in Incognito or
// Hotel mode has not agreed to show a name. Managers therefore see a booking's
// guest as the member's display name only for Open-mode members, and as
// "Guest XXXX" (the booking's short code) otherwise. They never see a public
// ref, an email or anything about the member beyond the booking itself.

const maxScheduledEvents = 20

// Manager is how an admin sees a partner's manager (never an email).
type Manager struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
}

// AddManager links the member with this email to a partner (admin only).
func (s *Service) AddManager(ctx context.Context, partnerID, email string) (*Manager, error) {
	if err := parse(partnerID); err != nil {
		return nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(email) > 254 {
		return nil, users.ValidationError{Msg: "an email address is required"}
	}
	var m Manager
	err := s.db.QueryRow(ctx, `
		WITH target AS (
		  SELECT u.id FROM users u WHERE lower(u.email) = $2 AND u.is_verified AND NOT u.is_banned),
		ins AS (
		  INSERT INTO partner_managers (partner_id, user_id)
		  SELECT $1::uuid, t.id FROM target t WHERE EXISTS (SELECT 1 FROM partners WHERE id = $1::uuid)
		  ON CONFLICT DO NOTHING RETURNING user_id)
		SELECT p.public_ref::text, p.display_name FROM profiles p WHERE p.user_id = (SELECT id FROM target)`, partnerID, email).
		Scan(&m.Ref, &m.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, users.ValidationError{Msg: "no verified member with that email"}
	}
	return &m, err
}

// Managers lists a partner's managers.
func (s *Service) Managers(ctx context.Context, partnerID string) ([]Manager, error) {
	if err := parse(partnerID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.public_ref::text, p.display_name FROM partner_managers m JOIN profiles p ON p.user_id = m.user_id
		 WHERE m.partner_id = $1::uuid ORDER BY m.created_at`, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Manager{}
	for rows.Next() {
		var m Manager
		if err := rows.Scan(&m.Ref, &m.Name); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RemoveManager unlinks a manager by their public ref (admin only).
func (s *Service) RemoveManager(ctx context.Context, partnerID, ref string) error {
	if err := parse(partnerID); err != nil {
		return err
	}
	if err := parse(ref); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		DELETE FROM partner_managers WHERE partner_id = $1::uuid
		   AND user_id = (SELECT user_id FROM profiles WHERE public_ref = $2::uuid)`, partnerID, ref)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// manages proves the caller manages the partner.
func (s *Service) manages(ctx context.Context, userID, partnerID string) error {
	if err := parse(partnerID); err != nil {
		return err
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM partner_managers m JOIN users u ON u.id = m.user_id
		                WHERE m.partner_id = $1::uuid AND m.user_id = $2::uuid AND NOT u.is_banned)`, partnerID, userID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// Managed lists the partners the caller manages.
func (s *Service) Managed(ctx context.Context, userID string) ([]Partner, error) {
	rows, err := s.db.Query(ctx, `
		SELECT p.id::text, p.kind, p.name, p.description, p.address, p.commission_bps
		  FROM partner_managers m JOIN partners p ON p.id = m.partner_id
		 WHERE m.user_id = $1::uuid ORDER BY p.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Partner{}
	for rows.Next() {
		var p Partner
		if err := rows.Scan(&p.ID, &p.Kind, &p.Name, &p.Description, &p.Address, &p.Bps); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ManagedEvent is an event as its partner sees it, with takings.
type ManagedEvent struct {
	Event
	Status      string `json:"status"`
	CoinsEarned int    `json:"coins_earned"`
}

// ManagerEvents lists a partner's events from the last 30 days onwards.
func (s *Service) ManagerEvents(ctx context.Context, userID, partnerID string) ([]ManagedEvent, error) {
	if err := s.manages(ctx, userID, partnerID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT e.id::text, e.partner_id::text, e.kind, e.title, e.description, e.starts_at, e.ends_at, e.capacity,
		       e.price_coins, e.hotel_guests_only, e.status,
		       (SELECT COUNT(*) FROM event_rsvps r WHERE r.event_id = e.id),
		       COALESCE((SELECT SUM(r.coins_paid) FROM event_rsvps r WHERE r.event_id = e.id), 0)
		  FROM events e WHERE e.partner_id = $1::uuid AND e.ends_at > NOW() - INTERVAL '30 days'
		 ORDER BY e.starts_at DESC LIMIT 100`, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManagedEvent{}
	for rows.Next() {
		var e ManagedEvent
		if err := rows.Scan(&e.ID, &e.PartnerID, &e.Kind, &e.Title, &e.About, &e.StartsAt, &e.EndsAt, &e.Capacity,
			&e.PriceCoins, &e.GuestsOnly, &e.Status, &e.Going, &e.CoinsEarned); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ManagerCreateEvent schedules an event for a partner the caller manages.
func (s *Service) ManagerCreateEvent(ctx context.Context, userID, partnerID string, in CreateEvent) (*Event, error) {
	if err := s.manages(ctx, userID, partnerID); err != nil {
		return nil, err
	}
	var n int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE partner_id=$1::uuid AND status='scheduled' AND ends_at > NOW()`, partnerID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= maxScheduledEvents {
		return nil, users.ValidationError{Msg: "a partner can have 20 upcoming events; cancel or wait for one to pass"}
	}
	return s.CreateEvent(ctx, partnerID, in)
}

// ManagerCancelEvent cancels an event of a partner the caller manages and
// refunds every ticket.
func (s *Service) ManagerCancelEvent(ctx context.Context, userID, eventID string) error {
	if err := parse(eventID); err != nil {
		return err
	}
	var partnerID string
	if err := s.db.QueryRow(ctx, `SELECT partner_id::text FROM events WHERE id=$1::uuid`, eventID).Scan(&partnerID); err != nil {
		return ErrNotFound
	}
	if err := s.manages(ctx, userID, partnerID); err != nil {
		return err
	}
	return s.CancelEvent(ctx, eventID)
}

// ManagerBookings lists a partner's bookings, newest first.
func (s *Service) ManagerBookings(ctx context.Context, userID, partnerID, status string) ([]Booking, error) {
	if err := s.manages(ctx, userID, partnerID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, bookingSelect+`
		 WHERE b.partner_id = $1::uuid AND ($2 = '' OR b.status = $2) ORDER BY b.requested_for DESC LIMIT 100`, partnerID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Booking{}
	for rows.Next() {
		b, err := scanBooking(rows, true)
		if err != nil {
			return nil, err
		}
		b.UserRef = ""
		out = append(out, *b)
	}
	return out, rows.Err()
}

// ManagerDecide confirms, declines or completes a booking of a partner the
// caller manages.
func (s *Service) ManagerDecide(ctx context.Context, userID, bookingID, action string, amountCents *int) (*Booking, error) {
	if err := parse(bookingID); err != nil {
		return nil, err
	}
	var partnerID string
	if err := s.db.QueryRow(ctx, `SELECT partner_id::text FROM bookings WHERE id=$1::uuid`, bookingID).Scan(&partnerID); err != nil {
		return nil, ErrNotFound
	}
	if err := s.manages(ctx, userID, partnerID); err != nil {
		return nil, err
	}
	b, err := s.Decide(ctx, bookingID, action, amountCents)
	if b != nil {
		b.UserRef = ""
	}
	return b, err
}

// ManagerReport is PartnerReport for a manager of that partner.
func (s *Service) ManagerReport(ctx context.Context, userID, partnerID string) (*Report, error) {
	if err := s.manages(ctx, userID, partnerID); err != nil {
		return nil, err
	}
	return s.PartnerReport(ctx, partnerID)
}
