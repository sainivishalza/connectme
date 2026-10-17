package live

import (
	"context"
	"errors"

	"connectme/internal/wallet"

	"github.com/jackc/pgx/v5"
)

var errBadGift = errors.New("gift_id and a request_id of 8-64 characters are required")

// GiftRequest sends a catalog gift to the host of a live room.
type GiftRequest struct {
	GiftID    string `json:"gift_id"`
	RequestID string `json:"request_id"`
}

// GiftResult reports what was charged and the viewer's remaining balance.
type GiftResult struct {
	Coins     int   `json:"coins"`
	Remaining int64 `json:"remaining"`
	Replayed  bool  `json:"replayed"`
}

// Gift charges the viewer, credits the host 70% and records the platform's
// 30% in one transaction. request_id makes retries free: a repeated request
// returns the original result without charging again.
func (s *Service) Gift(ctx context.Context, userID, roomID string, req GiftRequest) (*GiftResult, error) {
	if req.GiftID == "" || len(req.RequestID) < 8 || len(req.RequestID) > 64 {
		return nil, errBadGift
	}
	hostID, err := s.visible(ctx, userID, roomID)
	if err != nil {
		return nil, err
	}
	if hostID == userID {
		return nil, ErrForbidden // no self-gifting to farm coins
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, userID).Scan(&ok); err != nil || !ok {
		return nil, ErrForbidden
	}
	var price int
	if err := s.db.QueryRow(ctx, `SELECT price_coins FROM gift_catalog WHERE id=$1`, req.GiftID).Scan(&price); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	hostCoins := price * hostSharePct / 100
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Take the payer's row lock BEFORE inserting anything that references the
	// user: the insert's foreign-key share lock would otherwise be held by
	// every concurrent gift, and they would deadlock upgrading to the wallet's
	// FOR UPDATE. With the lock first, one user's gifts simply serialise.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return nil, err
	}
	// Claim the request id: a conflict means a retry.
	tag, err := tx.Exec(ctx, `
		INSERT INTO live_gifts (room_id, from_user, gift_id, coins, host_coins, platform_coins, request_id)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7) ON CONFLICT (from_user, request_id) DO NOTHING`,
		roomID, userID, req.GiftID, price, hostCoins, price-hostCoins, req.RequestID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		bal, err := wallet.Balance(ctx, s.db, userID)
		return &GiftResult{Coins: price, Remaining: bal, Replayed: true}, err
	}
	ref := "live:" + userID + ":" + req.RequestID
	if err := wallet.Spend(ctx, tx, userID, price, "live_gift", ref); err != nil {
		return nil, err // ErrInsufficient rolls the claim back too
	}
	if hostCoins > 0 {
		if _, err := wallet.Grant(ctx, tx, hostID, hostCoins, "live_gift_received", ref+":host"); err != nil {
			return nil, err
		}
	}
	var giftName string
	if err := tx.QueryRow(ctx, `SELECT name FROM gift_catalog WHERE id=$1`, req.GiftID).Scan(&giftName); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO live_events (room_id, user_id, kind, body) VALUES ($1::uuid,$2,'gift',$3)`,
		roomID, userID, "sent a "+giftName); err != nil {
		return nil, err
	}
	bal, err := wallet.Balance(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &GiftResult{Coins: price, Remaining: bal}, nil
}
