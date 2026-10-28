package payments

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// MinBookingCents is Stripe Checkout's floor for a card payment.
const MinBookingCents = 50

var ErrNotPayable = ValidationError{"this booking cannot be paid right now"}

// PayBooking starts a Checkout Session for a confirmed booking the caller
// made. The price is the amount staff recorded at confirmation.
func (s *Service) PayBooking(ctx context.Context, userID, bookingID string) (id, url string, err error) {
	if err := s.configured(); err != nil {
		return "", "", err
	}
	if err := s.requireVerified(ctx, userID); err != nil {
		return "", "", err
	}
	var name string
	var cents *int
	var status, pay string
	err = s.db.QueryRow(ctx, `
		SELECT p.name, b.amount_cents, b.status, b.payment_status
		  FROM bookings b JOIN partners p ON p.id = b.partner_id
		 WHERE b.id = $1::uuid AND b.user_id = $2::uuid`, bookingID, userID).Scan(&name, &cents, &status, &pay)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	if status != "confirmed" || pay != "unpaid" || cents == nil || int64(*cents) < MinBookingCents {
		return "", "", ErrNotPayable
	}
	return s.stripe.CreateCheckout(ctx, CheckoutParams{
		Mode: "payment", ProductName: "Booking: " + name, Cents: int64(*cents),
		UserID: userID, Metadata: map[string]string{"user_id": userID, "booking_id": bookingID},
		SuccessURL: s.cfg.SuccessURL, CancelURL: s.cfg.CancelURL,
	})
}

// RefundBooking asks Stripe to refund a paid booking in full (admin only; the
// handler enforces it). The booking is marked refunded when Stripe's webhook
// confirms, so the app never claims a refund that did not happen.
func (s *Service) RefundBooking(ctx context.Context, bookingID string) error {
	if err := s.configured(); err != nil {
		return err
	}
	var intent *string
	var pay string
	var settled bool
	err := s.db.QueryRow(ctx, `SELECT payment_intent, payment_status, payout_id IS NOT NULL FROM bookings WHERE id=$1::uuid`, bookingID).Scan(&intent, &pay, &settled)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if pay != "paid" || intent == nil || *intent == "" {
		return ValidationError{"only a paid booking can be refunded"}
	}
	if settled {
		return ValidationError{"this booking was already paid out to the partner; recover the money from the partner first"}
	}
	return s.stripe.Refund(ctx, *intent)
}

type bookingCheckout struct {
	ID            string `json:"id"`
	PaymentIntent string `json:"payment_intent"`
	AmountTotal   int64  `json:"amount_total"`
	Currency      string `json:"currency"`
	Metadata      struct {
		UserID    string `json:"user_id"`
		BookingID string `json:"booking_id"`
	} `json:"metadata"`
}

// onBookingPaid applies a paid Checkout Session for a booking. It trusts the
// database, not the payload: the booking must belong to the named user and the
// paid amount must equal the recorded one. A mismatch is logged loudly and
// changes nothing (the money then needs a manual refund).
func (s *Service) onBookingPaid(ctx context.Context, tx pgx.Tx, raw json.RawMessage) error {
	var o bookingCheckout
	if err := json.Unmarshal(raw, &o); err != nil {
		return err
	}
	var owner, pay string
	var amount *int
	err := tx.QueryRow(ctx, `SELECT user_id::text, payment_status, amount_cents FROM bookings WHERE id=$1::uuid FOR UPDATE`, o.Metadata.BookingID).
		Scan(&owner, &pay, &amount)
	if errors.Is(err, pgx.ErrNoRows) {
		slog.Error("payments: paid checkout for an unknown booking, refund needed", "session", o.ID, "booking", o.Metadata.BookingID)
		return nil
	}
	if err != nil {
		return err
	}
	if owner != o.Metadata.UserID || amount == nil || int64(*amount) != o.AmountTotal || o.Currency != Currency {
		slog.Error("payments: booking payment does not match the booking, refund needed", "session", o.ID, "booking", o.Metadata.BookingID, "paid", o.AmountTotal)
		return nil
	}
	if pay != "unpaid" {
		return nil // already applied (a second session for the same booking needs a refund)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE bookings SET payment_status='paid', paid_cents=$2, paid_at=NOW(), payment_intent=NULLIF($3,''), updated_at=NOW()
		 WHERE id=$1::uuid`, o.Metadata.BookingID, o.AmountTotal, o.PaymentIntent); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO payments (user_id, type, amount_cents, currency, stripe_id, status)
		VALUES ($1::uuid,'booking',$2,$3,NULLIF($4,''),'succeeded')
		ON CONFLICT (stripe_id) WHERE stripe_id IS NOT NULL DO NOTHING`, owner, o.AmountTotal, o.Currency, o.PaymentIntent)
	return err
}

// markBookingRefunded records a full refund Stripe has confirmed.
func (s *Service) markBookingRefunded(ctx context.Context, tx pgx.Tx, paymentIntent string) error {
	if _, err := tx.Exec(ctx, `UPDATE bookings SET payment_status='refunded', updated_at=NOW() WHERE payment_intent=$1 AND payment_status='paid'`, paymentIntent); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE payments SET status='refunded' WHERE stripe_id=$1 AND type='booking' AND status='succeeded'`, paymentIntent)
	return err
}
