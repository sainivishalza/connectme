package partners

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectme/internal/auth"
	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct{ svc *Service }

// Routes mounts /partners. Staff routes need the hotel-management permission
// (admins), because they speak for a business.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(8<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "partners:"+name, n, w, middleware.ByUser)
	}
	admin := auth.RequirePermission(auth.PermManageHotels)

	r.With(lim("read", 120, time.Minute)).Get("/nearby", h.nearby)
	r.With(lim("read", 120, time.Minute)).Get("/events", h.events)
	r.With(lim("read", 120, time.Minute)).Get("/events/mine", h.myEvents)
	r.With(lim("rsvp", 60, time.Hour)).Post("/events/{id}/rsvp", h.join)
	r.With(lim("rsvp", 60, time.Hour)).Delete("/events/{id}/rsvp", h.leave)
	r.With(lim("book", 30, time.Hour)).Post("/{id}/bookings", h.book)
	r.With(lim("read", 120, time.Minute)).Get("/bookings/mine", h.myBookings)
	r.With(lim("book", 60, time.Hour)).Delete("/bookings/{id}", h.cancelBooking)

	r.With(admin, lim("admin", 120, time.Hour)).Post("/", h.createPartner)
	r.With(admin, lim("admin", 120, time.Hour)).Post("/{id}/events", h.createEvent)
	r.With(admin, lim("admin", 120, time.Hour)).Delete("/events/{id}", h.cancelEvent)
	r.With(admin, lim("admin", 300, time.Hour)).Get("/bookings", h.staffBookings)
	r.With(admin, lim("admin", 300, time.Hour)).Post("/bookings/{id}/decision", h.decide)
	r.With(admin, lim("admin", 300, time.Hour)).Get("/{id}/report", h.report)
	r.With(admin, lim("admin", 60, time.Hour)).Post("/{id}/managers", h.addManager)
	r.With(admin, lim("admin", 300, time.Hour)).Get("/{id}/managers", h.managers)
	r.With(admin, lim("admin", 60, time.Hour)).Delete("/{id}/managers/{ref}", h.removeManager)

	// The partner portal: for managers of a partner, scoped to that partner.
	r.With(lim("manage", 300, time.Hour)).Get("/manage/partners", h.managed)
	r.With(lim("manage", 300, time.Hour)).Get("/manage/partners/{id}/events", h.managerEvents)
	r.With(lim("manage-write", 60, time.Hour)).Post("/manage/partners/{id}/events", h.managerCreateEvent)
	r.With(lim("manage-write", 60, time.Hour)).Delete("/manage/events/{id}", h.managerCancelEvent)
	r.With(lim("manage", 300, time.Hour)).Get("/manage/partners/{id}/bookings", h.managerBookings)
	r.With(lim("manage-write", 300, time.Hour)).Post("/manage/bookings/{id}/decision", h.managerDecide)
	r.With(lim("manage", 300, time.Hour)).Get("/manage/partners/{id}/report", h.managerReport)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrForbidden):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrNeedsPlace):
		middleware.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrFull), errors.Is(err, ErrStarted):
		middleware.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInsufficient):
		middleware.WriteError(w, http.StatusPaymentRequired, err.Error())
	default:
		slog.Error("partners: internal error", "err", err)
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

func (h *Handler) nearby(w http.ResponseWriter, r *http.Request) {
	ps, err := h.svc.Nearby(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"partners": ps})
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	es, err := h.svc.Events(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"events": es})
}

func (h *Handler) myEvents(w http.ResponseWriter, r *http.Request) {
	es, err := h.svc.MyEvents(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"events": es})
}

func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	e, err := h.svc.Join(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, e)
}

func (h *Handler) leave(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Leave(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) book(w http.ResponseWriter, r *http.Request) {
	var req BookingRequest
	if !decode(w, r, &req) {
		return
	}
	b, err := h.svc.Book(r.Context(), uid(r), chi.URLParam(r, "id"), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, b)
}

func (h *Handler) myBookings(w http.ResponseWriter, r *http.Request) {
	bs, err := h.svc.MyBookings(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"bookings": bs})
}

func (h *Handler) cancelBooking(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.CancelBooking(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createPartner(w http.ResponseWriter, r *http.Request) {
	var req CreatePartner
	if !decode(w, r, &req) {
		return
	}
	p, err := h.svc.CreatePartner(r.Context(), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, p)
}

func (h *Handler) createEvent(w http.ResponseWriter, r *http.Request) {
	var req CreateEvent
	if !decode(w, r, &req) {
		return
	}
	e, err := h.svc.CreateEvent(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, e)
}

func (h *Handler) cancelEvent(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.CancelEvent(r.Context(), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) staffBookings(w http.ResponseWriter, r *http.Request) {
	bs, err := h.svc.StaffBookings(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"bookings": bs})
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action      string `json:"action"`
		AmountCents *int   `json:"amount_cents"`
	}
	if !decode(w, r, &req) {
		return
	}
	b, err := h.svc.Decide(r.Context(), chi.URLParam(r, "id"), req.Action, req.AmountCents)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, b)
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	rep, err := h.svc.PartnerReport(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, rep)
}

func (h *Handler) addManager(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	m, err := h.svc.AddManager(r.Context(), chi.URLParam(r, "id"), req.Email)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, m)
}

func (h *Handler) managers(w http.ResponseWriter, r *http.Request) {
	ms, err := h.svc.Managers(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"managers": ms})
}

func (h *Handler) removeManager(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RemoveManager(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "ref")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) managed(w http.ResponseWriter, r *http.Request) {
	ps, err := h.svc.Managed(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"partners": ps})
}

func (h *Handler) managerEvents(w http.ResponseWriter, r *http.Request) {
	es, err := h.svc.ManagerEvents(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"events": es})
}

func (h *Handler) managerCreateEvent(w http.ResponseWriter, r *http.Request) {
	var req CreateEvent
	if !decode(w, r, &req) {
		return
	}
	e, err := h.svc.ManagerCreateEvent(r.Context(), uid(r), chi.URLParam(r, "id"), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, e)
}

func (h *Handler) managerCancelEvent(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ManagerCancelEvent(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) managerBookings(w http.ResponseWriter, r *http.Request) {
	bs, err := h.svc.ManagerBookings(r.Context(), uid(r), chi.URLParam(r, "id"), r.URL.Query().Get("status"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"bookings": bs})
}

func (h *Handler) managerDecide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action      string `json:"action"`
		AmountCents *int   `json:"amount_cents"`
	}
	if !decode(w, r, &req) {
		return
	}
	b, err := h.svc.ManagerDecide(r.Context(), uid(r), chi.URLParam(r, "id"), req.Action, req.AmountCents)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, b)
}

func (h *Handler) managerReport(w http.ResponseWriter, r *http.Request) {
	rep, err := h.svc.ManagerReport(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, rep)
}
