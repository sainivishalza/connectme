package safety

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

// Routes mounts /safety. Everything needs a signed-in member except the
// contact's confirmation link, which is protected by its unguessable token.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.MaxBody(2 << 10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "safety:"+name, n, w, middleware.ByUser)
	}
	authed := middleware.Auth(secret)

	r.With(middleware.RateLimit(rdb, "safety:confirm", 20, time.Hour, middleware.ByIP)).Post("/confirm", h.confirm)
	r.With(authed, lim("read", 120, time.Minute)).Get("/contacts", h.contacts)
	r.With(authed, lim("add", 6, 24*time.Hour)).Post("/contacts", h.addContact)
	r.With(authed, lim("write", 30, time.Hour)).Delete("/contacts/{id}", h.removeContact)
	r.With(authed, lim("phone", 10, 24*time.Hour)).Put("/contacts/{id}/phone", h.setPhone)
	r.With(authed, lim("phone-code", 30, 24*time.Hour)).Post("/contacts/{id}/phone/confirm", h.confirmPhone)
	r.With(authed, lim("write", 30, time.Hour)).Delete("/contacts/{id}/phone", h.removePhone)
	r.With(authed, lim("read", 120, time.Minute)).Get("/plans", h.plans)
	r.With(authed, lim("plan", 20, 24*time.Hour)).Post("/plans", h.createPlan)
	r.With(authed, lim("write", 60, time.Hour)).Post("/plans/{id}/{how}", h.finish)
	r.With(authed, lim("write", 60, time.Hour)).Post("/plans/{id}/extend", h.extend)
	return r
}

func (h *Handler) setPhone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.SetPhone(r.Context(), uid(r), chi.URLParam(r, "id"), req.Phone); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) confirmPhone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.ConfirmPhone(r.Context(), uid(r), chi.URLParam(r, "id"), req.Code); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) removePhone(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RemovePhone(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	default:
		slog.Error("safety: internal error", "err", err)
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

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.ConfirmContact(r.Context(), req.Token); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) contacts(w http.ResponseWriter, r *http.Request) {
	cs, err := h.svc.Contacts(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"contacts": cs})
}

func (h *Handler) addContact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := h.svc.AddContact(r.Context(), uid(r), req.Name, req.Email)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, c)
}

func (h *Handler) removeContact(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RemoveContact(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) plans(w http.ResponseWriter, r *http.Request) {
	ps, err := h.svc.Plans(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"plans": ps})
}

func (h *Handler) createPlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ContactID string    `json:"contact_id"`
		Place     string    `json:"place"`
		MeetAt    time.Time `json:"meet_at"`
		AfterMin  int       `json:"checkin_after_min"`
		MatchID   string    `json:"match_id"`
		ShareName bool      `json:"share_name"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, err := h.svc.CreatePlan(r.Context(), uid(r), PlanInput{ContactID: req.ContactID, Place: req.Place, MeetAt: req.MeetAt,
		AfterMin: req.AfterMin, MatchID: req.MatchID, ShareName: req.ShareName})
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, p)
}

func (h *Handler) finish(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Finish(r.Context(), uid(r), chi.URLParam(r, "id"), chi.URLParam(r, "how")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) extend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Minutes int `json:"minutes"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Extend(r.Context(), uid(r), chi.URLParam(r, "id"), req.Minutes); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
