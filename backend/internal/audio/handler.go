package audio

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

// Routes mounts /audio.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(4<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "audio:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("start", 10, 24*time.Hour)).Post("/rooms", h.start)
	r.With(lim("list", 300, 15*time.Minute)).Get("/rooms", h.list)
	r.With(lim("join", 120, time.Hour)).Post("/rooms/{id}/join", h.join)
	r.With(lim("state", 120, time.Minute)).Get("/rooms/{id}", h.state)
	r.With(lim("hand", 60, time.Minute)).Post("/rooms/{id}/hand", h.hand)
	r.With(lim("role", 120, time.Hour)).Post("/rooms/{id}/members/{member}", h.role)
	r.With(lim("leave", 120, time.Hour)).Post("/rooms/{id}/leave", h.leave)
	r.With(lim("end", 60, time.Hour)).Delete("/rooms/{id}", h.end)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrNotAllowed):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrEnded):
		middleware.WriteError(w, http.StatusGone, err.Error())
	default:
		slog.Error("audio: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func uid(r *http.Request) string { return middleware.UserID(r.Context()) }

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     string `json:"title"`
		Anonymous bool   `json:"anonymous"`
		Topic     string `json:"topic"`
	}
	if !decode(w, r, &req) {
		return
	}
	room, join, err := h.svc.StartWithTopic(r.Context(), uid(r), req.Title, req.Topic, req.Anonymous)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]any{"room": room, "connect": join})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	rooms, err := h.svc.ListTopic(r.Context(), uid(r), r.URL.Query().Get("topic"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"rooms": rooms, "topics": Topics})
}

func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	j, err := h.svc.Join(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, j)
}

func (h *Handler) state(w http.ResponseWriter, r *http.Request) {
	rm, ms, me, err := h.svc.State(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"room": rm, "members": ms, "me": me})
}

func (h *Handler) hand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Up bool `json:"up"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.RaiseHand(r.Context(), uid(r), chi.URLParam(r, "id"), req.Up); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) role(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.SetRole(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "member"), req.Action); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) leave(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Leave(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
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
