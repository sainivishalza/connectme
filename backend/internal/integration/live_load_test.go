package integration

import (
	"connectme/internal/matching"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Races that matter for money: concurrent gifts must never overdraw a wallet
// and a repeated request id must charge exactly once.
func TestLiveGiftRacesNeverOverdrawOrDoubleCharge(t *testing.T) {
	e := setup(t)
	host, fan := e.user("Host"), e.user("Fan")
	room := e.goLive(host, "Race stream")
	path := "/live/rooms/" + room.Room.ID + "/gift"
	e.grant(fan, 30) // three roses

	var ok, poor int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, _ := e.call("POST", path, map[string]string{"gift_id": "rose", "request_id": fmt.Sprintf("race-distinct-%02d", i)}, fan)
			switch c {
			case 200:
				atomic.AddInt32(&ok, 1)
			case 402:
				atomic.AddInt32(&poor, 1)
			default:
				t.Errorf("unexpected status %d", c)
			}
		}(i)
	}
	wg.Wait()
	if ok != 3 || poor != 17 {
		t.Errorf("ok=%d poor=%d, want 3 and 17", ok, poor)
	}
	if got := e.coins(fan); got != 0 {
		t.Errorf("fan balance %d, want 0 (never negative)", got)
	}
	if got := e.coins(host); got != 3*7 { // 70% of 10, three times
		t.Errorf("host received %d, want 21", got)
	}

	// Same request id fired 15 times at once: charged once.
	rich := e.user("Rich")
	e.grant(rich, 100)
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.call("POST", path, map[string]string{"gift_id": "champagne", "request_id": "same-request-id"}, rich)
		}()
	}
	wg.Wait()
	if got := e.coins(rich); got != 50 {
		t.Errorf("rich balance %d, want 50 (one 50-coin gift)", got)
	}
	if n := e.count(`SELECT COUNT(*) FROM live_gifts WHERE from_user=$1`, rich); n != 1 {
		t.Errorf("gift rows %d, want 1", n)
	}
	// Conservation: what viewers paid == host share + platform share.
	var paid, hostSide, platform int
	e.db.QueryRow(t.Context(), `SELECT COALESCE(SUM(coins),0), COALESCE(SUM(host_coins),0), COALESCE(SUM(platform_coins),0)
		FROM live_gifts WHERE room_id=$1::uuid`, room.Room.ID).Scan(&paid, &hostSide, &platform)
	if paid != hostSide+platform {
		t.Errorf("coins not conserved: %d != %d + %d", paid, hostSide, platform)
	}
}

// Many viewers polling, chatting and gifting one room. Run with LOAD=1.
func TestLoadLiveRoom(t *testing.T) {
	if os.Getenv("LOAD") != "1" {
		t.Skip("set LOAD=1 to run")
	}
	e := setup(t)
	host := e.user("LoadHost")
	room := e.goLive(host, "Load stream")
	base := "/live/rooms/" + room.Room.ID
	const viewers, rounds = 200, 20

	toks := make([]string, viewers)
	for i := range toks {
		u := e.user(fmt.Sprintf("Viewer%d", i))
		e.grant(u, 50)
		toks[i] = e.tokenFor(u, "user")
	}
	var mu sync.Mutex
	var lat []time.Duration
	codes := map[int]int{}
	do := func(w int, method, path, body string) {
		req := httptest.NewRequest(method, path, nil)
		if body != "" {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
		}
		req.Header.Set("Authorization", "Bearer "+toks[w])
		req.RemoteAddr = fmt.Sprintf("10.30.%d.%d:1", w/250, w%250)
		rec := httptest.NewRecorder()
		t0 := time.Now()
		e.h.ServeHTTP(rec, req)
		d := time.Since(t0)
		mu.Lock()
		lat = append(lat, d)
		codes[rec.Code]++
		mu.Unlock()
	}
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < viewers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			do(w, "POST", base+"/join", "")
			after := int64(0)
			for r := 0; r < rounds; r++ {
				do(w, "GET", fmt.Sprintf("%s/events?after=%d", base, after), "")
				if r%4 == 0 {
					do(w, "POST", base+"/chat", fmt.Sprintf(`{"text":"hello from %d round %d"}`, w, r))
				}
				if r%10 == 0 {
					do(w, "POST", base+"/gift", fmt.Sprintf(`{"gift_id":"rose","request_id":"load-%d-%d-xxxx"}`, w, r))
				}
			}
		}(w)
	}
	wg.Wait()
	total := time.Since(start)
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	n := len(lat)
	p := func(q float64) time.Duration { return lat[int(float64(n-1)*q)] }
	t.Logf("%d requests, %d viewers, %v (%.0f req/s)", n, viewers, total.Round(time.Millisecond), float64(n)/total.Seconds())
	t.Logf("latency p50=%v p95=%v p99=%v max=%v; codes=%v", p(.5), p(.95), p(.99), lat[n-1], codes)
	for c, k := range codes {
		if c >= 500 {
			t.Errorf("%d responses with status %d", k, c)
		}
	}
	if codes[http.StatusOK] == 0 {
		t.Error("no successful requests")
	}
}

// The older money paths get the same race treatment: no deadlocks (5xx) and
// never an overdraw.
func TestConcurrentSpendsAcrossFeaturesNeverOverdraw(t *testing.T) {
	e := setup(t)
	fan, target, _ := e.matched() // gifts are only allowed between matches
	e.grant(fan, 20)              // two 10-coin roses

	var ok, other int32
	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, _ := e.call("POST", "/payments/gift",
				map[string]string{"to_ref": e.refOf(target), "gift_id": "rose", "request_id": fmt.Sprintf("pay-race-%02d-xxxx", i)}, fan)
			switch c {
			case 204, 200:
				atomic.AddInt32(&ok, 1)
			case 402:
			default:
				atomic.AddInt32(&other, 1)
				t.Errorf("payments gift status %d", c)
			}
		}(i)
	}
	wg.Wait()
	if ok != 2 {
		t.Errorf("successful gifts %d, want 2", ok)
	}
	if got := e.coins(fan); got != 0 {
		t.Errorf("balance %d, want 0", got)
	}

	booster := e.user("Booster")
	e.grant(booster, 2*matching.BoostCoins)
	var boosted int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _ := e.call("POST", "/matching/boost", nil, booster)
			switch c {
			case 200, 204:
				atomic.AddInt32(&boosted, 1)
			case 402:
			default:
				t.Errorf("boost status %d", c)
			}
		}()
	}
	wg.Wait()
	if boosted > 2 {
		t.Errorf("%d boosts succeeded with coins for 2", boosted)
	}
	if got := e.coins(booster); got < 0 {
		t.Errorf("negative balance %d", got)
	}
}
