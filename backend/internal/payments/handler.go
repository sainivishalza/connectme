package payments

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"connectme/internal/entitlements"
	"connectme/pkg/metrics"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type Handler struct{ svc *Service }

// Routes mounts /payments. The webhook is the only unauthenticated route; it
// is protected by the Stripe signature instead.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()

	// Public by design (the Stripe signature is the proof), so it is limited per
	// address: generous for Stripe, finite for anyone else.
	r.With(middleware.RateLimit(rdb, "payments:webhook", 600, time.Minute, middleware.ByIP), middleware.MaxBody(256<<10)).Post("/webhook", h.webhook)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(secret), middleware.MaxBody(8<<10))
		lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
			return middleware.RateLimit(rdb, "pay:"+name, n, w, middleware.ByUser)
		}
		r.With(lim("read", 300, 15*time.Minute)).Get("/balance", h.balance)
		r.With(lim("read", 300, 15*time.Minute)).Get("/gifts/catalog", h.catalog)
		r.With(lim("read", 300, 15*time.Minute)).Get("/gifts", h.received)
		r.With(lim("checkout", 20, time.Hour)).Post("/subscribe", h.subscribe)
		r.With(lim("checkout", 20, time.Hour)).Post("/coins", h.coins)
		r.With(lim("gift", 60, time.Hour)).Post("/gift", h.gift)
		r.With(lim("cancel", 10, time.Hour)).Post("/cancel", h.cancel)
	})
	return r
}

func fail(w http.ResponseWriter, err error) {
	var ve ValidationError
	switch {
	case errors.As(err, &ve):
		middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrNoSubscription):
		middleware.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrInsufficient):
		middleware.WriteError(w, http.StatusPaymentRequired, err.Error())
	case errors.Is(err, ErrAlreadySubscribed):
		middleware.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrNotConfigured):
		middleware.WriteError(w, http.StatusServiceUnavailable, err.Error())
	default:
		slog.Error("payments: internal error", "err", err)
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

func (h *Handler) balance(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.Account(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, a)
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tier string `json:"tier"`
	}
	if !decode(w, r, &req) {
		return
	}
	id, url, err := h.svc.Subscribe(r.Context(), uid(r), entitlements.Tier(req.Tier))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"session_id": id, "checkout_url": url})
}

func (h *Handler) coins(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PackID string `json:"pack_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	id, url, err := h.svc.BuyCoins(r.Context(), uid(r), req.PackID)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"session_id": id, "checkout_url": url})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Cancel(r.Context(), uid(r)); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) gift(w http.ResponseWriter, r *http.Request) {
	var req GiftRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.SendGift(r.Context(), uid(r), req); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	g, err := h.svc.Catalog(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"gifts": g})
}

func (h *Handler) received(w http.ResponseWriter, r *http.Request) {
	g, err := h.svc.Received(r.Context(), uid(r))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"gifts": g})
}

// webhook reads the raw body (the signature covers exact bytes), verifies it
// and applies the event. Bad signature -> 400 with no detail; processing
// failure -> 500 so Stripe retries.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	if h.svc.cfg.WebhookSecret == "" {
		middleware.WriteError(w, http.StatusServiceUnavailable, "webhooks not configured")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad request")
		return
	}
	switch err := h.svc.HandleWebhook(r.Context(), body, r.Header.Get("Stripe-Signature")); {
	case err == nil:
		metrics.Webhook("ok")
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, ErrWebhookRejected):
		metrics.Webhook("rejected")
		middleware.WriteError(w, http.StatusBadRequest, "invalid webhook")
	default:
		metrics.Webhook("error")
		slog.Error("payments: webhook processing failed", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "processing error")
	}
}
