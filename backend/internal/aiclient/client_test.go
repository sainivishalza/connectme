package aiclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSendsKeyAndParses(t *testing.T) {
	var gotKey, gotType string
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotType = r.Header.Get("X-Internal-Key"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(Verdict{Action: "block", Reason: "no", Categories: []string{"underage"}})
	}))
	defer srv.Close()

	v, err := New(srv.URL, "s3cret").ModerateText(context.Background(), "bio", "I'm 15")
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "s3cret" || gotType != "application/json" || body["kind"] != "bio" || body["text"] != "I'm 15" {
		t.Fatalf("request: key=%q type=%q body=%v", gotKey, gotType, body)
	}
	if v.Action != "block" || v.Categories[0] != "underage" {
		t.Fatalf("verdict: %+v", v)
	}
}

func TestImageIsSentRaw(t *testing.T) {
	var got []byte
	var ct string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		ct = r.Header.Get("Content-Type")
		_ = json.NewEncoder(w).Encode(Verdict{Action: "allow"})
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "k").ModerateImage(context.Background(), []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if string(got) != "\x01\x02\x03" || ct != "application/octet-stream" {
		t.Fatalf("got %v %q", got, ct)
	}
}

func TestTimeoutAndErrorsAreSurfaced(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	start := time.Now()
	if _, err := New(slow.URL, "").ModerateText(context.Background(), "bio", "x"); err == nil {
		t.Fatal("expected a timeout")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("call must give up quickly, took %v", time.Since(start))
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("not json")) }))
	defer bad.Close()
	if _, err := New(bad.URL, "").Score(context.Background(), ScoreRequest{}); err == nil {
		t.Fatal("malformed JSON must be an error")
	}

	if _, err := New("", "").ModerateText(context.Background(), "bio", "x"); err != ErrUnavailable {
		t.Fatalf("disabled client: %v", err)
	}
}

func TestCircuitBreakerOpensAndRecovers(t *testing.T) {
	var calls atomic.Int32
	healthy := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !healthy.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(Verdict{Action: "allow"})
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	now := time.Now()
	c.now = func() time.Time { return now }

	for i := 0; i < failureThreshold; i++ {
		_, _ = c.ModerateText(context.Background(), "bio", "x")
	}
	before := calls.Load()
	for i := 0; i < 10; i++ {
		if _, err := c.ModerateText(context.Background(), "bio", "x"); err == nil {
			t.Fatal("calls must fail fast while the breaker is open")
		}
	}
	if calls.Load() != before {
		t.Fatalf("breaker open: %d calls reached the server", calls.Load()-before)
	}

	// After the cool-down a healthy service is used again.
	now = now.Add(openFor + time.Second)
	healthy.Store(true)
	if _, err := c.ModerateText(context.Background(), "bio", "x"); err != nil {
		t.Fatalf("after cool-down: %v", err)
	}
}

func TestClientErrorsDoNotTripTheBreaker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusUnprocessableEntity)
	}))
	defer srv.Close()
	c := New(srv.URL, "")
	for i := 0; i < failureThreshold*2; i++ {
		_, _ = c.Score(context.Background(), ScoreRequest{})
	}
	if !c.allow() {
		t.Fatal("4xx responses are our bug, not an outage: breaker must stay closed")
	}
}
