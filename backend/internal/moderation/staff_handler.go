package moderation

import (
	"net/http"
	"strconv"

	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
)

func actor(r *http.Request) Actor {
	return Actor{ID: middleware.UserID(r.Context()), Role: middleware.Role(r.Context())}
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.Stats(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, st)
}

func (h *Handler) viewUser(w http.ResponseWriter, r *http.Request) {
	u, err := h.svc.ViewUser(r.Context(), actor(r), chi.URLParam(r, "ref"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) staffPhoto(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.StaffPhoto(r.Context(), actor(r), chi.URLParam(r, "ref"), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", "inline")
	_, _ = w.Write(b)
}

func (h *Handler) ban(ban bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Reason string `json:"reason"`
		}
		if !decode(w, r, &req) {
			return
		}
		if err := h.svc.SetBan(r.Context(), actor(r), chi.URLParam(r, "ref"), ban, req.Reason); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) audit(w http.ResponseWriter, r *http.Request) {
	limit, offset := 50, 0
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v >= 1 && v <= 200 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 && v <= 100000 {
		offset = v
	}
	res, err := h.svc.Audit(r.Context(), actor(r), limit, offset)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"entries": res})
}

func (h *Handler) removePost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.RemovePost(r.Context(), actor(r), chi.URLParam(r, "id"), req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) removeVideo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.RemoveVideo(r.Context(), actor(r), chi.URLParam(r, "id"), req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) removeForumPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.RemoveForumPost(r.Context(), actor(r), chi.URLParam(r, "kind"), chi.URLParam(r, "id"), req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.Queue(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) approveVideo(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ApproveVideo(r.Context(), actor(r), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) videoTicket(w http.ResponseWriter, r *http.Request) {
	u, err := h.svc.VideoTicket(r.Context(), actor(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"url": u})
}

func (h *Handler) revokeSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.RevokeSessions(r.Context(), actor(r), chi.URLParam(r, "ref"), req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
