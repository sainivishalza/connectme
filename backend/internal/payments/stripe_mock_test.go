package payments

import (
	"context"
	"os"
	"testing"
)

// Opt-in: validates our Stripe requests against Stripe's official OpenAPI spec
// using stripe-mock (it rejects unknown or malformed parameters with a 400).
//
//	docker run --rm -p 12111:12111 stripe/stripe-mock
//	TEST_STRIPE_MOCK_URL=http://127.0.0.1:12111 go test ./internal/payments -run StripeMock
//
// This checks request shapes and response parsing, not Stripe's behaviour:
// real test-mode runs with the Stripe CLI are still needed before launch.
func TestStripeMockAcceptsOurRequests(t *testing.T) {
	base := os.Getenv("TEST_STRIPE_MOCK_URL")
	if base == "" {
		t.Skip("TEST_STRIPE_MOCK_URL not set")
	}
	s := NewHTTPStripe("sk_test_mock", base)
	ctx := context.Background()
	for _, p := range []CheckoutParams{
		{Mode: "payment", ProductName: "100 coins", Cents: 499, UserID: "u1",
			Metadata:   map[string]string{"user_id": "u1", "kind": "coins", "pack": "coins_100"},
			SuccessURL: "connectme://ok", CancelURL: "connectme://cancel"},
		{Mode: "subscription", ProductName: "Gold", Cents: 1999, Recurring: true, UserID: "u1",
			Metadata:   map[string]string{"user_id": "u1", "tier": "gold"},
			SuccessURL: "https://example.com/ok", CancelURL: "https://example.com/cancel"},
	} {
		id, url, err := s.CreateCheckout(ctx, p)
		if err != nil {
			t.Fatalf("%s checkout rejected by the Stripe spec: %v", p.Mode, err)
		}
		if id == "" || url == "" {
			t.Errorf("%s: empty id/url", p.Mode)
		}
	}
	if err := s.CancelAtPeriodEnd(ctx, "sub_123"); err != nil {
		t.Errorf("CancelAtPeriodEnd: %v", err)
	}
	if err := s.CancelNow(ctx, "sub_123"); err != nil {
		t.Errorf("CancelNow: %v", err)
	}
	if err := s.Refund(ctx, "pi_123"); err != nil {
		t.Errorf("Refund: %v", err)
	}
	// A booking payment is an ordinary one-off checkout with booking metadata.
	if _, _, err := s.CreateCheckout(ctx, CheckoutParams{Mode: "payment", ProductName: "Booking: Casa Verde", Cents: 20000, UserID: "u1",
		Metadata: map[string]string{"user_id": "u1", "booking_id": "b1"}, SuccessURL: "connectme://ok", CancelURL: "connectme://cancel"}); err != nil {
		t.Errorf("booking checkout rejected by the Stripe spec: %v", err)
	}
}
