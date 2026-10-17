package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Integration test: needs a real Redis (TEST_REDIS_URL), skipped otherwise.
func TestRateLimitIntegration(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()

	name := "test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	h := RateLimit(rdb, name, 50, 15*time.Minute, ByIP)(http.HandlerFunc(ok))

	for i := 1; i <= 50; i++ {
		if rec := do(h, nil, "GET"); rec.Code != http.StatusOK {
			t.Fatalf("request %d: got %d want 200", i, rec.Code)
		}
	}
	rec := do(h, nil, "GET")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 51: got %d want 429", rec.Code)
	}
	if secs, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || secs < 1 {
		t.Fatalf("bad Retry-After %q", rec.Header().Get("Retry-After"))
	}

	// A different client is unaffected.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.9:4444"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("other client: got %d want 200", rec2.Code)
	}
}

func TestRateLimitFailsClosed(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 200 * time.Millisecond})
	defer rdb.Close()
	h := RateLimit(rdb, "x", 1, time.Minute, ByIP)(http.HandlerFunc(ok))
	if rec := do(h, nil, "GET"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d want 503", rec.Code)
	}
}

func TestRateLimitReadsFailOpenWritesFailClosed(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 200 * time.Millisecond})
	defer rdb.Close()
	for name, want := range map[string]int{
		"users:read": http.StatusOK, "live:list": http.StatusOK, "live:events": http.StatusOK, "read": http.StatusOK,
		"auth:login": http.StatusServiceUnavailable, "payments:gift": http.StatusServiceUnavailable,
		"live:gift": http.StatusServiceUnavailable, "readme:write": http.StatusServiceUnavailable,
		"users:photo-read-more": http.StatusServiceUnavailable,
	} {
		h := RateLimit(rdb, name, 1, time.Minute, ByIP)(http.HandlerFunc(ok))
		if rec := do(h, nil, "GET"); rec.Code != want {
			t.Errorf("%s: got %d want %d", name, rec.Code, want)
		}
	}
}
