package investors

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectme/internal/auth"
	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct{ svc *Service }

// Routes mounts /investors. The ownership board is public (no sign-in) and
// shows only names and percentages. Pledging needs a verified member; deciding
// on a pledge needs an admin.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.MaxBody(2 << 10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "investors:"+name, n, w, middleware.ByUser)
	}
	authed := middleware.Auth(secret)
	admin := auth.RequirePermission(auth.PermManageHotels)

	r.With(middleware.RateLimit(rdb, "investors:public", 120, time.Minute, middleware.ByIP)).Get("/public", h.public)
	r.With(authed, lim("write", 20, time.Hour)).Post("/pledges", h.pledge)
	r.With(authed, lim("read", 120, time.Minute)).Get("/mine", h.mine)
	r.With(authed, lim("write", 20, time.Hour)).Delete("/pledges/{id}", h.cancel)
	r.With(authed, admin, lim("admin", 300, time.Hour)).Get("/admin/pledges", h.queue)
	r.With(authed, admin, lim("admin", 120, time.Hour)).Post("/admin/pledges/{id}/{decision}", h.decide)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrSoldOut):
		middleware.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("investors: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func uid(r *http.Request) string { return middleware.UserID(r.Context()) }

func (h *Handler) public(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.Public(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	middleware.WriteJSON(w, http.StatusOK, b)
}

func (h *Handler) pledge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount      string `json:"amount_usd"`
		ShowName    bool   `json:"show_name"`
		AcceptTerms bool   `json:"accept_terms"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p, err := h.svc.Pledge(r.Context(), uid(r), req.Amount, req.ShowName, req.AcceptTerms)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, p)
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	m, err := h.svc.Mine(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, m)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Cancel(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	q, err := h.svc.Queue(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"pledges": q})
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Decide(r.Context(), uid(r), middleware.Role(r.Context()), chi.URLParam(r, "id"), chi.URLParam(r, "decision")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
