package auth

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"connectme/pkg/middleware"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxImageBytes = 5 << 20

// CSAMScanner checks an image against known-CSAM hash lists (PhotoDNA / NCMEC
// or equivalent). It MUST run before any image is stored or published.
type CSAMScanner interface {
	// Clean returns false if the image matches a known hash.
	Clean(ctx context.Context, image []byte) (bool, error)
}

// LivenessVerifier compares an ID photo to a live selfie and checks liveness.
type LivenessVerifier interface {
	Verify(ctx context.Context, idPhoto, selfie []byte) (bool, error)
}

// StubCSAMScanner and StubLiveness accept everything. Development only:
// config refuses to start in production with CSAM_PROVIDER=stub.
type StubCSAMScanner struct{}

func (StubCSAMScanner) Clean(context.Context, []byte) (bool, error) { return true, nil }

type StubLiveness struct{}

func (StubLiveness) Verify(context.Context, []byte, []byte) (bool, error) { return true, nil }

type VerificationHandler struct {
	DB       *pgxpool.Pool
	CSAM     CSAMScanner
	Liveness LivenessVerifier
}

// Submit handles POST /auth/verification (multipart: id_photo, selfie).
// Images are held in memory only; just the result timestamp is stored.
func (h *VerificationHandler) Submit(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r.Context())
	idPhoto, ok := h.readImage(w, r, "id_photo")
	if !ok {
		return
	}
	selfie, ok := h.readImage(w, r, "selfie")
	if !ok {
		return
	}
	for _, img := range [][]byte{idPhoto, selfie} {
		clean, err := h.CSAM.Clean(r.Context(), img)
		if err != nil {
			slog.Error("csam scan failed", "err", err)
			middleware.WriteError(w, http.StatusServiceUnavailable, "verification unavailable")
			return
		}
		if !clean {
			// Block, do not store, escalate for legal reporting.
			slog.Error("CSAM hash match: upload blocked", "user_id", userID)
			middleware.WriteError(w, http.StatusUnprocessableEntity, "upload rejected")
			return
		}
	}
	passed, err := h.Liveness.Verify(r.Context(), idPhoto, selfie)
	if err != nil {
		slog.Error("liveness check failed", "err", err)
		middleware.WriteError(w, http.StatusServiceUnavailable, "verification unavailable")
		return
	}
	if !passed {
		middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
		return
	}
	if _, err := h.DB.Exec(r.Context(),
		`UPDATE users SET id_verified_at=$1 WHERE id=$2`, time.Now(), userID); err != nil {
		slog.Error("store verification", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "verified"})
}

func (h *VerificationHandler) readImage(w http.ResponseWriter, r *http.Request, field string) ([]byte, bool) {
	f, _, err := r.FormFile(field)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, field+" is required")
		return nil, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxImageBytes {
		middleware.WriteError(w, http.StatusBadRequest, field+" must be an image up to 5MB")
		return nil, false
	}
	// Trust the bytes, not the client-declared type.
	switch http.DetectContentType(data) {
	case "image/jpeg", "image/png":
		return data, true
	}
	middleware.WriteError(w, http.StatusUnsupportedMediaType, field+" must be JPEG or PNG")
	return nil, false
}
