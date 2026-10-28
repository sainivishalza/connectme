package partners

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectme/internal/push"
	"connectme/internal/users"

	"github.com/jackc/pgx/v5"
)

const (
	maxOpenBookings = 3
	bookingsPerDay  = 10
)

type Booking struct {
	ID            string    `json:"id"`
	PartnerID     string    `json:"partner_id"`
	Partner       string    `json:"partner"`
	PartnerKind   string    `json:"partner_kind"`
	PartySize     int       `json:"party_size"`
	RequestedFor  time.Time `json:"requested_for"`
	Note          string    `json:"note"`
	Status        string    `json:"status"`
	AmountCents   *int      `json:"amount_cents,omitempty"`
	PaymentStatus string    `json:"payment_status"`     // unpaid | paid | refunded
	UserRef       string    `json:"user_ref,omitempty"` // staff views only
	Guest         string    `json:"guest,omitempty"`    // partner and staff views: a name only for Open-mode members
	CreatedAt     time.Time `json:"created_at"`
}

type BookingRequest struct {
	PartySize    int       `json:"party_size"`
	RequestedFor time.Time `json:"requested_for"`
	Note         string    `json:"note"`
}

// Book files a booking request with a partner. The partner (via staff)
// confirms or declines it.
func (s *Service) Book(ctx context.Context, userID, partnerID string, in BookingRequest) (*Booking, error) {
	if err := parse(partnerID); err != nil {
		return nil, err
	}
	if err := s.member(ctx, userID); err != nil {
		return nil, err
	}
	now := s.now()
	switch {
	case in.PartySize < 1 || in.PartySize > 12:
		return nil, users.ValidationError{Msg: "party size must be 1-12"}
	case !in.RequestedFor.After(now.Add(time.Hour)):
		return nil, users.ValidationError{Msg: "choose a time at least an hour from now"}
	case in.RequestedFor.After(now.Add(120 * 24 * time.Hour)):
		return nil, users.ValidationError{Msg: "bookings open up to 120 days ahead"}
	}
	note, err := clean(in.Note, 0, 200, "note")
	if err != nil {
		return nil, err
	}
	if note != "" && s.vetter != nil {
		if err := s.vetter.VetText(ctx, userID, "post", note); err != nil {
			return nil, err
		}
	}
	var open, today int
	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status IN ('requested','confirmed') AND requested_for > NOW()),
		       COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '24 hours')
		  FROM bookings WHERE user_id=$1::uuid`, userID).Scan(&open, &today); err != nil {
		return nil, err
	}
	if open >= maxOpenBookings {
		return nil, users.ValidationError{Msg: "you have 3 open bookings; cancel or wait for one to pass"}
	}
	if today >= bookingsPerDay {
		return nil, users.ValidationError{Msg: "too many booking requests today; try again tomorrow"}
	}
	var id string
	err = s.db.QueryRow(ctx, `
		INSERT INTO bookings (partner_id, user_id, party_size, requested_for, note)
		SELECT p.id, $2::uuid, $3, $4, $5 FROM partners p WHERE p.id=$1::uuid AND p.active
		RETURNING id::text`, partnerID, userID, in.PartySize, in.RequestedFor, note).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.booking(ctx, id, userID)
}

const bookingSelect = `
	SELECT b.id::text, b.partner_id::text, p.name, p.kind, b.party_size, b.requested_for, b.note, b.status,
	       b.amount_cents, b.payment_status, COALESCE(pr.public_ref::text,''), b.created_at,
	       COALESCE(pr.display_name,''), COALESCE(pr.mode,'')
	  FROM bookings b JOIN partners p ON p.id = b.partner_id
	  LEFT JOIN profiles pr ON pr.user_id = b.user_id`

func scanBooking(row pgx.Row, staff bool) (*Booking, error) {
	var b Booking
	var name, mode string
	if err := row.Scan(&b.ID, &b.PartnerID, &b.Partner, &b.PartnerKind, &b.PartySize, &b.RequestedFor, &b.Note, &b.Status,
		&b.AmountCents, &b.PaymentStatus, &b.UserRef, &b.CreatedAt, &name, &mode); err != nil {
		return nil, err
	}
	if mode == "open" && name != "" {
		b.Guest = name
	} else {
		b.Guest = "Guest " + strings.ToUpper(b.ID[:4])
	}
	if !staff {
		b.UserRef, b.Guest = "", ""
	}
	return &b, nil
}

func (s *Service) booking(ctx context.Context, id, userID string) (*Booking, error) {
	b, err := scanBooking(s.db.QueryRow(ctx, bookingSelect+` WHERE b.id=$1::uuid AND b.user_id=$2::uuid`, id, userID), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

// MyBookings lists the caller's bookings, newest first.
func (s *Service) MyBookings(ctx context.Context, userID string) ([]Booking, error) {
	rows, err := s.db.Query(ctx, bookingSelect+` WHERE b.user_id=$1::uuid ORDER BY b.created_at DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Booking{}
	for rows.Next() {
		b, err := scanBooking(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// CancelBooking lets the caller withdraw a request or confirmed booking.
func (s *Service) CancelBooking(ctx context.Context, userID, id string) error {
	if err := parse(id); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE bookings SET status='cancelled', updated_at=NOW()
		 WHERE id=$1::uuid AND user_id=$2::uuid AND status IN ('requested','confirmed')`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// StaffBookings lists bookings for staff, optionally by status. A member is
// shown by public ref only.
func (s *Service) StaffBookings(ctx context.Context, status string) ([]Booking, error) {
	rows, err := s.db.Query(ctx, bookingSelect+` WHERE ($1 = '' OR b.status = $1) ORDER BY b.created_at DESC LIMIT 100`, status)
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
		out = append(out, *b)
	}
	return out, rows.Err()
}

// Decide moves a booking forward on the partner's behalf (staff). Confirming
// or completing records the agreed amount; completing fixes the commission.
func (s *Service) Decide(ctx context.Context, id, action string, amountCents *int) (*Booking, error) {
	if err := parse(id); err != nil {
		return nil, err
	}
	var from []string
	var to string
	switch action {
	case "confirm":
		from, to = []string{"requested"}, "confirmed"
	case "decline":
		from, to = []string{"requested"}, "declined"
	case "complete":
		from, to = []string{"confirmed"}, "completed"
	default:
		return nil, users.ValidationError{Msg: "action must be confirm, decline or complete"}
	}
	if amountCents != nil && (*amountCents < 0 || *amountCents > 10_000_000) {
		return nil, users.ValidationError{Msg: "amount must be 0-100000.00"}
	}
	if amountCents != nil {
		var paid bool
		if err := s.db.QueryRow(ctx, `SELECT payment_status <> 'unpaid' FROM bookings WHERE id=$1::uuid`, id).Scan(&paid); err == nil && paid {
			return nil, users.ValidationError{Msg: "the booking is already paid; the amount cannot change"}
		}
	}
	if action == "complete" && amountCents == nil {
		// completing without a new amount keeps the confirmed one, which must exist
		var have *int
		var status string
		if err := s.db.QueryRow(ctx, `SELECT amount_cents, status FROM bookings WHERE id=$1::uuid`, id).Scan(&have, &status); err != nil || status != "confirmed" {
			return nil, ErrNotFound
		}
		if have == nil {
			return nil, users.ValidationError{Msg: "an amount is required to complete a booking"}
		}
	}
	var out string
	err := s.db.QueryRow(ctx, `
		UPDATE bookings b SET status=$3::text,
		       amount_cents = COALESCE($4, b.amount_cents),
		       commission_cents = CASE WHEN $3::text = 'completed'
		           THEN (COALESCE($4, b.amount_cents)::bigint * (SELECT commission_bps FROM partners p WHERE p.id = b.partner_id) / 10000)::int
		           ELSE b.commission_cents END,
		       updated_at = NOW()
		 WHERE b.id=$1::uuid AND b.status = ANY($2::text[]) RETURNING b.id::text`, id, from, to, amountCents).Scan(&out)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	b, err := scanBooking(s.db.QueryRow(ctx, bookingSelect+` WHERE b.id=$1::uuid`, id), true)
	if err == nil && s.notifier != nil {
		var owner string
		if s.db.QueryRow(ctx, `SELECT user_id::text FROM bookings WHERE id=$1::uuid`, id).Scan(&owner) == nil {
			s.notifier.Notify(ctx, owner, push.KindBooking, id)
		}
	}
	return b, err
}

type Report struct {
	Partner           string `json:"partner"`
	CommissionBps     int    `json:"commission_bps"`
	TicketCoins       int    `json:"ticket_coins"`
	TicketCommission  int    `json:"ticket_commission_coins"`
	BookingsCompleted int    `json:"bookings_completed"`
	BookedCents       int    `json:"booked_cents"`
	CommissionCents   int    `json:"commission_cents"`
	CollectedCents    int    `json:"collected_cents"`       // paid through the app, not refunded
	OwedToPartner     int    `json:"owed_to_partner_cents"` // collected on completed bookings, minus commission
	RefundsDue        int    `json:"refunds_due"`           // paid bookings that were cancelled or declined
	SettledCents      int    `json:"settled_cents"`         // already recorded in a payout
	UnsettledCents    int    `json:"unsettled_cents"`       // owed and not yet in a payout
}

// PartnerReport totals what a partner has earned through the platform, for
// settlement outside the app.
func (s *Service) PartnerReport(ctx context.Context, partnerID string) (*Report, error) {
	if err := parse(partnerID); err != nil {
		return nil, err
	}
	var r Report
	err := s.db.QueryRow(ctx, `
		SELECT p.name, p.commission_bps,
		  COALESCE((SELECT SUM(r.coins_paid) FROM event_rsvps r JOIN events e ON e.id = r.event_id WHERE e.partner_id = p.id AND e.status = 'scheduled' AND e.starts_at <= NOW()), 0)::int,
		  COALESCE((SELECT COUNT(*) FROM bookings b WHERE b.partner_id = p.id AND b.status = 'completed'), 0)::int,
		  COALESCE((SELECT SUM(b.amount_cents) FROM bookings b WHERE b.partner_id = p.id AND b.status = 'completed'), 0)::int,
		  COALESCE((SELECT SUM(b.commission_cents) FROM bookings b WHERE b.partner_id = p.id AND b.status = 'completed'), 0)::int,
		  COALESCE((SELECT SUM(b.paid_cents) FROM bookings b WHERE b.partner_id = p.id AND b.payment_status = 'paid'), 0)::int,
		  COALESCE((SELECT SUM(b.paid_cents - COALESCE(b.commission_cents, 0)) FROM bookings b WHERE b.partner_id = p.id AND b.payment_status = 'paid' AND b.status = 'completed'), 0)::int,
		  COALESCE((SELECT COUNT(*) FROM bookings b WHERE b.partner_id = p.id AND b.payment_status = 'paid' AND b.status IN ('cancelled','declined')), 0)::int,
		  COALESCE((SELECT SUM(b.paid_cents - COALESCE(b.commission_cents, 0)) FROM bookings b WHERE b.partner_id = p.id AND b.payment_status = 'paid' AND b.status = 'completed' AND b.payout_id IS NOT NULL), 0)::int
		  FROM partners p WHERE p.id = $1::uuid`, partnerID).
		Scan(&r.Partner, &r.CommissionBps, &r.TicketCoins, &r.BookingsCompleted, &r.BookedCents, &r.CommissionCents, &r.CollectedCents, &r.OwedToPartner, &r.RefundsDue, &r.SettledCents)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	r.TicketCommission = r.TicketCoins * r.CommissionBps / 10000
	r.UnsettledCents = r.OwedToPartner - r.SettledCents
	return &r, err
}
