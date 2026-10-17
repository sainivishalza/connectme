package groups

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

// Routes mounts /groups.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(16<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "groups:"+name, n, w, middleware.ByUser)
	}
	read := lim("read", 300, 15*time.Minute)
	write := lim("write", 60, time.Hour)

	r.With(lim("create", 10, 24*time.Hour)).Post("/", h.create)
	r.With(read).Get("/", h.mine)
	r.With(lim("browse", 120, 15*time.Minute)).Get("/browse", h.browse)
	r.With(read).Get("/invites", h.invites)
	r.With(write).Post("/invites/{id}/accept", h.respond(true))
	r.With(write).Post("/invites/{id}/decline", h.respond(false))
	r.With(read).Get("/matches", h.matches)
	r.With(read).Get("/matches/{gm}/dates", h.dates)
	r.With(write).Post("/dates/{id}/confirm", h.confirmDate)
	r.With(write).Delete("/dates/{id}", h.cancelDate)

	r.With(read).Get("/{id}", h.get)
	r.With(lim("invite", 30, time.Hour)).Post("/{id}/invite", h.invite)
	r.With(write).Post("/{id}/join", h.join)
	r.With(write).Delete("/{id}/members/me", h.leave)
	r.With(lim("match", 60, time.Hour)).Post("/{id}/match", h.match)
	r.With(write).Post("/{id}/schedule", h.schedule)
	r.With(lim("venue", 30, time.Hour)).Post("/{id}/venue", h.venue)
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve users.ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrNeedsVerified):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrNeedsLocation), errors.Is(err, ErrNotReady):
		middleware.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrGroupFull), errors.Is(err, ErrGroupLocked), errors.Is(err, ErrConflict), errors.Is(err, ErrLimit):
		middleware.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("groups: internal error", "err", err)
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
	var req CreateGroupRequest
	if !decode(w, r, &req) {
		return
	}
	g, err := h.svc.Create(r.Context(), uid(r), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, g)
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	gs, err := h.svc.Mine(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"groups": gs})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	g, err := h.svc.Get(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, g)
}

func (h *Handler) browse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := BrowseFilter{ForGroup: q.Get("for_group"), Activity: q.Get("activity"),
		RadiusKm: 25, MinSize: 2, MaxSize: 6, Limit: 20}
	for key, dst := range map[string]*int{"radius_km": &f.RadiusKm, "min_size": &f.MinSize,
		"max_size": &f.MaxSize, "limit": &f.Limit, "offset": &f.Offset} {
		if v := q.Get(key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				fail(w, ValidationError{Msg: key + " must be a number"})
				return
			}
			*dst = n
		}
	}
	okRadius := false
	for _, o := range users.RadiusOptions() {
		okRadius = okRadius || o == f.RadiusKm
	}
	switch {
	case f.ForGroup == "":
		fail(w, ValidationError{Msg: "for_group is required"})
		return
	case !okRadius:
		fail(w, ValidationError{Msg: "radius_km must be one of 1, 2, 5, 10, 25, 50, 100"})
		return
	case f.Activity != "" && !activities[f.Activity]:
		fail(w, ValidationError{Msg: "unknown activity"})
		return
	case f.MinSize < 2 || f.MaxSize > 6 || f.MinSize > f.MaxSize:
		fail(w, ValidationError{Msg: "size range must be within 2-6"})
		return
	case f.Limit < 1 || f.Limit > 50 || f.Offset < 0 || f.Offset > 1000:
		fail(w, ValidationError{Msg: "limit must be 1-50 and offset 0-1000"})
		return
	}
	gs, err := h.svc.Browse(r.Context(), uid(r), f)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"groups": gs})
}

func (h *Handler) invite(w http.ResponseWriter, r *http.Request) {
	var req InviteRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Invite(r.Context(), uid(r), chi.URLParam(r, "id"), req.Ref); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) invites(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Invites(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"invites": res})
}

func (h *Handler) respond(accept bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h.svc.Respond(r.Context(), uid(r), chi.URLParam(r, "id"), accept); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) join(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Join(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) leave(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Leave(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) match(w http.ResponseWriter, r *http.Request) {
	var req MatchRequest
	if !decode(w, r, &req) {
		return
	}
	res, err := h.svc.Match(r.Context(), uid(r), chi.URLParam(r, "id"), req.TargetGroupID)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) matches(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Matches(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"matches": res})
}

func (h *Handler) schedule(w http.ResponseWriter, r *http.Request) {
	var req ScheduleRequest
	if !decode(w, r, &req) {
		return
	}
	d, err := h.svc.Schedule(r.Context(), uid(r), chi.URLParam(r, "id"), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, d)
}

func (h *Handler) dates(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Dates(r.Context(), uid(r), chi.URLParam(r, "gm"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"dates": res})
}

func (h *Handler) confirmDate(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.ConfirmDate(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, d)
}

func (h *Handler) cancelDate(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.CancelDate(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) venue(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.Suggest(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, s)
}
