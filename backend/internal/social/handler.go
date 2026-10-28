package social

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

// Routes mounts /social.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(4<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "social:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("post", 40, 24*time.Hour)).Post("/posts", h.create)
	r.With(lim("read", 120, time.Minute)).Get("/feed", h.feed)
	r.With(lim("read", 60, time.Minute)).Get("/posts/mine", h.mine)
	r.With(lim("write", 120, time.Hour)).Delete("/posts/{id}", h.remove)
	r.With(lim("write", 300, time.Hour)).Post("/posts/{id}/like", h.like)
	r.With(lim("report", 30, time.Hour)).Post("/posts/{id}/report", h.report)
	r.With(lim("comment", 60, time.Hour)).Post("/posts/{id}/comments", h.comment)
	r.With(lim("read", 120, time.Minute)).Get("/posts/{id}/comments", h.comments)
	r.With(lim("write", 120, time.Hour)).Delete("/comments/{id}", h.removeComment)
	r.With(lim("report", 30, time.Hour)).Post("/comments/{id}/report", h.reportComment)
	r.With(lim("claim", 20, time.Hour)).Post("/posts/{id}/claim", h.claim)
	r.With(lim("read", 60, time.Minute)).Get("/posts/{id}/claims", h.claims)
	r.With(lim("answer", 120, time.Hour)).Post("/claims/{id}/accept", h.accept)
	r.With(lim("answer", 120, time.Hour)).Post("/claims/{id}/decline", h.decline)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNeedsPlace), errors.Is(err, ErrNeedsStay):
		middleware.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrForbidden):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrLimit):
		middleware.WriteError(w, http.StatusTooManyRequests, err.Error())
	default:
		slog.Error("social: internal error", "err", err)
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

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, err := h.svc.Create(r.Context(), uid(r), req.Kind, req.Body)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, p)
}

func (h *Handler) feed(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	radius, _ := strconv.Atoi(q.Get("radius_km"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	ps, err := h.svc.Feed(r.Context(), uid(r), q.Get("kind"), radius, q.Get("before"), limit)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"posts": ps})
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	ps, err := h.svc.Mine(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"posts": ps})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) like(w http.ResponseWriter, r *http.Request) {
	liked, n, err := h.svc.Like(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"liked": liked, "likes": n})
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Report(r.Context(), uid(r), chi.URLParam(r, "id"), req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) claim(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Claim(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) claims(w http.ResponseWriter, r *http.Request) {
	cs, err := h.svc.Claims(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"claims": cs})
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	id, err := h.svc.Accept(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"match_id": id})
}

func (h *Handler) decline(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Decline(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) comment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := h.svc.Comment(r.Context(), uid(r), chi.URLParam(r, "id"), req.Body)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, c)
}

func (h *Handler) comments(w http.ResponseWriter, r *http.Request) {
	cs, err := h.svc.Comments(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"comments": cs})
}

func (h *Handler) removeComment(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteComment(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) reportComment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.ReportComment(r.Context(), uid(r), chi.URLParam(r, "id"), req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
