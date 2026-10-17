package live

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct{ svc *Service }

// Routes mounts /live.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(4<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "live:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("start", 10, 24*time.Hour)).Post("/rooms", h.start)
	r.With(lim("list", 300, 15*time.Minute)).Get("/rooms", h.list)
	r.With(lim("join", 120, time.Hour)).Post("/rooms/{id}/join", h.join)
	r.With(lim("gift", 120, time.Hour)).Post("/rooms/{id}/gift", h.gift)
	r.With(lim("end", 60, time.Hour)).Delete("/rooms/{id}", h.end)
	r.With(lim("moderate", 120, time.Hour)).Post("/rooms/{id}/moderate", h.moderate)
	r.With(lim("chat", 40, time.Minute)).Post("/rooms/{id}/chat", h.chat)
	r.With(lim("events", 90, time.Minute)).Get("/rooms/{id}/events", h.events)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, errBadGift):
		middleware.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrNotAllowed):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrEnded):
		middleware.WriteError(w, http.StatusGone, err.Error())
	case errors.Is(err, ErrInsufficient):
		middleware.WriteError(w, http.StatusPaymentRequired, err.Error())
	default:
		slog.Error("live: internal error", "err", err)
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
		Title string `json:"title"`
	}
	if !decode(w, r, &req) {
		return
	}
	room, join, err := h.svc.Start(r.Context(), uid(r), req.Title)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]any{"room": room, "connect": join})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	rooms, err := h.svc.List(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	j, err := h.svc.Join(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, j)
}

func (h *Handler) gift(w http.ResponseWriter, r *http.Request) {
	var req GiftRequest
	if !decode(w, r, &req) {
		return
	}
	res, err := h.svc.Gift(r.Context(), uid(r), chi.URLParam(r, "id"), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) end(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.End(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Chat(r.Context(), uid(r), chi.URLParam(r, "id"), req.Text); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if after < 0 {
		after = 0
	}
	ev, err := h.svc.Events(r.Context(), uid(r), chi.URLParam(r, "id"), after)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"events": ev})
}

func (h *Handler) moderate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ref    string `json:"ref"`
		Action string `json:"action"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Moderate(r.Context(), uid(r), chi.URLParam(r, "id"), req.Ref, req.Action); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
