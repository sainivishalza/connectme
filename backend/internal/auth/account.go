package auth

import (
	"context"
	"errors"
	"net/http"

	"connectme/pkg/middleware"
	"connectme/pkg/utils"

	"golang.org/x/crypto/bcrypt"
)

// ErrWrongPassword is a wrong *current* password on an authenticated call. It
// is deliberately not a 401: the caller's session is fine, and a 401 would
// make clients try to refresh or sign out.
var ErrWrongPassword = errors.New("current password is incorrect")

// ChangePasswordRequest changes the password of the signed-in user.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword verifies the current password, sets the new one, signs out
// every other session and returns a fresh pair for the calling device. Access
// tokens already issued stay valid until they expire (at most the access TTL).
func (s *Service) ChangePassword(ctx context.Context, userID string, req ChangePasswordRequest) (*TokenPair, error) {
	if err := utils.ValidatePassword(req.NewPassword); err != nil {
		return nil, ValidationError{err.Error()}
	}
	var hash, role string
	var banned bool
	if err := s.db.QueryRow(ctx, `SELECT password_hash, role, is_banned FROM users WHERE id=$1`, userID).
		Scan(&hash, &role, &banned); err != nil {
		return nil, err
	}
	if banned {
		return nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.CurrentPassword)) != nil {
		return nil, ErrWrongPassword
	}
	if req.CurrentPassword == req.NewPassword {
		return nil, ValidationError{"the new password must be different from the current one"}
	}
	nh, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcryptCost)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET password_hash=$1, updated_at=NOW() WHERE id=$2`, string(nh), userID); err != nil {
		return nil, err
	}
	if err := s.dropPushDevices(ctx, userID); err != nil {
		return nil, err
	}
	if err := s.tokens.RevokeAllForUser(ctx, userID); err != nil {
		return nil, err
	}
	return s.issuePair(ctx, userID, role, "")
}

// LogoutAll revokes every refresh-token family of the user, on all devices.
func (s *Service) LogoutAll(ctx context.Context, userID string) error {
	if err := s.tokens.RevokeAllForUser(ctx, userID); err != nil {
		return err
	}
	return s.dropPushDevices(ctx, userID)
}

// dropPushDevices forgets every push token of a user. Signing out elsewhere
// must also stop notifications arriving on those phones; the device that
// stays signed in registers its token again.
func (s *Service) dropPushDevices(ctx context.Context, userID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM push_devices WHERE user_id=$1::uuid`, userID)
	return err
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if !decode(w, r, &req) {
		return
	}
	pair, err := h.svc.ChangePassword(r.Context(), middleware.UserID(r.Context()), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, pair)
}

func (h *Handler) logoutAll(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.LogoutAll(r.Context(), middleware.UserID(r.Context())); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
