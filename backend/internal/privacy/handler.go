package privacy

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

// Routes mounts /privacy. Erasure is irreversible, so it needs the password
// and an explicit confirmation word, and is tightly rate limited.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(4<<10))
	r.With(middleware.RateLimit(rdb, "privacy:erase", 3, time.Hour, middleware.ByUser)).Post("/erase", h.erase)
	r.With(middleware.RateLimit(rdb, "privacy:export", 5, 24*time.Hour, middleware.ByUser)).Get("/export", h.export)
	return r
}

func (h *Handler) erase(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Confirm != "DELETE" {
		middleware.WriteError(w, http.StatusUnprocessableEntity, `type "DELETE" to confirm`)
		return
	}
	switch err := h.svc.Erase(r.Context(), middleware.UserID(r.Context()), req.Password); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrBadPassword):
		middleware.WriteError(w, http.StatusForbidden, "incorrect password")
	case errors.Is(err, ErrStaffAccount):
		middleware.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	default:
		slog.Error("privacy: erasure failed", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "could not complete erasure, please try again")
	}
}

func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	ex, err := h.svc.Export(r.Context(), middleware.UserID(r.Context()))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			middleware.WriteError(w, http.StatusNotFound, "not found")
			return
		}
		slog.Error("privacy: export failed", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="connectme-data-export.json"`)
	middleware.WriteJSON(w, http.StatusOK, ex)
}
