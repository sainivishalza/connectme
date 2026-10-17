package games

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

// Routes mounts /games.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(2<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "games:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("read", 120, time.Minute)).Get("/prompt", h.prompt)
	r.With(lim("read", 60, time.Minute)).Get("/quiz/sets", h.sets)
	r.With(lim("quiz", 20, time.Hour)).Post("/quiz", h.start)
	r.With(lim("read", 90, time.Minute)).Get("/quiz/match/{id}", h.current)
	r.With(lim("answer", 400, time.Hour)).Post("/quiz/{id}/answer", h.answer)
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
		slog.Error("games: internal error", "err", err)
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

func (h *Handler) prompt(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.Prompt(r.URL.Query().Get("kind"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, p)
}

func (h *Handler) sets(w http.ResponseWriter, _ *http.Request) {
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"sets": h.svc.Sets()})
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MatchID string `json:"match_id"`
		SetID   string `json:"set_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	q, err := h.svc.Start(r.Context(), uid(r), req.MatchID, req.SetID)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, q)
}

func (h *Handler) current(w http.ResponseWriter, r *http.Request) {
	q, err := h.svc.Current(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"quiz": q})
}

func (h *Handler) answer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question int `json:"question"`
		Choice   int `json:"choice"`
	}
	if !decode(w, r, &req) {
		return
	}
	q, err := h.svc.Answer(r.Context(), uid(r), chi.URLParam(r, "id"), req.Question, req.Choice)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, q)
}
