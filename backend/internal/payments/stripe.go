package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// WebhookTolerance is how far a signed timestamp may be from now (replay window).
const WebhookTolerance = 5 * time.Minute

// ErrBadSignature is returned for any signature problem; callers must not say why.
var ErrBadSignature = errors.New("invalid signature")

// VerifySignature validates a Stripe-Signature header ("t=...,v1=...[,v1=...]")
// over the raw request body: HMAC-SHA256(secret, "<t>.<payload>") compared in
// constant time, with a replay window. It must run on the exact bytes received.
func VerifySignature(payload []byte, header, secret string, now time.Time) error {
	if secret == "" {
		return ErrBadSignature
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return ErrBadSignature
	}
	if d := now.Sub(time.Unix(sec, 0)); d > WebhookTolerance || d < -WebhookTolerance {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(payload)
	want := mac.Sum(nil)
	for _, s := range sigs {
		got, err := hex.DecodeString(s)
		if err == nil && hmac.Equal(got, want) {
			return nil
		}
	}
	return ErrBadSignature
}

// SignPayload builds a valid header for tests and local tooling.
func SignPayload(payload []byte, secret string, at time.Time) string {
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(payload)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// CheckoutParams describes one Checkout Session. Amounts always come from the
// server-side catalog.
type CheckoutParams struct {
	Mode        string // "payment" or "subscription"
	ProductName string
	Cents       int64
	Recurring   bool
	UserID      string
	Metadata    map[string]string
	SuccessURL  string
	CancelURL   string
}

// Stripe is the subset of the Stripe API this app uses.
type Stripe interface {
	CreateCheckout(ctx context.Context, p CheckoutParams) (id, url string, err error)
	CancelAtPeriodEnd(ctx context.Context, subscriptionID string) error
	// CancelNow ends a subscription immediately (account erasure). A
	// subscription that no longer exists counts as cancelled.
	CancelNow(ctx context.Context, subscriptionID string) error
	// Refund refunds a payment in full.
	Refund(ctx context.Context, paymentIntent string) error
}

// HTTPStripe talks to the real Stripe REST API with the secret key.
type HTTPStripe struct {
	SecretKey string
	BaseURL   string // default https://api.stripe.com
	Client    *http.Client
}

func NewHTTPStripe(secretKey, baseURL string) *HTTPStripe {
	if baseURL == "" {
		baseURL = "https://api.stripe.com"
	}
	return &HTTPStripe{SecretKey: secretKey, BaseURL: baseURL, Client: &http.Client{Timeout: 15 * time.Second}}
}

func (s *HTTPStripe) post(ctx context.Context, path string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(s.SecretKey, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("stripe %s: status %d", path, resp.StatusCode)
	}
	return body, nil
}

func (s *HTTPStripe) CreateCheckout(ctx context.Context, p CheckoutParams) (string, string, error) {
	f := url.Values{}
	f.Set("mode", p.Mode)
	f.Set("success_url", p.SuccessURL)
	f.Set("cancel_url", p.CancelURL)
	f.Set("client_reference_id", p.UserID)
	f.Set("line_items[0][quantity]", "1")
	f.Set("line_items[0][price_data][currency]", Currency)
	f.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(p.Cents, 10))
	f.Set("line_items[0][price_data][product_data][name]", p.ProductName)
	if p.Recurring {
		f.Set("line_items[0][price_data][recurring][interval]", "month")
	}
	for k, v := range p.Metadata {
		f.Set("metadata["+k+"]", v)
		if p.Mode == "subscription" { // so subscription.* events carry them too
			f.Set("subscription_data[metadata]["+k+"]", v)
		}
	}
	body, err := s.post(ctx, "/v1/checkout/sessions", f)
	if err != nil {
		return "", "", err
	}
	var out struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.ID == "" || out.URL == "" {
		return "", "", errors.New("stripe: unexpected checkout response")
	}
	return out.ID, out.URL, nil
}

func (s *HTTPStripe) Refund(ctx context.Context, paymentIntent string) error {
	f := url.Values{}
	f.Set("payment_intent", paymentIntent)
	_, err := s.post(ctx, "/v1/refunds", f)
	return err
}

func (s *HTTPStripe) CancelAtPeriodEnd(ctx context.Context, subscriptionID string) error {
	f := url.Values{}
	f.Set("cancel_at_period_end", "true")
	_, err := s.post(ctx, "/v1/subscriptions/"+url.PathEscape(subscriptionID), f)
	return err
}

func (s *HTTPStripe) CancelNow(ctx context.Context, subscriptionID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		s.BaseURL+"/v1/subscriptions/"+url.PathEscape(subscriptionID), nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.SecretKey, "")
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("stripe cancel: status %d", resp.StatusCode)
}
