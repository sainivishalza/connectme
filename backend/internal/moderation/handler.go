package moderation

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

// Routes mounts /moderation: block/report for users, review for moderators.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	root := chi.NewRouter()
	// Public: a banned member cannot sign in, so the appeal form proves ownership
	// of the account with email and password instead.
	root.With(middleware.MaxBody(4<<10), middleware.RateLimit(rdb, "mod:appeal", 5, time.Hour, middleware.ByIP)).Post("/appeals", h.submitAppeal)
	r := root.With(middleware.Auth(secret), middleware.MaxBody(32<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "mod:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("block", 60, time.Hour)).Post("/blocks", h.block)
	r.With(lim("block", 60, time.Hour)).Delete("/blocks/{ref}", h.unblock)
	r.With(lim("report", 20, time.Hour)).Post("/reports", h.report)
	r.With(lim("mine", 120, time.Hour)).Get("/reports/mine", h.myReports)

	staff := r.With(auth.RequirePermission(auth.PermReviewReports), lim("staff", 600, time.Hour))
	staff.Get("/reports", h.list)
	staff.Post("/reports/{id}/resolve", h.resolve)
	staff.Get("/stats", h.stats)
	staff.Get("/analytics", h.analytics)
	staff.Get("/members", h.searchMembers)
	staff.Get("/rooms", h.reportedRooms)
	staff.Get("/appeals", h.listAppeals)
	staff.Get("/queue", h.queue)
	staff.Get("/videos/{id}/ticket", h.videoTicket)
	staff.Get("/users/{ref}", h.viewUser)
	staff.Get("/users/{ref}/photos/{id}", h.staffPhoto)
	banners := r.With(auth.RequirePermission(auth.PermBanUser), lim("staff", 600, time.Hour))
	banners.Post("/users/{ref}/ban", h.ban(true))
	banners.Post("/users/{ref}/unban", h.ban(false))
	banners.Post("/appeals/{id}/decide", h.decideAppeal)
	banners.Post("/posts/{id}/remove", h.removePost)
	banners.Post("/videos/{id}/remove", h.removeVideo)
	banners.Post("/videos/{id}/approve", h.approveVideo)
	banners.Post("/forum/{kind}/{id}/remove", h.removeForumPost)
	r.With(auth.RequirePermission(auth.PermViewAuditLog), lim("audit", 120, time.Hour)).Get("/audit", h.audit)
	r.With(auth.RequirePermission(auth.PermViewAuditLog), lim("revoke", 60, time.Hour)).Post("/users/{ref}/revoke-sessions", h.revokeSessions)
	return root
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
	case errors.Is(err, ErrForbidden):
		middleware.WriteError(w, http.StatusForbidden, "forbidden")
	default:
		slog.Error("moderation: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (h *Handler) block(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ref string `json:"ref"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Block(r.Context(), middleware.UserID(r.Context()), req.Ref); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unblock(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Unblock(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "ref")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	var req ReportRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.Report(r.Context(), middleware.UserID(r.Context()), req); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v >= 1 && v <= 100 {
		limit = v
	}
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 && v <= 10000 {
		offset = v
	}
	if status != "open" && status != "reviewing" && status != "actioned" && status != "dismissed" {
		fail(w, users.ValidationError{Msg: "unknown status"})
		return
	}
	res, err := h.svc.List(r.Context(), status, limit, offset)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"reports": res})
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := h.svc.Resolve(r.Context(), middleware.UserID(r.Context()), middleware.Role(r.Context()),
		chi.URLParam(r, "id"), req.Action)
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) myReports(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.MyReports(r.Context(), middleware.UserID(r.Context()))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"reports": out})
}
