package follows

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct{ svc *Service }

// Routes mounts /follows.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(1<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "follows:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("write", 120, time.Hour)).Post("/{ref}", h.follow)
	r.With(lim("write", 120, time.Hour)).Delete("/{ref}", h.unfollow)
	r.With(lim("read", 120, time.Minute)).Get("/{direction}", h.list)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrSelf):
		middleware.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		slog.Error("follows: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func (h *Handler) follow(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Follow(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "ref")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unfollow(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Unfollow(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "ref")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ps, err := h.svc.List(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "direction"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"profiles": ps})
}
