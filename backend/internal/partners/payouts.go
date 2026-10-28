package partners

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/users"

	"github.com/jackc/pgx/v5/pgconn"
)

// Payout is one recorded settlement with a partner.
type Payout struct {
	ID          string    `json:"id"`
	AmountCents int       `json:"amount_cents"`
	Bookings    int       `json:"bookings"`
	Method      string    `json:"method"`
	Reference   string    `json:"reference"`
	Note        string    `json:"note,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// PayoutRequest records that staff have paid the partner what is currently owed.
type PayoutRequest struct {
	Method    string `json:"method"`
	Reference string `json:"reference"`
	Note      string `json:"note"`
}

var payoutMethods = map[string]bool{"bank_transfer": true, "stripe_connect": true, "cash": true, "other": true}

func plain(s string, min, max int, what string) (string, error) {
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

// SettlePartner pays out, on paper, every completed and fully paid booking
// that has not been settled yet: amount = paid - commission. The rows are
// locked and stamped in one transaction, so two staff members clicking at the
// same time cannot both settle the same booking, and a repeated transfer
// reference is refused.
func (s *Service) SettlePartner(ctx context.Context, staffID, partnerID string, in PayoutRequest) (*Payout, error) {
	if err := parse(partnerID); err != nil {
		return nil, err
	}
	if !payoutMethods[in.Method] {
		return nil, users.ValidationError{Msg: "method must be bank_transfer, stripe_connect, cash or other"}
	}
	ref, err := plain(in.Reference, 3, 80, "reference")
	if err != nil {
		return nil, err
	}
	note, err := plain(in.Note, 0, 200, "note")
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM partners WHERE id=$1::uuid)`, partnerID).Scan(&exists); err != nil || !exists {
		if err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	rows, err := tx.Query(ctx, `
		SELECT id, paid_cents - COALESCE(commission_cents, 0)
		  FROM bookings
		 WHERE partner_id=$1::uuid AND payment_status='paid' AND status='completed' AND payout_id IS NULL
		 FOR UPDATE`, partnerID)
	if err != nil {
		return nil, err
	}
	var ids []string
	total := 0
	for rows.Next() {
		var id string
		var owed int
		if err := rows.Scan(&id, &owed); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		total += owed
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 || total <= 0 {
		return nil, users.ValidationError{Msg: "nothing is owed to this partner right now"}
	}
	p := Payout{AmountCents: total, Bookings: len(ids), Method: in.Method, Reference: ref, Note: note}
	err = tx.QueryRow(ctx, `
		INSERT INTO partner_payouts (partner_id, amount_cents, bookings, method, reference, note, created_by)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7::uuid) RETURNING id::text, created_at`,
		partnerID, total, len(ids), in.Method, ref, note, staffID).Scan(&p.ID, &p.CreatedAt)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return nil, users.ValidationError{Msg: "this transfer reference was already recorded for the partner"}
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE bookings SET payout_id=$1::uuid WHERE id = ANY($2::uuid[])`, p.ID, ids); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &p, nil
}

// Payouts lists a partner's recorded settlements, newest first.
func (s *Service) Payouts(ctx context.Context, partnerID string) ([]Payout, error) {
	if err := parse(partnerID); err != nil {
		return nil, err
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM partners WHERE id=$1::uuid)`, partnerID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text, amount_cents, bookings, method, reference, note, created_at
		  FROM partner_payouts WHERE partner_id=$1::uuid ORDER BY created_at DESC LIMIT 100`, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Payout{}
	for rows.Next() {
		var p Payout
		if err := rows.Scan(&p.ID, &p.AmountCents, &p.Bookings, &p.Method, &p.Reference, &p.Note, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ManagerPayouts is Payouts for a manager of that partner.
func (s *Service) ManagerPayouts(ctx context.Context, userID, partnerID string) ([]Payout, error) {
	if err := s.manages(ctx, userID, partnerID); err != nil {
		return nil, err
	}
	return s.Payouts(ctx, partnerID)
}
