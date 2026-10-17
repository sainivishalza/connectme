package users

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register decoders for image.DecodeConfig
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	MaxUploadBytes = 5 << 20
	maxDimension   = 1080
	maxSourceSide  = 12000
	maxSourcePix   = 40_000_000
)

// MediaStore persists processed photo bytes. Keys are server-generated.
type MediaStore interface {
	Put(ctx context.Context, id string, data []byte) error
	Get(ctx context.Context, id string) ([]byte, error)
	Delete(ctx context.Context, id string) error
}

// MediaScanner is the CSAM hash check. It must run before anything is stored.
type MediaScanner interface {
	Clean(ctx context.Context, image []byte) (bool, error)
}

// ErrRejectedMedia is returned when a scan matches; callers answer with a
// generic message and escalate out of band.
var ErrRejectedMedia = errors.New("media rejected")

// LocalStore keeps photos on local disk (dev / single node). Swap for an
// object store behind the same interface in production.
type LocalStore struct{ Dir string }

func (s LocalStore) path(id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil { // blocks path traversal
		return "", ErrNotFound
	}
	return filepath.Join(s.Dir, id+".jpg"), nil
}

func (s LocalStore) Put(_ context.Context, id string, data []byte) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func (s LocalStore) Get(_ context.Context, id string) ([]byte, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

func (s LocalStore) Delete(_ context.Context, id string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// processImage validates, orients, downsizes and re-encodes an upload as JPEG.
// Re-encoding drops every metadata block (EXIF/GPS, XMP, ICC comments).
func processImage(raw []byte) ([]byte, error) {
	switch http.DetectContentType(raw) {
	case "image/jpeg", "image/png":
	default:
		return nil, ValidationError{"photo must be a JPEG or PNG image"}
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, ValidationError{"photo could not be read"}
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxSourceSide || cfg.Height > maxSourceSide ||
		cfg.Width*cfg.Height > maxSourcePix {
		return nil, ValidationError{"photo dimensions are too large"}
	}
	img, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return nil, ValidationError{"photo could not be read"}
	}
	if b := img.Bounds(); b.Dx() > maxDimension || b.Dy() > maxDimension {
		img = imaging.Fit(img, maxDimension, maxDimension, imaging.Lanczos)
	}
	var out bytes.Buffer
	if err := imaging.Encode(&out, img, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// UploadPhoto scans, sanitizes, stores and records a photo; returns its metadata.
func (s *Service) UploadPhoto(ctx context.Context, userID string, raw []byte) (*PhotoMeta, error) {
	if len(raw) == 0 || len(raw) > MaxUploadBytes {
		return nil, ValidationError{"photo must be up to 5MB"}
	}
	clean, err := s.scan.Clean(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("media scan: %w", err)
	}
	if !clean {
		return nil, ErrRejectedMedia
	}
	if err := s.VetImage(ctx, userID, raw); err != nil {
		return nil, err
	}
	data, err := processImage(raw)
	if err != nil {
		return nil, err
	}

	meta := PhotoMeta{ID: uuid.NewString(), CreatedAt: s.now().UTC()}
	if err := s.media.Put(ctx, meta.ID, data); err != nil {
		return nil, err
	}
	if err := s.appendPhoto(ctx, userID, meta); err != nil {
		_ = s.media.Delete(ctx, meta.ID)
		return nil, err
	}
	return &meta, nil
}

func (s *Service) appendPhoto(ctx context.Context, userID string, meta PhotoMeta) error {
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
	if len(photos) >= MaxPhotos {
		return ValidationError{"you can have at most 6 photos"}
	}
	b, _ := json.Marshal(append(photos, meta))
	if _, err := tx.Exec(ctx, `UPDATE profiles SET photos=$1 WHERE user_id=$2`, b, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeletePhoto removes a photo the caller owns.
func (s *Service) DeletePhoto(ctx context.Context, userID, photoID string) error {
	if _, err := uuid.Parse(photoID); err != nil {
		return ErrNotFound
	}
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
	kept, found := photos[:0:0], false
	for _, p := range photos {
		if p.ID == photoID {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return ErrNotFound
	}
	b, _ := json.Marshal(kept)
	if _, err := tx.Exec(ctx, `UPDATE profiles SET photos=$1 WHERE user_id=$2`, b, userID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return s.media.Delete(ctx, photoID)
}

// OpenPhoto returns photo bytes if (and only if) the viewer may see the
// owner's full profile. Everything else is "not found", so incognito photos
// are not even confirmable.
func (s *Service) OpenPhoto(ctx context.Context, viewerID, photoID string) ([]byte, error) {
	if _, err := uuid.Parse(photoID); err != nil {
		return nil, ErrNotFound
	}
	filter, _ := json.Marshal([]map[string]string{{"id": photoID}})
	var ownerID string
	var mode Mode
	err := s.db.QueryRow(ctx, `
		SELECT p.user_id::text, p.mode
		  FROM profiles p JOIN users u ON u.id = p.user_id
		 WHERE p.photos @> $1::jsonb AND u.is_verified AND NOT u.is_banned
		   AND `+notBlocked("p.user_id", "$2::uuid"), string(filter), viewerID).Scan(&ownerID, &mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rel, err := s.relation(ctx, viewerID, ownerID)
	if err != nil {
		return nil, err
	}
	if ResolveVisibility(mode, rel) != VisFull {
		return nil, ErrNotFound
	}
	return s.media.Get(ctx, photoID)
}
