package payouts

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

// Routes mounts /payouts: a host's own earnings and requests, and the admin
// queue where staff record the transfers they made.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(2<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "payouts:"+name, n, w, middleware.ByUser)
	}
	admin := auth.RequirePermission(auth.PermManageHotels)
	r.With(lim("read", 120, time.Minute)).Get("/mine", h.mine)
	r.With(lim("request", 10, 24*time.Hour)).Post("/request", h.request)
	r.With(admin, lim("admin", 300, time.Hour)).Get("/admin/queue", h.queue)
	r.With(admin, lim("admin", 120, time.Hour)).Post("/admin/{id}/pay", h.pay)
	r.With(admin, lim("admin", 120, time.Hour)).Post("/admin/{id}/reject", h.reject)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	default:
		slog.Error("payouts: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func uid(r *http.Request) string { return middleware.UserID(r.Context()) }

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	e, err := h.svc.Earnings(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, e)
}

func (h *Handler) request(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.Request(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, p)
}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	q, err := h.svc.Queue(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"payouts": q})
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

func (h *Handler) pay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AmountCents int    `json:"amount_cents"`
		Method      string `json:"method"`
		Reference   string `json:"reference"`
		Note        string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := h.svc.Pay(r.Context(), uid(r), middleware.Role(r.Context()), chi.URLParam(r, "id"),
		PayInput{AmountCents: req.AmountCents, Method: req.Method, Reference: req.Reference, Note: req.Note})
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Reject(r.Context(), uid(r), middleware.Role(r.Context()), chi.URLParam(r, "id"), req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
