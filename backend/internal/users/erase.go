package users

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// PurgePhotoFiles deletes the stored files of all of a user's photos and their
// voice intro. It is idempotent, so account erasure can be retried safely.
func (s *Service) PurgePhotoFiles(ctx context.Context, userID string) error {
	var raw []byte
	err := s.db.QueryRow(ctx, `SELECT photos FROM profiles WHERE user_id=$1::uuid`, userID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	photos, err := parsePhotos(raw)
	if err != nil {
		return err
	}
	for _, p := range photos {
		if err := s.media.Delete(ctx, p.ID); err != nil {
			return err
		}
	}
	return s.PurgeVoiceFile(ctx, userID)
}
