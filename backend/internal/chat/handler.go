package chat

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct{ svc *Service }

// Routes mounts /chat. Message sends are limited to 30 per minute per user.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "chat:"+name, n, w, middleware.ByUser)
	}
	small := middleware.MaxBody(32 << 10)

	r.With(small, lim("keys", 20, time.Hour)).Post("/keys", h.uploadKeys)
	r.With(lim("read", 300, 15*time.Minute)).Get("/keys/count", h.prekeyCount)
	r.With(lim("fetchkeys", 60, time.Hour)).Get("/keys/{matchID}", h.fetchKeys)

	r.With(lim("read", 600, 15*time.Minute)).Get("/matches/{id}/access", h.access)
	r.With(lim("read", 600, 15*time.Minute)).Get("/matches/{id}/messages", h.list)
	r.With(small, lim("send", 30, time.Minute)).Post("/matches/{id}/messages", h.send)
	r.With(lim("read", 600, 15*time.Minute)).Post("/matches/{id}/read", h.read)
	r.With(lim("delete", 20, time.Hour)).Delete("/matches/{id}", h.deleteConversation)

	r.With(lim("fetchkeys", 60, time.Hour)).Get("/groups/{id}/keys", h.groupKeys)
	r.With(small, lim("envelope", 120, time.Hour)).Post("/groups/{id}/envelopes", h.sendEnvelope)
	r.With(lim("read", 300, 15*time.Minute)).Get("/groups/{id}/envelopes", h.fetchEnvelopes)

	r.With(middleware.MaxBody(MaxMediaBytes+4096), lim("media", 30, time.Hour)).Post("/matches/{id}/media", h.upload)
	r.With(lim("read", 600, 15*time.Minute)).Get("/media/{id}", h.download)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve ValidationError
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrTooLarge), errors.As(err, &mbe):
		middleware.WriteError(w, http.StatusRequestEntityTooLarge, "payload too large")
	default:
		slog.Error("chat: internal error", "err", err)
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

func (h *Handler) uploadKeys(w http.ResponseWriter, r *http.Request) {
	var b KeyBundle
	if !decode(w, r, &b) {
		return
	}
	if err := h.svc.UploadKeys(r.Context(), uid(r), b); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) prekeyCount(w http.ResponseWriter, r *http.Request) {
	n, err := h.svc.PreKeyCount(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]int{"one_time_prekeys": n})
}

func (h *Handler) fetchKeys(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.FetchBundle(r.Context(), uid(r), chi.URLParam(r, "matchID"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, b)
}

func (h *Handler) access(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Access(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	msgs, err := h.svc.List(r.Context(), uid(r), chi.URLParam(r, "id"), r.URL.Query().Get("before"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	var req SendRequest
	if !decode(w, r, &req) {
		return
	}
	m, err := h.svc.Send(r.Context(), uid(r), chi.URLParam(r, "id"), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, m)
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	n, at, err := h.svc.MarkRead(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"count": n, "read_at": at})
}

func (h *Handler) deleteConversation(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteConversation(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	id, err := h.svc.UploadMedia(r.Context(), uid(r), chi.URLParam(r, "id"), r.Body)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"media_id": id})
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.OpenMedia(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(b)
}
