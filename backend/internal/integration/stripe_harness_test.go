package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"connectme/internal/payments"
	"connectme/internal/server"
	"connectme/internal/wallet"
)

const whSecret = "whsec_integration_test_secret"

// stubStripe is a fake Stripe REST API that records what the server sends.
type stubStripe struct {
	mu       sync.Mutex
	sessions []url.Values
	cancels  []string
	refunds  []string
	srv      *httptest.Server
}

func (s *stubStripe) last() url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) == 0 {
		return nil
	}
	return s.sessions[len(s.sessions)-1]
}

// withStripe points the server under test at a stub Stripe and a known webhook secret.
func (e *env) withStripe() *stubStripe {
	e.t.Helper()
	st := &stubStripe{}
	st.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, _, ok := r.BasicAuth(); !ok || u != "sk_test_stub" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = r.ParseForm()
		st.mu.Lock()
		defer st.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/checkout/sessions":
			st.sessions = append(st.sessions, r.PostForm)
			id := fmt.Sprintf("cs_test_%d", len(st.sessions))
			json.NewEncoder(w).Encode(map[string]string{"id": id, "url": "https://checkout.stripe.test/" + id})
		case r.URL.Path == "/v1/refunds":
			st.refunds = append(st.refunds, r.PostForm.Get("payment_intent"))
			w.Write([]byte(`{"id":"re_1"}`))
		case strings.HasPrefix(r.URL.Path, "/v1/subscriptions/"):
			if r.Method == http.MethodDelete || r.PostForm.Get("cancel_at_period_end") == "true" {
				st.cancels = append(st.cancels, strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/"))
			}
			w.Write([]byte(`{"id":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	e.t.Cleanup(st.srv.Close)
	cfg := e.deps.Cfg
	cfg.StripeSecretKey, cfg.StripeWebhookSecret, cfg.StripeAPIBase = "sk_test_stub", whSecret, st.srv.URL
	cfg.CheckoutSuccessURL, cfg.CheckoutCancelURL = "connectme://ok", "connectme://nope"
	e.deps.Cfg = cfg
	e.h = server.New(e.deps).Handler
	return st
}

// webhookRaw posts a body with an explicit signature header.
func (e *env) webhookRaw(body []byte, header string) int {
	req := httptest.NewRequest("POST", "/payments/webhook", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", header)
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1", rand.Intn(256), rand.Intn(256), rand.Intn(256))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec.Code
}

// event delivers a correctly signed event.
func (e *env) event(id, typ string, object any) int {
	body, _ := json.Marshal(map[string]any{"id": id, "type": typ, "data": map[string]any{"object": object}})
	return e.webhookRaw(body, payments.SignPayload(body, whSecret, time.Now()))
}

var evSeq = time.Now().UnixNano()

func uniq(prefix string) string { evSeq++; return fmt.Sprintf("%s_%d", prefix, evSeq) }

func (e *env) paidCheckout(uid, pack string, cents int64) (session string, status int) {
	session = uniq("cs_paid")
	status = e.event(uniq("evt"), "checkout.session.completed", map[string]any{
		"id": session, "mode": "payment", "payment_status": "paid", "payment_intent": uniq("pi"),
		"amount_total": cents, "currency": "usd",
		"metadata": map[string]string{"user_id": uid, "pack_id": pack},
	})
	return
}

// subscribe makes a user hold a tier through the same webhook path Stripe uses.
func (e *env) subscribe(uid, tier, status string, endsIn time.Duration) string {
	sub := uniq("sub")
	e.subEvent("customer.subscription.created", sub, uid, tier, status, endsIn)
	return sub
}

func (e *env) subEvent(typ, sub, uid, tier, status string, endsIn time.Duration) int {
	return e.event(uniq("evt"), typ, map[string]any{
		"id": sub, "status": status, "cancel_at_period_end": false,
		"current_period_end": time.Now().Add(endsIn).Unix(),
		"metadata":           map[string]string{"user_id": uid, "tier": tier},
	})
}

func (e *env) coins(uid string) int64 {
	e.t.Helper()
	b, err := wallet.Balance(context.Background(), e.db, uid)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func (e *env) grant(uid string, n int) {
	e.t.Helper()
	if _, err := wallet.Grant(context.Background(), e.db, uid, n, "test", ""); err != nil {
		e.t.Fatal(err)
	}
}

func needPay(t *testing.T) (*env, *stubStripe) {
	t.Helper()
	e := setup(t)
	return e, e.withStripe()
}
