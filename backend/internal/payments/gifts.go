package payments

import (
	"context"
	"errors"
	"time"

	"connectme/internal/users"
	"connectme/internal/wallet"

	"github.com/jackc/pgx/v5"
)

type GiftRequest struct {
	ToRef     string `json:"to_ref"`
	GiftID    string `json:"gift_id"`
	RequestID string `json:"request_id"` // optional; makes retries free
}

type Gift struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Coins int    `json:"price_coins"`
}

type ReceivedGift struct {
	GiftID    string    `json:"gift_id"`
	Name      string    `json:"name"`
	FromRef   string    `json:"from_ref"`
	CreatedAt time.Time `json:"created_at"`
}

// Catalog lists purchasable gifts.
func (s *Service) Catalog(ctx context.Context) ([]Gift, error) {
	rows, err := s.db.Query(ctx, `SELECT id, name, price_coins FROM gift_catalog WHERE active ORDER BY price_coins`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Gift{}
	for rows.Next() {
		var g Gift
		if err := rows.Scan(&g.ID, &g.Name, &g.Coins); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SendGift debits the sender and delivers the gift in one transaction.
// Recipients must be a current match or a guest at the same hotel; a gift
// sent inside a stay is bound to it and deleted with it.
func (s *Service) SendGift(ctx context.Context, userID string, req GiftRequest) error {
	if len(req.RequestID) > 64 {
		return ValidationError{"request_id too long"}
	}
	toID, err := s.users.ResolveVisibleRef(ctx, userID, req.ToRef)
	if errors.Is(err, users.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if toID == userID {
		return ValidationError{"you cannot gift yourself"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var price int
	err = tx.QueryRow(ctx, `SELECT price_coins FROM gift_catalog WHERE id=$1 AND active`, req.GiftID).Scan(&price)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	// Is there a legitimate relationship? (and which stay does it belong to)
	var matched bool
	var checkin *string
	u1, u2 := userID, toID
	if u2 < u1 {
		u1, u2 = u2, u1
	}
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM matches WHERE user1_id=$1 AND user2_id=$2 AND status='matched'),
		       (SELECT a.id::text FROM hotel_checkins a JOIN hotel_checkins b ON b.hotel_id = a.hotel_id
		         WHERE a.user_id=$3 AND b.user_id=$4 AND a.checked_out_at IS NULL AND b.checked_out_at IS NULL)`,
		u1, u2, userID, toID).Scan(&matched, &checkin); err != nil {
		return err
	}
	if !matched && checkin == nil {
		return ErrNotFound
	}

	if req.RequestID != "" {
		var dup bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM gifts_sent WHERE from_user=$1 AND request_id=$2)`,
			userID, req.RequestID).Scan(&dup); err != nil {
			return err
		}
		if dup {
			return nil // a retry: already delivered and charged
		}
	}
	if err := wallet.Spend(ctx, tx, userID, price, "gift", ""); err != nil {
		if errors.Is(err, wallet.ErrInsufficient) {
			return ErrInsufficient
		}
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO gifts_sent (from_user, to_user, gift_id, coins, request_id, checkin_id)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6)`, userID, toID, req.GiftID, price, req.RequestID, checkin); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Received lists gifts the caller has been sent; senders appear only as their
// current public ref.
func (s *Service) Received(ctx context.Context, userID string) ([]ReceivedGift, error) {
	rows, err := s.db.Query(ctx, `
		SELECT g.gift_id, c.name, p.public_ref::text, g.created_at
		  FROM gifts_sent g
		  JOIN gift_catalog c ON c.id = g.gift_id
		  JOIN profiles p ON p.user_id = g.from_user
		 WHERE g.to_user = $1 AND `+users.NotBlockedSQL("g.from_user", "$1::uuid")+`
		 ORDER BY g.created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReceivedGift{}
	for rows.Next() {
		var r ReceivedGift
		if err := rows.Scan(&r.GiftID, &r.Name, &r.FromRef, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
