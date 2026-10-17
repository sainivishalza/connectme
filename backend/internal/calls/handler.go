package calls

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

type Handler struct{ svc *Service }

// Routes mounts /calls.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(2<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "calls:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("start", 40, time.Hour)).Post("/", h.start)
	r.With(lim("read", 60, time.Minute)).Get("/incoming", h.incoming)
	r.With(lim("read", 90, time.Minute)).Get("/{id}", h.get)
	r.With(lim("answer", 120, time.Hour)).Post("/{id}/accept", h.accept)
	r.With(lim("answer", 120, time.Hour)).Post("/{id}/decline", h.decline)
	r.With(lim("answer", 120, time.Hour)).Post("/{id}/end", h.end)
	return r
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrNeedsKeep), errors.Is(err, ErrNeedsReveal):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrBusy), errors.Is(err, ErrNotRinging):
		middleware.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("calls: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func uid(r *http.Request) string { return middleware.UserID(r.Context()) }

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MatchID string `json:"match_id"`
		Video   bool   `json:"video"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	st, err := h.svc.Start(r.Context(), uid(r), req.MatchID, req.Video)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, st)
}

func (h *Handler) incoming(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.Incoming(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"call": c})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.Get(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, c)
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.Accept(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) decline(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Decline(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) end(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.End(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
