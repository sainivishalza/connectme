package communities

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"connectme/internal/social"
	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	svc    *Service
	social *social.Service
}

// Routes mounts /communities.
func Routes(svc *Service, posts *social.Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc, social: posts}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(4<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "communities:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("create", 10, 24*time.Hour)).Post("/", h.create)
	r.With(lim("read", 90, time.Minute)).Get("/nearby", h.nearby)
	r.With(lim("read", 90, time.Minute)).Get("/mine", h.mine)
	r.With(lim("join", 60, time.Hour)).Post("/{id}/join", h.join)
	r.With(lim("join", 60, time.Hour)).Delete("/{id}/membership", h.leave)
	r.With(lim("manage", 30, time.Hour)).Delete("/{id}", h.remove)
	r.With(lim("read", 120, time.Minute)).Get("/{id}/posts", h.feed)
	r.With(lim("post", 60, 24*time.Hour)).Post("/{id}/posts", h.post)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNeedsPlace), errors.Is(err, ErrTooFar), errors.Is(err, ErrNeedsStay):
		middleware.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrNotFound), errors.Is(err, social.ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrForbidden), errors.Is(err, social.ErrForbidden):
		middleware.WriteError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, ErrLimit), errors.Is(err, social.ErrLimit):
		middleware.WriteError(w, http.StatusTooManyRequests, err.Error())
	default:
		slog.Error("communities: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func uid(r *http.Request) string { return middleware.UserID(r.Context()) }

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := h.svc.Create(r.Context(), uid(r), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, c)
}

func (h *Handler) nearby(w http.ResponseWriter, r *http.Request) {
	cs, err := h.svc.Nearby(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"communities": cs})
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	cs, err := h.svc.Mine(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"communities": cs})
}

func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.Join(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, c)
}

func (h *Handler) leave(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Leave(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) feed(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	ps, err := h.social.CommunityFeed(r.Context(), uid(r), chi.URLParam(r, "id"), r.URL.Query().Get("before"), limit)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"posts": ps})
}

func (h *Handler) post(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p, err := h.social.CreateCommunityPost(r.Context(), uid(r), chi.URLParam(r, "id"), req.Body)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, p)
}
