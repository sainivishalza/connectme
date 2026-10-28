// Package payouts is the ledger for paying live hosts what their gifts earned.
//
// The app does not move money. A host asks to be paid, staff send the money
// outside the app (bank transfer, UPI, PayPal) and record it here with the
// transfer reference. Every gift belongs to at most one payout, a reference can
// be recorded once, and a rejected request frees its gifts again.
package payouts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MinPayoutCoins is the least a host can ask for in one payout, so staff are
// not recording transfers of a few cents.
const MinPayoutCoins = 5000

var ErrNotFound = errors.New("not found")

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// Payout is one request and, once staff decide it, its outcome.
type Payout struct {
	ID          string     `json:"id"`
	Coins       int        `json:"coins"`
	Status      string     `json:"status"`
	AmountCents int        `json:"amount_cents,omitempty"`
	Method      string     `json:"method,omitempty"`
	Reference   string     `json:"reference,omitempty"`
	Note        string     `json:"note,omitempty"`
	RequestedAt time.Time  `json:"requested_at"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
}

// Earnings is a host's own picture: what is waiting, what is being paid, and
// what has been paid.
type Earnings struct {
	Available   int      `json:"available_coins"` // earned and not in any payout
	Requested   int      `json:"requested_coins"` // in a request staff have not decided yet
	Paid        int      `json:"paid_coins"`
	MinPayout   int      `json:"min_payout_coins"`
	CanRequest  bool     `json:"can_request"`
	NeedsIDNote string   `json:"needs_id,omitempty"` // set when ID verification is what is missing
	Payouts     []Payout `json:"payouts"`
}

// Earnings reports the host's gift earnings.
func (s *Service) Earnings(ctx context.Context, hostID string) (*Earnings, error) {
	e := &Earnings{MinPayout: MinPayoutCoins, Payouts: []Payout{}}
	var idOK bool
	if err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(lg.host_coins) FILTER (WHERE lg.payout_id IS NULL), 0)::int,
		       COALESCE(SUM(lg.host_coins) FILTER (WHERE po.status = 'requested'), 0)::int,
		       COALESCE(SUM(lg.host_coins) FILTER (WHERE po.status = 'paid'), 0)::int,
		       (SELECT id_verified_at IS NOT NULL FROM users WHERE id = $1::uuid)
		  FROM live_rooms r
		  JOIN live_gifts lg ON lg.room_id = r.id
		  LEFT JOIN host_payouts po ON po.id = lg.payout_id
		 WHERE r.host_id = $1::uuid`, hostID).Scan(&e.Available, &e.Requested, &e.Paid, &idOK); err != nil {
		return nil, err
	}
	if !idOK {
		e.NeedsIDNote = "Verify your ID to receive payouts."
	}
	e.CanRequest = idOK && e.Requested == 0 && e.Available >= MinPayoutCoins
	rows, err := s.db.Query(ctx, `
		SELECT id::text, coins, status, COALESCE(amount_cents,0), COALESCE(method,''), COALESCE(reference,''), note, requested_at, decided_at
		  FROM host_payouts WHERE host_id = $1::uuid ORDER BY requested_at DESC LIMIT 50`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Payout
		if err := rows.Scan(&p.ID, &p.Coins, &p.Status, &p.AmountCents, &p.Method, &p.Reference, &p.Note, &p.RequestedAt, &p.DecidedAt); err != nil {
			return nil, err
		}
		e.Payouts = append(e.Payouts, p)
	}
	return e, rows.Err()
}

