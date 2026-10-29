package sponsors

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

// Routes mounts /sponsors. Creating and managing placements needs the
// admin-level hotel/partner management permission.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(8<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "sponsors:"+name, n, w, middleware.ByUser)
	}
	admin := auth.RequirePermission(auth.PermManageHotels)

	r.With(lim("read", 120, time.Minute)).Get("/feed", h.feed)
	r.With(lim("click", 120, time.Hour)).Post("/{id}/click", h.click)

	r.With(admin, lim("admin", 120, time.Hour)).Post("/", h.create)
	r.With(admin, lim("admin", 300, time.Hour)).Get("/", h.list)
	r.With(admin, lim("admin", 120, time.Hour)).Post("/{id}/pause", h.setActive(false))
	r.With(admin, lim("admin", 120, time.Hour)).Post("/{id}/resume", h.setActive(true))
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
		slog.Error("sponsors: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func (h *Handler) feed(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.ForFeed(r.Context(), middleware.UserID(r.Context()))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"placement": p})
}

func (h *Handler) click(w http.ResponseWriter, r *http.Request) {
	link, err := h.svc.Click(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"link_url": link})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	a, err := h.svc.Create(r.Context(), middleware.UserID(r.Context()), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, a)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	as, err := h.svc.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"placements": as})
}

func (h *Handler) setActive(active bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h.svc.SetActive(r.Context(), chi.URLParam(r, "id"), active); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
