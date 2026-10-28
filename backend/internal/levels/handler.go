package levels

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

// Routes mounts /levels: the caller's levels and visibility, and leaderboards.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(1<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "levels:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("read", 120, time.Minute)).Get("/me", h.mine)
	r.With(lim("write", 30, time.Hour)).Put("/me/visibility", h.setVisibility)
	r.With(lim("board", 60, time.Minute)).Get("/board/{kind}", h.board)
	return r
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrBadRequest):
		middleware.WriteError(w, http.StatusUnprocessableEntity, "kind must be hosts or supporters and period week, month or all")
	default:
		slog.Error("levels: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	m, err := h.svc.MineFor(r.Context(), middleware.UserID(r.Context()))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, m)
}

func (h *Handler) setVisibility(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ShowCharm     *bool `json:"show_charm"`
		ShowSupporter *bool `json:"show_supporter"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.ShowCharm == nil || req.ShowSupporter == nil {
		middleware.WriteError(w, http.StatusBadRequest, "show_charm and show_supporter are required")
		return
	}
	if err := h.svc.SetVisibility(r.Context(), middleware.UserID(r.Context()), *req.ShowCharm, *req.ShowSupporter); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) board(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "week"
	}
	b, err := h.svc.Leaderboard(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "kind"), period, 20)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, b)
}
