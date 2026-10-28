package users

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct{ svc *Service }

// Routes mounts /users. Everything requires a valid access token.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret))
	limit := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "users:"+name, n, w, middleware.ByUser)
	}
	json := middleware.MaxBody(16 << 10)

	r.With(limit("read", 300, 15*time.Minute)).Get("/me", h.getMe)
	r.With(json, limit("update", 30, 15*time.Minute)).Patch("/me", h.patchMe)
	r.With(json, limit("mode", 10, time.Hour)).Post("/me/mode", h.setMode)
	r.With(json, limit("location", 60, 15*time.Minute)).Put("/me/location", h.setLocation)
	r.With(json, limit("mode", 10, time.Hour)).Post("/me/invisible", h.setInvisible)
	r.With(limit("update", 30, 15*time.Minute)).Post("/me/onboarded", h.onboarded)
	r.With(middleware.MaxBody(MaxUploadBytes+(512<<10)), limit("photo", 20, time.Hour)).Post("/me/photos", h.uploadPhoto)
	r.With(limit("photo", 20, time.Hour)).Delete("/me/photos/{id}", h.deletePhoto)
	r.With(json, limit("photo-edit", 60, time.Hour)).Put("/me/photos/order", h.reorderPhotos)
	r.With(limit("photo-edit", 60, time.Hour)).Put("/me/photos/{id}/main", h.mainPhoto)
	r.With(json, limit("photo-edit", 60, time.Hour)).Put("/me/photos/{id}/caption", h.photoCaption)
	r.With(middleware.MaxBody(MaxVoiceBytes+4096), limit("voice", 10, time.Hour)).Put("/me/voice-intro", h.putVoice)
	r.With(limit("voice", 20, time.Hour)).Delete("/me/voice-intro", h.deleteVoice)
	r.With(json, limit("update", 30, 15*time.Minute)).Put("/me/details", h.putDetails)
	r.With(json, limit("update", 30, 15*time.Minute)).Put("/me/prompts", h.putPrompts)
	r.With(limit("read", 120, 15*time.Minute)).Get("/prompts", h.promptBank)
	r.With(limit("nearby", 60, 15*time.Minute)).Get("/nearby", h.nearby)
	r.With(limit("read", 300, 15*time.Minute)).Get("/{ref}", h.getPublic)
	return r
}

// MediaRoutes serves photos behind the same visibility rules as profiles.
func MediaRoutes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.RateLimit(rdb, "media:read", 600, 15*time.Minute, middleware.ByUser))
	r.Get("/photos/{id}", h.servePhoto)
	r.Get("/voice/{ref}", h.serveVoice)
	return r
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func fail(w http.ResponseWriter, err error) {
	var ve ValidationError
	var needs ErrNeedsTier
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.As(err, &needs):
		middleware.WriteError(w, http.StatusPaymentRequired, err.Error())
	case errors.Is(err, ErrNeedsLocation), errors.Is(err, ErrNeedsVerification):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrRejectedMedia):
		middleware.WriteError(w, http.StatusUnprocessableEntity, "upload rejected")
	default:
		slog.Error("users: internal error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

func (h *Handler) getMe(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetMe(r.Context(), middleware.UserID(r.Context()))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, p)
}

func (h *Handler) patchMe(w http.ResponseWriter, r *http.Request) {
	var req UpdateProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := middleware.UserID(r.Context())
	if err := h.svc.UpdateProfile(r.Context(), uid, req); err != nil {
		fail(w, err)
		return
	}
	h.getMe(w, r)
}

func (h *Handler) setMode(w http.ResponseWriter, r *http.Request) {
	var req SetModeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := h.svc.SetMode(r.Context(), middleware.UserID(r.Context()), req.Mode)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, p)
}

func (h *Handler) setLocation(w http.ResponseWriter, r *http.Request) {
	var req LocationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.SetLocation(r.Context(), middleware.UserID(r.Context()), req.Lat, req.Lng); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	f, _, err := r.FormFile("photo")
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "photo is required")
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxUploadBytes+1))
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "photo could not be read")
		return
	}
	meta, err := h.svc.UploadPhoto(r.Context(), middleware.UserID(r.Context()), raw)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, meta)
}

