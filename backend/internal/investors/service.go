package investors

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

var ErrNotFound = errors.New("not found")

const (
	minCents          = 100        // $1
	maxCents          = 10_000_000 // $100,000 per pledge
	maxPendingPerUser = 3
)

type Service struct {
	db          *pgxpool.Pool
	ownerName   string
	paymentNote string
	mode        string
	priceCents  int64 // price of 1% in ModeFixed (and the "starting price" shown in ModeProRata)
}

// NewService returns the ledger. ownerName is shown at the top of the public
// page; paymentNote tells a pledger how to send the money (a bank transfer
// instruction written by the owner).
func NewService(db *pgxpool.Pool, ownerName, paymentNote string) *Service {
	return &Service{db: db, ownerName: ownerName, paymentNote: paymentNote, mode: ModeProRata, priceCents: DefaultPricePerPercentCents}
}

// SetModel chooses how money becomes a share (ModeProRata or ModeFixed) and
// the price of 1%. An unknown mode or a price below $1 keeps the default.
func (s *Service) SetModel(mode string, pricePerPercentCents int64) {
	if mode == ModeFixed || mode == ModeProRata {
		s.mode = mode
	}
	if pricePerPercentCents >= 100 {
		s.priceCents = pricePerPercentCents
	}
}

// share is one investor's holding under the chosen model.
func (s *Service) share(amountCents, totalCents int64) int64 {
	if s.mode == ModeFixed {
		return FixedShareMicro(amountCents, s.priceCents)
	}
	return ShareMicro(amountCents, totalCents)
}

// poolCents is how much money fills the 49% at the fixed price.
func (s *Service) poolCents() int64 { return int64(PoolPercent) * s.priceCents }

// ErrSoldOut means confirming would put more than 49% in investors' hands.
var ErrSoldOut = errors.New("the investor pool is full")

// Holder is one line of the public board. Only a name and a percentage ever
// appear here: never an amount, an email or an account.
type Holder struct {
	Rank    int    `json:"rank"`
	Name    string `json:"name"`
	Percent string `json:"percent"`
	Founder bool   `json:"founder,omitempty"`
}

// Board is the public ownership table.
type Board struct {
	Holders       []Holder `json:"holders"`
	FounderName   string   `json:"founder_name"`
	FounderPct    string   `json:"founder_percent"`
	PoolPct       string   `json:"pool_percent"`
	InvestorCount int      `json:"investor_count"`
	PaymentNote   string   `json:"payment_note,omitempty"`
	// Model is "prorata" or "fixed"; PricePerPercent is dollars for 1%.
	Model           string `json:"model"`
	PricePerPercent string `json:"price_per_percent"`
	// OpenPercent is what is still for sale: always the full pool when
	// pro-rata (new money dilutes instead), the unsold part when fixed.
	OpenPercent string `json:"open_percent"`
}

