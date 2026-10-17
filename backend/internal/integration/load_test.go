package integration

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

// TestLoadSmoke is an indicative in-process load check, not a capacity test:
// it measures the application + database + Redis path with many concurrent
// authenticated users, excluding the network and TLS. Run with LOAD=1.
func TestLoadSmoke(t *testing.T) {
	if os.Getenv("LOAD") != "1" {
		t.Skip("set LOAD=1 to run")
	}
	e := setup(t)
	const users, perUser = 120, 40
	lat, lng := randomBase()

	ids := make([]string, users)
	toks := make([]string, users)
	for i := range ids {
		ids[i] = e.user(fmt.Sprintf("Load%d", i))
		e.locate(ids[i], lat+float64(i%20)*0.001, lng+float64(i/20)*0.001)
		toks[i] = e.tokenFor(ids[i], "user")
	}
	paths := []string{"/users/me", "/users/nearby?radius_km=25", "/matching/matches", "/users/me", "/groups"}

	var mu sync.Mutex
	var lat_ []time.Duration
	codes := map[int]int{}
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < users; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			ip := fmt.Sprintf("10.20.%d.%d:1", w/250, w%250)
			for i := 0; i < perUser; i++ {
				req := httptest.NewRequest("GET", paths[rand.Intn(len(paths))], nil)
				req.Header.Set("Authorization", "Bearer "+toks[w])
				req.RemoteAddr = ip
				rec := httptest.NewRecorder()
				t0 := time.Now()
				e.h.ServeHTTP(rec, req)
				d := time.Since(t0)
				mu.Lock()
				lat_ = append(lat_, d)
				codes[rec.Code]++
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	total := time.Since(start)

	sort.Slice(lat_, func(i, j int) bool { return lat_[i] < lat_[j] })
	pct := func(p float64) time.Duration { return lat_[int(float64(len(lat_)-1)*p)] }
	n := len(lat_)
	t.Logf("%d requests from %d concurrent users in %v (%.0f req/s)", n, users, total.Round(time.Millisecond), float64(n)/total.Seconds())
	t.Logf("latency p50=%v p95=%v p99=%v max=%v", pct(.5), pct(.95), pct(.99), lat_[n-1])
	t.Logf("status codes: %v", codes)
	for code, c := range codes {
		if code >= 500 {
			t.Errorf("%d responses with status %d under load", c, code)
		}
		if code == http.StatusTooManyRequests {
			t.Logf("note: %d requests hit rate limits", c)
		}
	}
}
