package csam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RecordHit handles a positive match. It never receives or stores the image
// bytes beyond hashing them: it writes a csam_hits record (who, what hash,
// when, the provider's reference), suspends the account and opens a report so
// staff are alerted. Nothing is deleted; legal counsel decides reporting and
// retention (docs/CSAM_RUNBOOK.md).
func RecordHit(ctx context.Context, db *pgxpool.Pool, image []byte, v Verdict, meta ScanMeta) error {
	sum := sha256.Sum256(image)
	hash := hex.EncodeToString(sum[:])
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
		INSERT INTO csam_hits (user_id, content_sha256, source, provider_ref)
		VALUES ($1::uuid, $2, $3, $4)`, meta.UserID, hash, meta.Source, v.Reference); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET is_banned = TRUE, updated_at = NOW() WHERE id = $1::uuid`, meta.UserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason, details)
		VALUES (NULL, $1::uuid, 'csam_hit', $2)`, meta.UserID, meta.Source+": scanner match, account suspended"); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	slog.Error("csam: match recorded, account suspended", "user", meta.UserID, "source", meta.Source)
	return nil
}