// Request asks for everything the host has earned and not yet been paid. It
// stamps those gifts with the new payout inside one transaction, so the same
// gift cannot be requested twice.
func (s *Service) Request(ctx context.Context, hostID string) (*Payout, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "payout:"+hostID); err != nil {
		return nil, err
	}
	var idOK bool
	if err := tx.QueryRow(ctx, `SELECT id_verified_at IS NOT NULL AND NOT is_banned FROM users WHERE id=$1::uuid`, hostID).Scan(&idOK); err != nil {
		return nil, err
	}
	if !idOK {
		return nil, users.ValidationError{Msg: "verify your ID before asking for a payout"}
	}
	var open int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM host_payouts WHERE host_id=$1::uuid AND status='requested'`, hostID).Scan(&open); err != nil {
		return nil, err
	}
	if open > 0 {
		return nil, users.ValidationError{Msg: "you already have a payout waiting to be paid"}
	}
	var p Payout
	// The row is created first (with the least allowed) so gifts can point at
	// it; the real total is written once the gifts are stamped.
	if err := tx.QueryRow(ctx, `
		INSERT INTO host_payouts (host_id, coins) VALUES ($1::uuid, $2)
		RETURNING id::text, status, requested_at`, hostID, MinPayoutCoins).Scan(&p.ID, &p.Status, &p.RequestedAt); err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `
		WITH stamped AS (
		  UPDATE live_gifts lg SET payout_id = $2::uuid FROM live_rooms r
		   WHERE r.id = lg.room_id AND r.host_id = $1::uuid AND lg.payout_id IS NULL AND lg.host_coins > 0
		  RETURNING lg.host_coins)
		SELECT COALESCE(SUM(host_coins), 0)::int FROM stamped`, hostID, p.ID).Scan(&p.Coins); err != nil {
		return nil, err
	}
	if p.Coins < MinPayoutCoins {
		return nil, users.ValidationError{Msg: "you can ask for a payout from 5000 coins"}
	}
	if _, err := tx.Exec(ctx, `UPDATE host_payouts SET coins=$2 WHERE id=$1::uuid`, p.ID, p.Coins); err != nil {
		return nil, err
	}
	return &p, tx.Commit(ctx)
}

// QueueItem is a request as staff see it: who, how much, and a ref to open them.
type QueueItem struct {
	Payout
	HostRef  string `json:"host_ref"`
	HostName string `json:"host_name"`
}

// Queue lists payouts by status for the admin (default: waiting).
func (s *Service) Queue(ctx context.Context, status string) ([]QueueItem, error) {
	if status != "paid" && status != "rejected" {
		status = "requested"
	}
	rows, err := s.db.Query(ctx, `
		SELECT po.id::text, po.coins, po.status, COALESCE(po.amount_cents,0), COALESCE(po.method,''), COALESCE(po.reference,''), po.note,
		       po.requested_at, po.decided_at, p.public_ref::text, COALESCE(p.display_name,'')
		  FROM host_payouts po JOIN profiles p ON p.user_id = po.host_id
		 WHERE po.status = $1 ORDER BY po.requested_at LIMIT 100`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueItem{}
	for rows.Next() {
		var q QueueItem
		if err := rows.Scan(&q.ID, &q.Coins, &q.Status, &q.AmountCents, &q.Method, &q.Reference, &q.Note, &q.RequestedAt, &q.DecidedAt, &q.HostRef, &q.HostName); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// PayInput is what staff record when they have sent the money.
type PayInput struct {
	AmountCents int
	Method      string
	Reference   string
	Note        string
}

var methods = map[string]bool{"bank_transfer": true, "upi": true, "paypal": true, "other": true}

// Pay records a transfer for a waiting request.
func (s *Service) Pay(ctx context.Context, actorID, actorRole, id string, in PayInput) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	if in.AmountCents <= 0 || in.AmountCents > 100_000_000 || !methods[in.Method] || len(in.Reference) < 3 || len(in.Reference) > 80 || len(in.Note) > 200 {
		return users.ValidationError{Msg: "give the amount sent, the method and the transfer reference"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var host string
	err = tx.QueryRow(ctx, `
		UPDATE host_payouts SET status='paid', amount_cents=$2, method=$3, reference=$4, note=$5, decided_at=NOW(), decided_by=$6::uuid
		 WHERE id=$1::uuid AND status='requested' RETURNING host_id::text`,
		id, in.AmountCents, in.Method, in.Reference, in.Note, actorID).Scan(&host)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		var dup interface{ SQLState() string }
		if errors.As(err, &dup) && dup.SQLState() == "23505" {
			return users.ValidationError{Msg: "that transfer reference was already recorded"}
		}
		return err
	}
	if err := s.audit(ctx, tx, actorID, actorRole, "host_payout_paid", host, map[string]any{"payout": id, "amount_cents": in.AmountCents, "method": in.Method}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Reject turns a request down; its gifts become available again.
func (s *Service) Reject(ctx context.Context, actorID, actorRole, id, reason string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	if len(reason) < 3 || len(reason) > 200 {
		return users.ValidationError{Msg: "give a short reason"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var host string
	err = tx.QueryRow(ctx, `
		UPDATE host_payouts SET status='rejected', note=$2, decided_at=NOW(), decided_by=$3::uuid
		 WHERE id=$1::uuid AND status='requested' RETURNING host_id::text`, id, reason, actorID).Scan(&host)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE live_gifts SET payout_id = NULL WHERE payout_id = $1::uuid`, id); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, actorID, actorRole, "host_payout_rejected", host, map[string]any{"payout": id}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) audit(ctx context.Context, tx pgx.Tx, actorID, role, action, target string, detail map[string]any) error {
	d, _ := json.Marshal(detail)
	_, err := tx.Exec(ctx, `
		INSERT INTO staff_audit_log (actor_id, actor_role, action, target_user_id, detail)
		VALUES ($1::uuid, $2, $3, $4::uuid, $5)`, actorID, role, action, target, d)
	return err
}