// Public builds the board.
func (s *Service) Public(ctx context.Context) (*Board, error) {
	rows, err := s.db.Query(ctx, `
		SELECT i.user_id::text, SUM(i.amount_cents)::bigint,
		       BOOL_AND(i.show_name), COALESCE(MAX(p.display_name), '')
		  FROM investments i JOIN profiles p ON p.user_id = i.user_id
		 WHERE i.status = 'confirmed'
		 GROUP BY i.user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type hold struct {
		uid   string
		cents int64
		show  bool
		name  string
	}
	var hs []hold
	var total int64
	for rows.Next() {
		var h hold
		if err := rows.Scan(&h.uid, &h.cents, &h.show, &h.name); err != nil {
			return nil, err
		}
		hs = append(hs, h)
		total += h.cents
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	b := &Board{FounderName: s.ownerName, FounderPct: FormatPercent(int64(FounderPercent) * microPerPercent),
		PoolPct: FormatPercent(PoolMicro), InvestorCount: len(hs), PaymentNote: s.paymentNote,
		Model: s.mode, PricePerPercent: usd(s.priceCents), OpenPercent: FormatPercent(PoolMicro)}
	if s.mode == ModeFixed {
		sold := FixedShareMicro(total, s.priceCents)
		if sold > PoolMicro {
			sold = PoolMicro
		}
		b.OpenPercent = FormatPercent(PoolMicro - sold)
	}
	b.Holders = append(b.Holders, Holder{Rank: 1, Name: s.ownerName, Percent: b.FounderPct, Founder: true})
	// Biggest holding first; ties by id so the order is stable.
	for i := 1; i < len(hs); i++ {
		for j := i; j > 0 && (hs[j].cents > hs[j-1].cents || (hs[j].cents == hs[j-1].cents && hs[j].uid < hs[j-1].uid)); j-- {
			hs[j], hs[j-1] = hs[j-1], hs[j]
		}
	}
	for i, h := range hs {
		name := h.name
		if !h.show || name == "" {
			name = "Anonymous investor"
		}
		b.Holders = append(b.Holders, Holder{Rank: i + 2, Name: name, Percent: FormatPercent(s.share(h.cents, total))})
	}
	return b, nil
}

// Pledge is one of the caller's own pledges.
type Pledge struct {
	ID        string    `json:"id"`
	Amount    string    `json:"amount_usd"`
	Status    string    `json:"status"`
	Reference string    `json:"reference"`
	CreatedAt time.Time `json:"created_at"`
}

// Mine is the caller's pledges and what they currently own.
type Mine struct {
	Pledges     []Pledge `json:"pledges"`
	Percent     string   `json:"percent"`
	PaymentNote string   `json:"payment_note,omitempty"`
}

func usd(cents int64) string {
	whole, frac := cents/100, cents%100
	if frac == 0 {
		return itoa(whole)
	}
	f := itoa(frac)
	if frac < 10 {
		f = "0" + f
	}
	return itoa(whole) + "." + f
}

func itoa(n int64) string { return FormatPercent(n * microPerPercent) }

// Pledge records a pledge. No money moves: the owner confirms it after the
// transfer arrives, and until then it counts for nothing.
func (s *Service) Pledge(ctx context.Context, userID, amount string, showName, acceptTerms bool) (*Pledge, error) {
	if !acceptTerms {
		return nil, users.ValidationError{Msg: "please confirm you have read the notice"}
	}
	cents, ok := ParseUSD(amount)
	if !ok || cents < minCents || cents > maxCents {
		return nil, users.ValidationError{Msg: "enter an amount from 1 to 100000 US dollars, with at most two decimals"}
	}
	var okUser bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1::uuid`, userID).Scan(&okUser); err != nil || !okUser {
		return nil, users.ValidationError{Msg: "verify your account first"}
	}
	if s.mode == ModeFixed {
		var confirmed int64
		if err := s.db.QueryRow(ctx, `SELECT COALESCE(SUM(amount_cents),0)::bigint FROM investments WHERE status='confirmed'`).Scan(&confirmed); err != nil {
			return nil, err
		}
		if left := s.poolCents() - confirmed; cents > left {
			if left <= 0 {
				return nil, users.ValidationError{Msg: "the investor pool is full"}
			}
			return nil, users.ValidationError{Msg: "only $" + usd(left) + " is still open; pledge that much or less"}
		}
	}
	var pending int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM investments WHERE user_id=$1::uuid AND status='pending'`, userID).Scan(&pending); err != nil {
		return nil, err
	}
	if pending >= maxPendingPerUser {
		return nil, users.ValidationError{Msg: "you already have pending pledges; wait for them to be confirmed or cancel one"}
	}
	var p Pledge
	var c int64
	err := s.db.QueryRow(ctx, `
		INSERT INTO investments (user_id, amount_cents, show_name, reference)
		VALUES ($1::uuid, $2, $3, 'INV-' || upper(substr(replace(gen_random_uuid()::text,'-',''), 1, 8)))
		RETURNING id::text, amount_cents, status, reference, created_at`, userID, cents, showName).
		Scan(&p.ID, &c, &p.Status, &p.Reference, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.Amount = usd(c)
	return &p, nil
}

// Mine lists the caller's pledges and their current holding.
func (s *Service) Mine(ctx context.Context, userID string) (*Mine, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, amount_cents, status, reference, created_at FROM investments
		 WHERE user_id=$1::uuid ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := &Mine{Pledges: []Pledge{}, PaymentNote: s.paymentNote}
	for rows.Next() {
		var p Pledge
		var c int64
		if err := rows.Scan(&p.ID, &c, &p.Status, &p.Reference, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Amount = usd(c)
		m.Pledges = append(m.Pledges, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var mine, total int64
	if err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cents) FILTER (WHERE user_id=$1::uuid), 0)::bigint, COALESCE(SUM(amount_cents), 0)::bigint
		  FROM investments WHERE status='confirmed'`, userID).Scan(&mine, &total); err != nil {
		return nil, err
	}
	m.Percent = FormatPercent(s.share(mine, total))
	return m, nil
}