func (h *Handler) putVoice(w http.ResponseWriter, r *http.Request) {
	secs, _ := strconv.Atoi(r.URL.Query().Get("seconds"))
	raw, err := io.ReadAll(io.LimitReader(r.Body, MaxVoiceBytes+1))
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "recording could not be read")
		return
	}
	if err := h.svc.SetVoiceIntro(r.Context(), middleware.UserID(r.Context()), raw, secs); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteVoice(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteVoiceIntro(r.Context(), middleware.UserID(r.Context())); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) serveVoice(w http.ResponseWriter, r *http.Request) {
	data, mime, err := h.svc.OpenVoiceIntro(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "ref"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "inline")
	_, _ = w.Write(data)
}

func (h *Handler) deletePhoto(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeletePhoto(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) reorderPhotos(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.ReorderPhotos(r.Context(), middleware.UserID(r.Context()), req.IDs); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) mainPhoto(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.MakeMainPhoto(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) photoCaption(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Caption string `json:"caption"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.SetPhotoCaption(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "id"), req.Caption); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getPublic(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetPublic(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "ref"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, p)
}

func (h *Handler) servePhoto(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.OpenPhoto(r.Context(), middleware.UserID(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", "inline")
	_, _ = w.Write(data)
}

func (h *Handler) nearby(w http.ResponseWriter, r *http.Request) {
	f, err := parseNearby(r)
	if err != nil {
		fail(w, err)
		return
	}
	res, err := h.svc.Nearby(r.Context(), middleware.UserID(r.Context()), f)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"results": res})
}

func parseNearby(r *http.Request) (NearbyFilter, error) {
	q := r.URL.Query()
	f := NearbyFilter{RadiusKm: 25, AgeMin: 18, AgeMax: 120, Limit: 20}
	num := func(key string, dst *int) error {
		if v := q.Get(key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return ValidationError{key + " must be a number"}
			}
			*dst = n
		}
		return nil
	}
	for key, dst := range map[string]*int{"radius_km": &f.RadiusKm, "age_min": &f.AgeMin,
		"age_max": &f.AgeMax, "limit": &f.Limit, "offset": &f.Offset} {
		if err := num(key, dst); err != nil {
			return f, err
		}
	}
	okRadius := false
	for _, o := range RadiusOptions() {
		okRadius = okRadius || o == f.RadiusKm
	}
	switch {
	case !okRadius:
		return f, ValidationError{"radius_km must be one of 1, 2, 5, 10, 25, 50, 100"}
	case f.AgeMin < 18 || f.AgeMax > 120 || f.AgeMin > f.AgeMax:
		return f, ValidationError{"age range must be within 18-120"}
	case f.Limit < 1 || f.Limit > 50 || f.Offset < 0 || f.Offset > 1000:
		return f, ValidationError{"limit must be 1-50 and offset 0-1000"}
	}
	if g := q.Get("gender"); g != "" {
		if !genders[g] {
			return f, ValidationError{"invalid gender"}
		}
		f.Gender = g
	}
	if m := Mode(q.Get("mode")); m != "" {
		if m != ModeOpen && m != ModeIncognito {
			return f, ValidationError{"mode must be open or incognito"}
		}
		f.Mode = m
	}
	for _, c := range []struct {
		key     string
		allowed map[string]bool
		dst     *string
	}{{"goal", goals, &f.Goal}, {"kids", kidsOpts, &f.Kids}, {"drinking", drinkOpts, &f.Drinking},
		{"smoking", drinkOpts, &f.Smoking}, {"exercise", exerOpts, &f.Exercise}, {"language", langCodes, &f.Language}} {
		if v := q.Get(c.key); v != "" {
			if !c.allowed[v] {
				return f, ValidationError{"invalid " + c.key}
			}
			*c.dst = v
		}
	}
	f.VerifiedOnly = q.Get("verified") == "true"
	if t := q.Get("tags"); t != "" {
		tags, err := normalizeTags(strings.Split(t, ","))
		if err != nil {
			return f, err
		}
		f.Tags = tags
	}
	return f, nil
}

func (h *Handler) putDetails(w http.ResponseWriter, r *http.Request) {
	var req DetailsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	d, err := h.svc.SetDetails(r.Context(), middleware.UserID(r.Context()), req)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, d)
}

func (h *Handler) putPrompts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompts []PromptIn `json:"prompts"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := h.svc.SetPrompts(r.Context(), middleware.UserID(r.Context()), req.Prompts)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"prompts": out})
}

func (h *Handler) promptBank(w http.ResponseWriter, _ *http.Request) {
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"prompts": PromptBank})
}

func (h *Handler) setInvisible(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := h.svc.SetInvisible(r.Context(), middleware.UserID(r.Context()), req.Enabled)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, p)
}

func (h *Handler) onboarded(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.MarkOnboarded(r.Context(), middleware.UserID(r.Context())); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
