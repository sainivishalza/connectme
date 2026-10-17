package forums

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"connectme/internal/auth"
	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct{ svc *Service }

// Routes mounts /forums.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(8<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "forums:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("read", 120, time.Minute)).Get("/boards", h.boards)
	r.With(auth.RequirePermission(auth.PermManageHotels), lim("admin", 30, time.Hour)).Post("/boards", h.createBoard)
	r.With(lim("read", 120, time.Minute)).Get("/boards/{slug}/threads", h.threads)
	r.With(lim("thread", 30, 24*time.Hour)).Post("/boards/{slug}/threads", h.createThread)
	r.With(lim("read", 120, time.Minute)).Get("/threads/{id}", h.thread)
	r.With(lim("read", 120, time.Minute)).Get("/threads/{id}/replies", h.replies)
	r.With(lim("reply", 120, 24*time.Hour)).Post("/threads/{id}/replies", h.reply)
	r.With(lim("delete", 60, time.Hour)).Delete("/threads/{id}", h.deleteThread)
	r.With(lim("delete", 60, time.Hour)).Delete("/replies/{id}", h.deleteReply)
	r.With(lim("report", 30, time.Hour)).Post("/{kind}/{id}/report", h.report)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrNotAllowed):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrLimit):
		middleware.WriteError(w, http.StatusTooManyRequests, err.Error())
	default:
		slog.Error("forums: internal error", "err", err)
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

func (h *Handler) boards(w http.ResponseWriter, r *http.Request) {
	bs, err := h.svc.Boards(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"boards": bs})
}

func (h *Handler) createBoard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug        string `json:"slug"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
	}
	if !decode(w, r, &req) {
		return
	}
	b, err := h.svc.CreateBoard(r.Context(), req.Slug, req.Title, req.Description, req.Kind)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, b)
}

func (h *Handler) threads(w http.ResponseWriter, r *http.Request) {
	var before time.Time
	if s := r.URL.Query().Get("before"); s != "" {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		before = t
	}
	ts, err := h.svc.Threads(r.Context(), uid(r), chi.URLParam(r, "slug"), before)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"threads": ts})
}

func (h *Handler) createThread(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	t, err := h.svc.CreateThread(r.Context(), uid(r), chi.URLParam(r, "slug"), req.Title, req.Body)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, t)
}

func (h *Handler) thread(w http.ResponseWriter, r *http.Request) {
	t, err := h.svc.Thread(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, t)
}

func (h *Handler) replies(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	rs, err := h.svc.Replies(r.Context(), uid(r), chi.URLParam(r, "id"), after)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"replies": rs})
}

func (h *Handler) reply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	rp, err := h.svc.Reply(r.Context(), uid(r), chi.URLParam(r, "id"), req.Body)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, rp)
}

func (h *Handler) deleteThread(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteThread(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteReply(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		fail(w, ErrNotFound)
		return
	}
	if err := h.svc.DeleteReply(r.Context(), uid(r), id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	kind := chi.URLParam(r, "kind")
	if kind == "threads" {
		kind = "thread"
	} else if kind == "replies" {
		kind = "reply"
	}
	if err := h.svc.Report(r.Context(), uid(r), kind, chi.URLParam(r, "id"), req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
