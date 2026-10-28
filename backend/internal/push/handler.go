package push

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

// Routes mounts /push.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(4<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "push:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("device", 60, time.Hour)).Put("/devices", h.register)
	r.With(lim("device", 60, time.Hour)).Post("/devices/remove", h.unregister)
	r.With(lim("read", 120, time.Minute)).Get("/prefs", h.prefs)
	r.With(lim("prefs", 60, time.Hour)).Put("/prefs", h.setPrefs)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	if errors.As(err, &ve) {
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
		return
	}
	slog.Error("push: internal error", "err", err)
	middleware.WriteError(w, http.StatusInternalServerError, "internal error")
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

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Register(r.Context(), middleware.UserID(r.Context()), req.Platform, req.Token); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unregister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Unregister(r.Context(), middleware.UserID(r.Context()), req.Token); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) prefs(w http.ResponseWriter, r *http.Request) {
	uid := middleware.UserID(r.Context())
	p, err := h.svc.Prefs(r.Context(), uid)
	if err != nil {
		fail(w, err)
		return
	}
	n, err := h.svc.Devices(r.Context(), uid)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"prefs": p, "devices": n})
}

func (h *Handler) setPrefs(w http.ResponseWriter, r *http.Request) {
	// Fields left out keep their current value, so an older client that
	// does not know the "live" switch cannot turn it off by omission.
	p, err := h.svc.Prefs(r.Context(), middleware.UserID(r.Context()))
	if err != nil {
		fail(w, err)
		return
	}
	if !decode(w, r, &p) {
		return
	}
	if err := h.svc.SetPrefs(r.Context(), middleware.UserID(r.Context()), p); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
