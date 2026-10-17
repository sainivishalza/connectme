package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"connectme/internal/entitlements"

	"github.com/jackc/pgx/v5"
)

// ErrNotConfiguredWebhook: with no signing secret every webhook is refused.
var ErrWebhookRejected = errors.New("webhook rejected")

type event struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// HandleWebhook verifies the signature over the raw body, then applies the
// event at most once. The event id is recorded in the same transaction as its
// effects, so a failure rolls both back and Stripe's retry reprocesses it;
// a replay of a processed event is acknowledged and ignored.
func (s *Service) HandleWebhook(ctx context.Context, payload []byte, sigHeader string) error {
	if err := VerifySignature(payload, sigHeader, s.cfg.WebhookSecret, s.now()); err != nil {
		return ErrWebhookRejected
	}
	var ev event
	if err := json.Unmarshal(payload, &ev); err != nil || ev.ID == "" {
		return ErrWebhookRejected
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx, `INSERT INTO stripe_events (id) VALUES ($1) ON CONFLICT DO NOTHING`, ev.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil // already processed
	}
	switch ev.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		err = s.onCheckout(ctx, tx, ev.Data.Object)
	case "customer.subscription.created", "customer.subscription.updated":
		err = s.onSubscription(ctx, tx, ev.Data.Object, false)
	case "customer.subscription.deleted":
		err = s.onSubscription(ctx, tx, ev.Data.Object, true)
	case "charge.refunded":
		err = s.onRefund(ctx, tx, ev.Data.Object)
	default:
		// Unhandled types are acknowledged so Stripe stops retrying.
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) onCheckout(ctx context.Context, tx pgx.Tx, raw json.RawMessage) error {
	var o struct {
		ID            string `json:"id"`
		Mode          string `json:"mode"`
		PaymentStatus string `json:"payment_status"`
		PaymentIntent string `json:"payment_intent"`
		AmountTotal   int64  `json:"amount_total"`
		Currency      string `json:"currency"`
		Metadata      struct {
			UserID    string `json:"user_id"`
			PackID    string `json:"pack_id"`
			BookingID string `json:"booking_id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return err
	}
	if o.Mode != "payment" || o.PaymentStatus != "paid" {
		return nil // subscriptions are handled via subscription.* events
	}
	if o.Metadata.BookingID != "" {
		return s.onBookingPaid(ctx, tx, raw)
	}
	pack, ok := PackByID(o.Metadata.PackID)
	if !ok || o.Metadata.UserID == "" {
		slog.Warn("payments: checkout with unknown pack/user ignored", "session", o.ID)
		return nil
	}
	// Trust the catalog, not the payload: refuse if what was paid is not the price.
	if o.AmountTotal != pack.Cents || o.Currency != Currency {
		slog.Error("payments: amount mismatch, no coins granted", "session", o.ID, "paid", o.AmountTotal, "pack", pack.ID)
		return nil
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id=$1::uuid)`, o.Metadata.UserID).Scan(&exists); err != nil || !exists {
		return err
	}
	// The ledger ref is the session id, so a session can credit at most once
	// even if two different events announce it.
	tag, err := tx.Exec(ctx, `
		INSERT INTO coin_ledger (user_id, delta, reason, ref) VALUES ($1::uuid,$2,'purchase',$3)
		ON CONFLICT (ref) DO NOTHING`, o.Metadata.UserID, pack.Coins, "checkout:"+o.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO payments (user_id, type, amount_cents, currency, stripe_id, status, coins)
		VALUES ($1::uuid,'coins',$2,$3,NULLIF($4,''),'succeeded',$5)
		ON CONFLICT (stripe_id) WHERE stripe_id IS NOT NULL DO NOTHING`,
		o.Metadata.UserID, o.AmountTotal, o.Currency, o.PaymentIntent, pack.Coins)
	return err
}

func (s *Service) onSubscription(ctx context.Context, tx pgx.Tx, raw json.RawMessage, deleted bool) error {
	var o struct {
		ID                string `json:"id"`
		Status            string `json:"status"`
		CancelAtPeriodEnd bool   `json:"cancel_at_period_end"`
		CurrentPeriodEnd  int64  `json:"current_period_end"`
		Items             struct {
			Data []struct {
				CurrentPeriodEnd int64 `json:"current_period_end"`
			} `json:"data"`
		} `json:"items"`
		Metadata struct {
			UserID string `json:"user_id"`
			Tier   string `json:"tier"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return err
	}
	tier := entitlements.Tier(o.Metadata.Tier)
	if o.ID == "" || o.Metadata.UserID == "" || !tier.Valid() {
		slog.Warn("payments: subscription event without valid metadata ignored", "sub", o.ID)
		return nil
	}
	// Newer Stripe API versions carry the period end on the item.
	end := o.CurrentPeriodEnd
	if end == 0 && len(o.Items.Data) > 0 {
		end = o.Items.Data[0].CurrentPeriodEnd
	}
	status := o.Status
	if deleted {
		status = "canceled"
	}
	var expires *time.Time
	if deleted {
		n := s.now()
		expires = &n
	} else if end > 0 {
		t := time.Unix(end, 0).UTC()
		expires = &t
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id=$1::uuid)`, o.Metadata.UserID).Scan(&exists); err != nil || !exists {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO subscriptions (user_id, tier, status, expires_at, stripe_subscription_id, cancel_at_period_end)
		VALUES ($1::uuid,$2,$3,$4,$5,$6)
		ON CONFLICT (stripe_subscription_id) DO UPDATE SET
		  tier=EXCLUDED.tier, status=EXCLUDED.status, expires_at=EXCLUDED.expires_at,
		  cancel_at_period_end=EXCLUDED.cancel_at_period_end, updated_at=NOW()`,
		o.Metadata.UserID, string(tier), status, expires, o.ID, o.CancelAtPeriodEnd); err != nil {
		return fmt.Errorf("upsert subscription: %w", err)
	}
	// Lapsed perk: stop hiding the profile once no active tier remains.
	_, err := tx.Exec(ctx, `
		UPDATE profiles SET invisible=FALSE
		 WHERE user_id=$1::uuid AND invisible AND `+entitlements.ActiveTierSQL("$1::uuid")+` IS NULL`, o.Metadata.UserID)
	return err
}

// onRefund claws back coins for a fully refunded coin purchase. The balance
// may go negative; spending is blocked until it is positive again.
func (s *Service) onRefund(ctx context.Context, tx pgx.Tx, raw json.RawMessage) error {
	var o struct {
		ID             string `json:"id"`
		PaymentIntent  string `json:"payment_intent"`
		Amount         int64  `json:"amount"`
		AmountRefunded int64  `json:"amount_refunded"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return err
	}
	if o.PaymentIntent == "" || o.AmountRefunded < o.Amount || o.Amount == 0 {
		slog.Warn("payments: partial refund noted, no automatic clawback", "charge", o.ID)
		return nil
	}
	if err := s.markBookingRefunded(ctx, tx, o.PaymentIntent); err != nil {
		return err
	}
	var userID string
	var coins int
	err := tx.QueryRow(ctx, `
		UPDATE payments SET status='refunded'
		 WHERE stripe_id=$1 AND type='coins' AND status='succeeded'
		RETURNING user_id::text, COALESCE(coins,0)`, o.PaymentIntent).Scan(&userID, &coins)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if coins <= 0 || userID == "" {
		return nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO coin_ledger (user_id, delta, reason, ref) VALUES ($1::uuid,$2,'refund',$3)
		ON CONFLICT (ref) DO NOTHING`, userID, -coins, "refund:"+o.ID)
	return err
}
