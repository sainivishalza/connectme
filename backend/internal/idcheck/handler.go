package idcheck

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"connectme/internal/auth"
	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

const maxImageBytes = 5 << 20

type Handler struct{ svc *Service }

// Routes mounts /idcheck: members start a check and upload two photos;
// moderators review the queue.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "idcheck:"+name, n, w, middleware.ByUser)
	}
	small := middleware.MaxBody(4 << 10)

	r.With(small, lim("read", 120, time.Hour)).Get("/status", h.status)
	r.With(small, lim("start", 10, 24*time.Hour)).Post("/start", h.start)
	r.With(middleware.MaxBody(2*maxImageBytes+(256<<10)), lim("submit", 6, 24*time.Hour)).Post("/submit", h.submit)

	staff := r.With(small, auth.RequirePermission(auth.PermBanUser), lim("staff", 600, time.Hour))
	staff.Get("/queue", h.queue)
	staff.Get("/{id}/image/{which}", h.image)
	staff.Post("/{id}/decision", h.decide)
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
	case errors.Is(err, ErrForbidden):
		middleware.WriteError(w, http.StatusForbidden, "forbidden")
	default:
		slog.Error("idcheck: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func uid(r *http.Request) string { return middleware.UserID(r.Context()) }

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.StatusOf(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.Start(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

// submit takes multipart id_photo and selfie. The bytes, not the declared
// type, decide whether a file is a JPEG or PNG.
func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	read := func(field string) ([]byte, bool) {
		f, _, err := r.FormFile(field)
		if err != nil {
			middleware.WriteError(w, http.StatusBadRequest, field+" is required")
			return nil, false
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
		if err != nil || len(data) == 0 || len(data) > maxImageBytes {
			middleware.WriteError(w, http.StatusBadRequest, field+" must be an image up to 5MB")
			return nil, false
		}
		if t := http.DetectContentType(data); t != "image/jpeg" && t != "image/png" {
			middleware.WriteError(w, http.StatusUnsupportedMediaType, field+" must be JPEG or PNG")
			return nil, false
		}
		return data, true
	}
	idPhoto, ok := read("id_photo")
	if !ok {
		return
	}
	selfie, ok := read("selfie")
	if !ok {
		return
	}
	if err := h.svc.Submit(r.Context(), uid(r), idPhoto, selfie); err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"state": "pending"})
}

func actor(r *http.Request) Actor {
	return Actor{ID: middleware.UserID(r.Context()), Role: middleware.Role(r.Context())}
}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	q, err := h.svc.Queue(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"items": q})
}

func (h *Handler) image(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.Image(r.Context(), actor(r), chi.URLParam(r, "id"), chi.URLParam(r, "which"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(b))
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.Decide(r.Context(), actor(r), chi.URLParam(r, "id"), req.Action, req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
