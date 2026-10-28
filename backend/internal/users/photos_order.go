package users

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxCaptionRunes = 100

// editPhotos reads the caller's photo list under a row lock, lets fn change
// it, and stores the result, so two edits cannot overwrite each other.
func (s *Service) editPhotos(ctx context.Context, userID string, fn func([]PhotoMeta) ([]PhotoMeta, error)) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT photos FROM profiles WHERE user_id=$1 FOR UPDATE`, userID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	photos, err := parsePhotos(raw)
	if err != nil {
		return err
	}
	out, err := fn(photos)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(out)
	if _, err := tx.Exec(ctx, `UPDATE profiles SET photos=$1 WHERE user_id=$2`, b, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReorderPhotos sets the order of the caller's photos; the first is the one
// shown on cards. ids must name every photo exactly once.
func (s *Service) ReorderPhotos(ctx context.Context, userID string, ids []string) error {
	return s.editPhotos(ctx, userID, func(photos []PhotoMeta) ([]PhotoMeta, error) {
		if len(ids) != len(photos) {
			return nil, ValidationError{"list every photo exactly once"}
		}
		byID := make(map[string]PhotoMeta, len(photos))
		for _, p := range photos {
			byID[p.ID] = p
		}
		out := make([]PhotoMeta, 0, len(ids))
		for _, id := range ids {
			p, ok := byID[id]
			if !ok {
				return nil, ValidationError{"list every photo exactly once"}
			}
			delete(byID, id) // a repeated id is no longer found
			out = append(out, p)
		}
		return out, nil
	})
}

// MakeMainPhoto moves one photo to the front.
func (s *Service) MakeMainPhoto(ctx context.Context, userID, photoID string) error {
	if _, err := uuid.Parse(photoID); err != nil {
		return ErrNotFound
	}
	return s.editPhotos(ctx, userID, func(photos []PhotoMeta) ([]PhotoMeta, error) {
		for i, p := range photos {
			if p.ID == photoID {
				out := append([]PhotoMeta{p}, photos[:i]...)
				return append(out, photos[i+1:]...), nil
			}
		}
		return nil, ErrNotFound
	})
}

// SetPhotoCaption sets (or, when empty, clears) the short line under a photo.
// Captions are shown to the same people who can see the photo and pass the
// same text screening as a bio.
func (s *Service) SetPhotoCaption(ctx context.Context, userID, photoID, caption string) error {
	if _, err := uuid.Parse(photoID); err != nil {
		return ErrNotFound
	}
	caption = strings.Join(strings.Fields(caption), " ")
	if utf8.RuneCountInString(caption) > maxCaptionRunes {
		return ValidationError{"a caption can be up to 100 characters"}
	}
	if caption != "" {
		if err := s.VetText(ctx, userID, "photo_caption", caption); err != nil {
			return err
		}
	}
	return s.editPhotos(ctx, userID, func(photos []PhotoMeta) ([]PhotoMeta, error) {
		for i := range photos {
			if photos[i].ID == photoID {
				photos[i].Caption = caption
				return photos, nil
			}
		}
		return nil, ErrNotFound
	})
}
