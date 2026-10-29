package chapters

import (
	"encoding/json"
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

// Routes mounts /chapters.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(16<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "chapters:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("read", 120, time.Minute)).Get("/me", h.mine)
	r.With(lim("write", 30, time.Hour)).Put("/me", h.replace)
	r.With(lim("read", 120, time.Minute)).Get("/{ref}", h.forRef)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.Is(err, users.ErrModerationUnavailable):
		middleware.WriteModerationUnavailable(w)
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	default:
		slog.Error("chapters: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	cs, err := h.svc.Mine(r.Context(), middleware.UserID(r.Context()))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"chapters": cs})
}

func (h *Handler) replace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Chapters []Chapter `json:"chapters"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cs, err := h.svc.Replace(r.Context(), middleware.UserID(r.Context()), req.Chapters)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"chapters": cs})
}

func (h *Handler) forRef(w http.ResponseWriter, r *http.Request) {
	cs, err := h.svc.For(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "ref"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"chapters": cs})
}
