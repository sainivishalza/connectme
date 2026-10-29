package geo

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

// Routes mounts /geo.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(16<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "geo:"+name, n, w, middleware.ByUser)
	}

	r.With(auth.RequirePermission(auth.PermManageHotels), lim("admin", 60, time.Hour)).Post("/hotels", h.createHotel)
	r.With(lim("read", 120, 15*time.Minute)).Get("/detect", h.detect)
	r.With(lim("checkin", 10, time.Hour)).Post("/checkin", h.checkIn)
	r.With(lim("checkin", 10, time.Hour)).Post("/checkout", h.checkOut)
	r.With(lim("read", 120, 15*time.Minute)).Get("/checkin", h.current)
	r.With(lim("mood", 60, time.Hour)).Put("/checkin/mood", h.setMood)
	r.With(lim("read", 120, 15*time.Minute)).Get("/hotels/{id}/moods", h.moods)

	r.With(lim("read", 120, 15*time.Minute)).Get("/hotels/{id}/users", h.feed)
	r.With(lim("read", 120, 15*time.Minute)).Get("/hotels/{id}/board", h.board)
	r.With(lim("post", 10, time.Hour)).Post("/hotels/{id}/board", h.post)
	r.With(lim("drink", 10, time.Hour)).Post("/hotels/{id}/drink", h.drink)
	r.With(lim("read", 120, 15*time.Minute)).Get("/hotels/{id}/drinks", h.drinks)
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
	case errors.Is(err, ErrNotCheckedIn), errors.Is(err, ErrNeedsVerified):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrStaleLocation), errors.Is(err, ErrOutsideFence):
		middleware.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrAlreadyCheckedIn), errors.Is(err, ErrAlreadySent):
		middleware.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("geo: internal error", "err", err)
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

func (h *Handler) createHotel(w http.ResponseWriter, r *http.Request) {
	var req CreateHotelRequest
	if !decode(w, r, &req) {
		return
	}
	hotel, err := h.svc.CreateHotel(r.Context(), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, hotel)
}

func (h *Handler) detect(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Detect(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"hotels": res})
}

func (h *Handler) checkIn(w http.ResponseWriter, r *http.Request) {
	var req CheckInRequest
	if !decode(w, r, &req) {
		return
	}
	ci, err := h.svc.CheckIn(r.Context(), uid(r), req.HotelID)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, ci)
}

func (h *Handler) checkOut(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.CheckOut(r.Context(), uid(r)); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) current(w http.ResponseWriter, r *http.Request) {
	ci, err := h.svc.Current(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, ci)
}

func (h *Handler) feed(w http.ResponseWriter, r *http.Request) {
	limit, offset := 20, 0
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v >= 1 && v <= 50 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 && v <= 1000 {
		offset = v
	}
	res, err := h.svc.Feed(r.Context(), uid(r), chi.URLParam(r, "id"), r.URL.Query().Get("mood"), limit, offset)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, FeedResult{Users: res})
}

func (h *Handler) board(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Board(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"posts": res})
}

func (h *Handler) post(w http.ResponseWriter, r *http.Request) {
	var req PostRequest
	if !decode(w, r, &req) {
		return
	}
	p, err := h.svc.PostToBoard(r.Context(), uid(r), chi.URLParam(r, "id"), req.Message)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, p)
}

func (h *Handler) drink(w http.ResponseWriter, r *http.Request) {
	var req DrinkRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.SendDrink(r.Context(), uid(r), chi.URLParam(r, "id"), req.ToRef); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) drinks(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Drinks(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"drinks": res})
}

func (h *Handler) setMood(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mood string `json:"mood"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.SetMood(r.Context(), uid(r), req.Mood); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) moods(w http.ResponseWriter, r *http.Request) {
	m, err := h.svc.Moods(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"moods": m})
}
