package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

const (
	jsonBodyLimit   = 16 << 10
	uploadBodyLimit = 12 << 20
)

type Handler struct {
	svc *Service
}

// Routes mounts the auth endpoints. Every route has an IP rate limit and a
// body-size cap; /verification additionally requires authentication.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()

	limit := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "auth:"+name, n, w, middleware.ByIP)
	}
	body := middleware.MaxBody(jsonBodyLimit)

	r.With(body, limit("register", 10, time.Hour)).Post("/register", h.register)
	r.With(body, limit("login", 20, 15*time.Minute)).Post("/login", h.login)
	r.With(body, limit("refresh", 60, 15*time.Minute)).Post("/refresh", h.refresh)
	r.With(body, limit("logout", 60, 15*time.Minute)).Post("/logout", h.logout)
	r.With(body, limit("forgot", 5, time.Hour)).Post("/forgot-password", h.forgot)
	r.With(body, limit("reset", 10, time.Hour)).Post("/reset-password", h.reset)
	r.With(body, limit("verify", 20, time.Hour)).Post("/verify-email", h.verifyEmail)

	// Authenticated account security. The per-user limit is what protects the
	// current-password check from being used as a guessing oracle.
	r.With(body, middleware.Auth(secret), middleware.RateLimit(rdb, "auth:changepw", 5, time.Hour, middleware.ByUser)).Post("/change-password", h.changePassword)
	r.With(body, middleware.Auth(secret), middleware.RateLimit(rdb, "auth:logoutall", 10, time.Hour, middleware.ByUser)).Post("/logout-all", h.logoutAll)
	return r
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// fail maps service errors to responses. Internal errors are logged, never returned.
func fail(w http.ResponseWriter, err error) {
	var ve ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrInvalidToken), errors.Is(err, ErrTokenReuse):
		middleware.WriteError(w, http.StatusUnauthorized, "invalid credentials or token")
	case errors.Is(err, ErrWrongPassword):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrTooManyAttempts):
		w.Header().Set("Retry-After", "900")
		middleware.WriteError(w, http.StatusTooManyRequests, "too many attempts, try again later")
	default:
		slog.Error("auth: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Register(r.Context(), req); err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{
		"message": "check your email to verify your account"})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if !decode(w, r, &req) {
		return
	}
	res, err := h.svc.Login(WithClientIP(r.Context(), middleware.ClientIP(r)), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if !decode(w, r, &req) {
		return
	}
	pair, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, pair)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) forgot(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.ForgotPassword(r.Context(), req.Email); err != nil {
		slog.Error("auth: forgot-password", "err", err) // never reveal to the caller
	}
	middleware.WriteJSON(w, http.StatusAccepted, map[string]string{
		"message": "if the account exists, an email was sent"})
}

func (h *Handler) reset(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var req VerifyEmailRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.VerifyEmail(r.Context(), req.Token); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
