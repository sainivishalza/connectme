package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"connectme/pkg/middleware"
)

// The read paths added in Phases 2 and 3 are the ones every member hits: the
// post feeds, short videos, forums, partner events and nearby groups. This
// seeds a realistic amount of data and hammers them concurrently, reporting
// latency per endpoint. Opt in with LOAD=1; it deletes what it created.
func TestFeedsUnderLoad(t *testing.T) {
	if os.Getenv("LOAD") == "" {
		t.Skip("set LOAD=1 to run the feed load test")
	}
	e := setup(t)
	ctx := context.Background()
	lat0, lng0 := randomBase()

	const viewers = 80
	ids := make([]string, viewers)
	toks := make([]string, viewers)
	for i := range ids {
		ids[i] = e.user(fmt.Sprintf("Loader %d", i))
		e.locate(ids[i], lat0+float64(i%9)*0.004, lng0+float64(i%7)*0.004)
		toks[i], _ = middleware.SignAccessToken([]byte(secret), ids[i], "user", 10*time.Minute)
	}
	t.Cleanup(func() {
		if os.Getenv("KEEP") != "" {
			return // leave the data for EXPLAIN work
		}
		_, _ = e.db.Exec(ctx, `DELETE FROM users WHERE id = ANY($1::uuid[])`, ids)
		_, _ = e.db.Exec(ctx, `DELETE FROM partners WHERE name LIKE 'load-%'`)
		_, _ = e.db.Exec(ctx, `DELETE FROM forum_boards WHERE slug LIKE 'load-%'`)
	})

	board := fmt.Sprintf("load-%d", time.Now().UnixNano()%1_000_000)
	seed := []struct {
		q    string
		args []any
	}{
		// 4000 moments and universe posts around the viewers
		{`INSERT INTO posts (author_id, kind, body, alias, location, expires_at)
		 SELECT ($1::uuid[])[1 + (g % 80)], CASE WHEN g % 2 = 0 THEN 'moment' ELSE 'universe' END, 'seeded post ' || g,
		        CASE WHEN g % 2 = 0 THEN NULL ELSE 'Quiet Otter ' || (g % 90) END,
		        ST_SnapToGrid(ST_SetSRID(ST_MakePoint($3 + (random()-0.5)*0.1, $2 + (random()-0.5)*0.1),4326), 0.01)::geography,
		        NOW() + INTERVAL '12 hours'
		   FROM generate_series(1, 4000) g`, []any{ids, lat0, lng0}},
		// 1500 short videos (the feed never reads the blobs)
		{`INSERT INTO videos (owner_id, kind, blob_id, mime, seconds, bytes, caption, created_at)
		 SELECT ($1::uuid[])[1 + (g % 80)], 'short', gen_random_uuid(), 'video/mp4', 10, 1000, 'clip ' || g, NOW() - (g || ' minutes')::interval
		   FROM generate_series(1, 1500) g`, []any{ids}},
	}
	for _, sd := range seed {
		if _, err := e.db.Exec(ctx, sd.q, sd.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	e.exec(`INSERT INTO forum_boards (slug, title, kind) VALUES ($1, 'Load board', 'advice')`, board)
	if _, err := e.db.Exec(ctx, `
		INSERT INTO forum_threads (board, author_id, title, body, last_reply_at)
		SELECT $2, ($1::uuid[])[1 + (g % 80)], 'Seeded thread ' || g, 'Some body text for thread ' || g, NOW() - (g || ' minutes')::interval
		  FROM generate_series(1, 400) g`, ids, board); err != nil {
		t.Fatal(err)
	}
	var thread string
	if err := e.db.QueryRow(ctx, `SELECT id::text FROM forum_threads WHERE board=$1 ORDER BY last_reply_at DESC LIMIT 1`, board).Scan(&thread); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, `
		INSERT INTO forum_replies (thread_id, author_id, body)
		SELECT t.id, ($1::uuid[])[1 + (g % 80)], 'Seeded reply ' || g
		  FROM (SELECT id FROM forum_threads WHERE board=$2) t, generate_series(1, 20) g`, ids, board); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, `
		INSERT INTO partners (kind, name, location, commission_bps)
		SELECT 'restaurant', 'load-' || g, ST_SetSRID(ST_MakePoint($2 + (random()-0.5)*0.2, $1 + (random()-0.5)*0.2),4326)::geography, 1000
		  FROM generate_series(1, 150) g`, lat0, lng0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, `
		INSERT INTO events (partner_id, kind, title, starts_at, ends_at, capacity)
		SELECT p.id, 'hangout', 'Seeded event ' || g, NOW() + (g || ' hours')::interval, NOW() + ((g + 3) || ' hours')::interval, 50
		  FROM partners p, generate_series(1, 4) g WHERE p.name LIKE 'load-%'`); err != nil {
		t.Fatal(err)
	}
	e.exec(`ANALYZE posts, videos, forum_threads, forum_replies, partners, events`)

	endpoints := []struct{ name, path string }{
		{"moments", "/social/feed?kind=moment&radius_km=25"},
		{"universe", "/social/feed?kind=universe&radius_km=25"},
		{"videos", "/videos/feed"},
		{"forum threads", "/forums/boards/" + board + "/threads"},
		{"forum replies", "/forums/threads/" + thread + "/replies"},
		{"partner events", "/partners/events"},
		{"partners nearby", "/partners/nearby"},
		{"groups nearby", "/communities/nearby"},
	}
	var mu sync.Mutex
	lat := map[string][]time.Duration{}
	codes := map[string]map[int]int{}
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < viewers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for round := 0; round < 6; round++ {
				for _, ep := range endpoints {
					req := httptest.NewRequest("GET", ep.path, nil)
					req.Header.Set("Authorization", "Bearer "+toks[w])
					req.RemoteAddr = fmt.Sprintf("10.40.%d.%d:1", w/250, w%250)
					rec := httptest.NewRecorder()
					t0 := time.Now()
					e.h.ServeHTTP(rec, req)
					d := time.Since(t0)
					mu.Lock()
					lat[ep.name] = append(lat[ep.name], d)
					if codes[ep.name] == nil {
						codes[ep.name] = map[int]int{}
					}
					codes[ep.name][rec.Code]++
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()
	t.Logf("%d viewers x 6 rounds x %d endpoints in %v", viewers, len(endpoints), time.Since(start).Round(time.Millisecond))
	for _, ep := range endpoints {
		ds := lat[ep.name]
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		p := func(q float64) time.Duration { return ds[int(float64(len(ds)-1)*q)] }
		t.Logf("%-16s p50=%-8v p95=%-8v max=%-8v codes=%v", ep.name, p(.5).Round(time.Millisecond), p(.95).Round(time.Millisecond), ds[len(ds)-1].Round(time.Millisecond), codes[ep.name])
		for c, n := range codes[ep.name] {
			if c >= 500 {
				t.Errorf("%s: %d responses with status %d", ep.name, n, c)
			}
		}
		if codes[ep.name][http.StatusOK] == 0 {
			t.Errorf("%s: no successful responses (%v)", ep.name, codes[ep.name])
		}
		if p95 := p(.95); p95 > 2*time.Second {
			t.Errorf("%s: p95 %v is over 2 s", ep.name, p95)
		}
	}
}