// Cancel withdraws one of the caller's own pledges that is still pending.
func (s *Service) Cancel(ctx context.Context, userID, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `UPDATE investments SET status='cancelled', decided_at=NOW() WHERE id=$1::uuid AND user_id=$2::uuid AND status='pending'`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// QueueItem is one pledge as an admin sees it.
type QueueItem struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Amount    string    `json:"amount_usd"`
	Status    string    `json:"status"`
	Reference string    `json:"reference"`
	CreatedAt time.Time `json:"created_at"`
}

// Queue lists pledges for the owner, newest first. status may be empty (all).
func (s *Service) Queue(ctx context.Context, status string) ([]QueueItem, error) {
	rows, err := s.db.Query(ctx, `
		SELECT i.id::text, COALESCE(p.display_name,''), u.email, i.amount_cents, i.status, i.reference, i.created_at
		  FROM investments i JOIN users u ON u.id = i.user_id LEFT JOIN profiles p ON p.user_id = i.user_id
		 WHERE ($1 = '' OR i.status = $1) ORDER BY i.created_at DESC LIMIT 200`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueItem{}
	for rows.Next() {
		var q QueueItem
		var c int64
		if err := rows.Scan(&q.ID, &q.Name, &q.Email, &c, &q.Status, &q.Reference, &q.CreatedAt); err != nil {
			return nil, err
		}
		q.Amount = usd(c)
		out = append(out, q)
	}
	return out, rows.Err()
}

// Decide confirms (money received) or cancels a pending pledge, and writes the
// staff audit entry in the same transaction.
func (s *Service) Decide(ctx context.Context, actorID, actorRole, id, decision string) error {
	if decision != "confirm" && decision != "cancel" {
		return users.ValidationError{Msg: "decision must be confirm or cancel"}
	}
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	status := map[string]string{"confirm": "confirmed", "cancel": "cancelled"}[decision]
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if s.mode == ModeFixed && decision == "confirm" {
		// One confirmation at a time, so two cannot both squeeze under the cap.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('investments:confirm'))`); err != nil {
			return err
		}
		var amount, confirmed int64
		if err := tx.QueryRow(ctx, `SELECT amount_cents FROM investments WHERE id=$1::uuid AND status='pending'`, id).Scan(&amount); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(amount_cents),0)::bigint FROM investments WHERE status='confirmed'`).Scan(&confirmed); err != nil {
			return err
		}
		if confirmed+amount > s.poolCents() {
			return ErrSoldOut
		}
	}
	var owner string
	var cents int64
	err = tx.QueryRow(ctx, `
		UPDATE investments SET status=$2, decided_at=NOW(), decided_by=$3::uuid
		 WHERE id=$1::uuid AND status='pending' RETURNING user_id::text, amount_cents`, id, status, actorID).Scan(&owner, &cents)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	d, _ := json.Marshal(map[string]any{"investment": id, "amount_usd": usd(cents)})
	if _, err := tx.Exec(ctx, `
		INSERT INTO staff_audit_log (actor_id, actor_role, action, target_user_id, detail)
		VALUES ($1::uuid, $2, $3, $4::uuid, $5)`, actorID, actorRole, "investment_"+decision, owner, d); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
