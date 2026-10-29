package videos

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct{ svc *Service }

// Routes mounts /videos. The file route is the only unauthenticated one: it
// accepts nothing but a valid, unexpired signature.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.With(middleware.RateLimit(rdb, "videos:file", 1200, 15*time.Minute, middleware.ByIP)).Get("/file/{id}", h.file)
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(secret))
		small := middleware.MaxBody(4 << 10)
		lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
			return middleware.RateLimit(rdb, "videos:"+name, n, w, middleware.ByUser)
		}
		r.With(middleware.MaxBody(MaxBytes+4096), lim("upload", 30, 24*time.Hour)).Post("/", h.upload)
		r.With(small, lim("read", 300, 15*time.Minute)).Get("/feed", h.feed)
		r.With(small, lim("read", 300, 15*time.Minute)).Get("/mine", h.mine)
		r.With(small, lim("read", 300, 15*time.Minute)).Get("/reel/{ref}", h.reel)
		r.With(small, lim("ticket", 600, 15*time.Minute)).Get("/{id}/ticket", h.ticket)
		r.With(small, lim("like", 240, time.Hour)).Post("/{id}/like", h.like)
		r.With(small, lim("report", 20, time.Hour)).Post("/{id}/report", h.report)
		r.With(small, lim("comment", 60, time.Hour)).Post("/{id}/comments", h.comment)
		r.With(small, lim("read", 120, time.Minute)).Get("/{id}/comments", h.comments)
		r.With(small, lim("delete", 120, time.Hour)).Delete("/comments/{cid}", h.removeComment)
		r.With(small, lim("report", 30, time.Hour)).Post("/comments/{cid}/report", h.reportComment)
		r.With(small, lim("delete", 60, time.Hour)).Delete("/{id}", h.remove)
	})
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
	case errors.Is(err, ErrNotOpen):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	default:
		slog.Error("videos: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func uid(r *http.Request) string { return middleware.UserID(r.Context()) }

// upload takes the raw video bytes as the body; kind and caption are query
// parameters (a caption is public text, not a secret).
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, MaxBytes+1))
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "the video could not be read")
		return
	}
	q := r.URL.Query()
	v, err := h.svc.UploadWithSound(r.Context(), uid(r), q.Get("kind"), q.Get("caption"), q.Get("sound"), raw)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]any{"video": v})
}

func (h *Handler) feed(w http.ResponseWriter, r *http.Request) {
	var before time.Time
	if s := r.URL.Query().Get("before"); s != "" {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		before = t
	}
	vs, err := h.svc.Feed(r.Context(), uid(r), before, r.URL.Query().Get("following") == "1")
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"videos": vs})
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	vs, err := h.svc.Mine(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"videos": vs})
}

func (h *Handler) reel(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.Reel(r.Context(), uid(r), chi.URLParam(r, "ref"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"video": v})
}

func (h *Handler) ticket(w http.ResponseWriter, r *http.Request) {
	u, err := h.svc.Ticket(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"url": u})
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
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.Report(r.Context(), uid(r), chi.URLParam(r, "id"), req.Reason); err != nil {
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

// file serves the bytes with range support so players can seek.
func (h *Handler) file(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data, mime, err := h.svc.Open(r.Context(), chi.URLParam(r, "id"), q.Get("exp"), q.Get("sig"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

func (h *Handler) comment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if err := middleware.DecodeStrict(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
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
	if err := h.svc.DeleteComment(r.Context(), uid(r), chi.URLParam(r, "cid")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) reportComment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := middleware.DecodeStrict(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.ReportComment(r.Context(), uid(r), chi.URLParam(r, "cid"), req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
