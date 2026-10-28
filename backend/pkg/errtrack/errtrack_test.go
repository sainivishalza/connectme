package errtrack

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type sink struct {
	mu     sync.Mutex
	bodies []string
	hdrs   []http.Header
	paths  []string
	srv    *httptest.Server
}

func newSink(t *testing.T) *sink {
	s := &sink{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies, s.hdrs, s.paths = append(s.bodies, string(b)), append(s.hdrs, r.Header.Clone()), append(s.paths, r.URL.Path)
		s.mu.Unlock()
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *sink) dsn() string { return strings.Replace(s.srv.URL, "://", "://pubkey123@", 1) + "/42" }

func (s *sink) wait(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < 300; i++ {
		s.mu.Lock()
		c := len(s.bodies)
		s.mu.Unlock()
		if c >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d events", n)
}

func TestNoDSNMeansNothingIsSent(t *testing.T) {
	r, err := Init("", "test", "v1")
	if r != nil || err != nil {
		t.Fatalf("got %v %v", r, err)
	}
	Capture(errors.New("boom"), "GET /x") // must be a safe no-op
}

func TestBadDSNsAreRejected(t *testing.T) {
	for _, d := range []string{"nonsense", "ftp://k@h/1", "https://h/1", "https://k@h", "https://k@h/1/2"} {
		if _, err := Init(d, "t", "v"); err == nil {
			t.Errorf("accepted %q", d)
		}
	}
}

func TestEventIsAnEnvelopeWithoutPersonalData(t *testing.T) {
	s := newSink(t)
	r, err := Init(s.dsn(), "production", "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	Capture(errors.New("insert failed for ana@example.com token=abc123 call 5551234567 at /x?email=a@b.co&k=1"), "POST /matching/swipe")
	s.wait(t, 1)
	r.Flush(time.Second)

	body := s.bodies[0]
	if s.paths[0] != "/api/42/envelope/" || !strings.Contains(s.hdrs[0].Get("X-Sentry-Auth"), "sentry_key=pubkey123") {
		t.Errorf("request: %s %v", s.paths[0], s.hdrs[0])
	}
	lines := strings.Split(body, "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], `"type":"event"`) {
		t.Fatalf("not an envelope: %q", body)
	}
	for _, leak := range []string{"ana@example.com", "abc123", "5551234567", "a@b.co", "email=", "Authorization", "ip_address", `"user"`, `"request"`} {
		if strings.Contains(body, leak) {
			t.Errorf("event leaks %q: %s", leak, body)
		}
	}
	for _, want := range []string{`"environment":"production"`, `"release":"1.2.3"`, `"route":"POST /matching/swipe"`, "[email]", "[number]", "pkg/errtrack/errtrack_test.go"} {
		if !strings.Contains(body, want) {
			t.Errorf("event lacks %q: %s", want, body)
		}
	}
}

func TestDuplicatesAndFloodsAreLimited(t *testing.T) {
	s := newSink(t)
	r, _ := Init(s.dsn(), "t", "v")
	for i := 0; i < 20; i++ {
		Capture(errors.New("same problem"), "GET /a") // the same error twenty times is one event
	}
	for i := 0; i < 100; i++ {
		Capture(errors.New("different "+strings.Repeat("x", i+1)), "GET /b")
	}
	r.Flush(3 * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) != 30 { // one collapsed duplicate plus 29 distinct, then the per-minute cap
		t.Errorf("sent %d events, want 30", len(s.bodies))
	}
}

func TestPanicBecomesA500AndOneReport(t *testing.T) {
	s := newSink(t)
	r, _ := Init(s.dsn(), "t", "v")
	h := chi.NewRouter()
	h.Use(Recoverer)
	h.Get("/users/{ref}", func(http.ResponseWriter, *http.Request) { panic("secret=hunter2 for bob@example.com") })
	h.Get("/ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("fine")) })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/users/real-person-ref?token=zzz", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("response: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/ok", nil))
	if rec.Code != 200 {
		t.Errorf("healthy route: %d", rec.Code)
	}
	s.wait(t, 1)
	r.Flush(time.Second)
	body := s.bodies[0]
	if !strings.Contains(body, `"route":"GET /users/{ref}"`) || !strings.Contains(body, `"level":"fatal"`) {
		t.Errorf("event: %s", body)
	}
	for _, leak := range []string{"hunter2", "bob@example.com", "real-person-ref", "zzz"} {
		if strings.Contains(body, leak) {
			t.Errorf("leaks %q", leak)
		}
	}
}

func TestSlowOrDeadCollectorNeverBlocksTheCaller(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)
	if _, err := Init(strings.Replace(srv.URL, "://", "://k@", 1)+"/1", "t", "v"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			Capture(errors.New("e"+strings.Repeat("y", i)), "GET /z")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Capture blocked on a stalled collector")
	}
}

func TestErrorLogsAreReportedOnceAndScrubbed(t *testing.T) {
	s := newSink(t)
	r, _ := Init(s.dsn(), "t", "v")
	var out strings.Builder
	log := slog.New(Wrap(slog.NewTextHandler(&out, nil)))
	log.Info("fine, not reported", "err", "ignored")
	log.Error("matching: swipe failed", "err", "duplicate key for ana@example.com", "route", "POST /matching/swipe")
	log.Error("panic in handler", "err", "x", "reported", true) // already reported by the recoverer
	r.Flush(time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) != 1 || !strings.Contains(s.bodies[0], "swipe failed") || strings.Contains(s.bodies[0], "ana@example.com") ||
		!strings.Contains(s.bodies[0], `"route":"POST /matching/swipe"`) {
		t.Errorf("events: %v", s.bodies)
	}
	if !strings.Contains(out.String(), "fine, not reported") {
		t.Error("the wrapped handler stopped logging")
	}
}
