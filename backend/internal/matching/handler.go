package matching

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

// Routes mounts /matching. Swipes carry the spec's limits: 50 per 15 minutes
// and 100 per hour, per user.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(8<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "matching:"+name, n, w, middleware.ByUser)
	}

	r.With(lim("swipe15", 50, 15*time.Minute), lim("swipe60", 100, time.Hour)).Post("/swipe", h.swipe)
	r.With(lim("superlike", 30, time.Hour)).Post("/superlike", h.superlike)
	r.With(lim("boost", 10, time.Hour)).Post("/boost", h.boost)
	r.With(lim("read", 300, 15*time.Minute)).Get("/matches", h.matches)
	r.With(lim("read", 120, 15*time.Minute)).Get("/likes-you", h.likesYou)
	r.With(lim("write", 60, time.Hour)).Delete("/matches/{id}", h.unmatch)
	r.With(lim("write", 60, time.Hour)).Post("/matches/{id}/reveal", h.reveal)
	r.With(lim("rescore", 10, time.Hour)).Post("/matches/{id}/rescore", h.rescore)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrSelf):
		middleware.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrNeedsVerified):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrAlreadySwiped):
		middleware.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInsufficient), errors.Is(err, ErrDailyLimit):
		middleware.WriteError(w, http.StatusPaymentRequired, err.Error())
	default:
		slog.Error("matching: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func (h *Handler) swipe(w http.ResponseWriter, r *http.Request) {
	var req SwipeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Action == ActionSuperLike { // super likes must go through the paid endpoint's limits
		middleware.WriteError(w, http.StatusUnprocessableEntity, "use /matching/superlike")
		return
	}
	res, err := h.svc.Swipe(r.Context(), middleware.UserID(r.Context()), req.TargetRef, req.Action)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) superlike(w http.ResponseWriter, r *http.Request) {
	var req TargetRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	res, err := h.svc.Swipe(r.Context(), middleware.UserID(r.Context()), req.TargetRef, ActionSuperLike)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) boost(w http.ResponseWriter, r *http.Request) {
	until, err := h.svc.Boost(r.Context(), middleware.UserID(r.Context()))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]time.Time{"boosted_until": until})
}

func (h *Handler) matches(w http.ResponseWriter, r *http.Request) {
	limit, offset := 20, 0
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v >= 1 && v <= 50 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 && v <= 1000 {
		offset = v
	}
	res, err := h.svc.Matches(r.Context(), middleware.UserID(r.Context()), limit, offset)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"matches": res})
}

func (h *Handler) unmatch(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Unmatch(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) reveal(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Reveal(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) likesYou(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.LikesYou(r.Context(), middleware.UserID(r.Context()), 50)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) rescore(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Rescore(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, res)
}
