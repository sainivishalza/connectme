// Package wallet is the append-only coin ledger used by matching, gifts and
// live streaming. Balances are always derived from the ledger.
package wallet

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrInsufficient = errors.New("insufficient coins")

// Querier is satisfied by *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func Balance(ctx context.Context, q Querier, userID string) (int64, error) {
	var b int64
	err := q.QueryRow(ctx, `SELECT COALESCE(SUM(delta),0)::bigint FROM coin_ledger WHERE user_id=$1`, userID).Scan(&b)
	return b, err
}

// Spend debits coins inside the caller's transaction. Concurrent spends by
// the same user are serialised by a row lock, so the balance can never go
// negative. A non-empty ref makes the operation idempotent.
func Spend(ctx context.Context, tx pgx.Tx, userID string, amount int, reason, ref string) error {
	if amount <= 0 {
		return errors.New("wallet: amount must be positive")
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return err
	}
	bal, err := Balance(ctx, tx, userID)
	if err != nil {
		return err
	}
	if bal < int64(amount) {
		return ErrInsufficient
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO coin_ledger (user_id, delta, reason, ref) VALUES ($1,$2,$3,NULLIF($4,''))
		 ON CONFLICT (ref) DO NOTHING`, userID, -amount, reason, ref)
	return err
}

// Grant credits coins. It returns false when ref was already applied.
func Grant(ctx context.Context, q Querier, userID string, amount int, reason, ref string) (bool, error) {
	if amount <= 0 {
		return false, errors.New("wallet: amount must be positive")
	}
	tag, err := q.Exec(ctx,
		`INSERT INTO coin_ledger (user_id, delta, reason, ref) VALUES ($1,$2,$3,NULLIF($4,''))
		 ON CONFLICT (ref) DO NOTHING`, userID, amount, reason, ref)
	return tag.RowsAffected() == 1, err
}
