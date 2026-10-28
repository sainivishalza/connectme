// Command loadtest drives a running API over real HTTP with many concurrent
// virtual users and reports throughput, latency and errors for each stage.
//
//	set -a; . ../.env; set +a
//	go run ./cmd/loadtest -base http://127.0.0.1:8080 -stages 100,500,1000 -dur 30s
//
// It is a dev tool: it inserts users named load-*@example.test straight into
// the database (so it needs POSTGRES_URL and JWT_SECRET), signs their access
// tokens, clears the rate-limit counters between stages, and deletes the users
// at the end. Never point it at production.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"connectme/pkg/middleware"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type op struct {
	name, path string
	weight     int
}

var mix = []op{
	{"me", "/users/me", 40},
	{"nearby", "/users/nearby", 25},
	{"feed", "/social/feed?kind=moment", 15},
	{"matches", "/matching/matches", 10},
	{"balance", "/payments/balance", 10},
}

type result struct {
	op      string
	status  int
	latency time.Duration
}

func main() {
	base := flag.String("base", "http://127.0.0.1:8080", "API base URL")
	stagesF := flag.String("stages", "100,300,600,1000", "concurrent virtual users per stage")
	dur := flag.Duration("dur", 30*time.Second, "length of each stage")
	think := flag.Duration("think", 800*time.Millisecond, "mean pause between a user's requests")
	pop := flag.Int("population", 0, "people to create in the area (default: the largest stage); density, not concurrency, drives the cost of /users/nearby")
	keep := flag.Bool("keep", false, "keep the generated users")
	flag.Parse()

	var stages []int
	for _, s := range strings.Split(*stagesF, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 1 {
			fatal("bad stage %q", s)
		}
		stages = append(stages, n)
	}
	maxUsers := stages[len(stages)-1]
	secret, pg, rd := os.Getenv("JWT_SECRET"), os.Getenv("POSTGRES_URL"), os.Getenv("REDIS_URL")
	if secret == "" || pg == "" || rd == "" {
		fatal("JWT_SECRET, POSTGRES_URL and REDIS_URL must be set (source ../.env)")
	}
	if !strings.Contains(*base, "127.0.0.1") && !strings.Contains(*base, "localhost") {
		fatal("refusing to load-test %s: this tool only targets a local API", *base)
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, pg)
	check(err)
	defer db.Close()
	ropt, err := redis.ParseURL(rd)
	check(err)
	rc := redis.NewClient(ropt)

	if *pop < maxUsers {
		*pop = maxUsers
	}
	ids := makeUsers(ctx, db, *pop)
	if !*keep {
		defer func() {
			_, err := db.Exec(ctx, `DELETE FROM users WHERE email LIKE 'load-%@example.test'`)
			fmt.Println("cleanup:", map[bool]string{true: "ok", false: fmt.Sprint(err)}[err == nil])
		}()
	}
	tokens := make([]string, len(ids))
	for i, id := range ids {
		tokens[i], err = middleware.SignAccessToken([]byte(secret), id, "user", time.Hour)
		check(err)
	}

	tr := &http.Transport{MaxIdleConns: maxUsers + 10, MaxIdleConnsPerHost: maxUsers + 10, IdleConnTimeout: 90 * time.Second}
	client := &http.Client{Transport: tr, Timeout: 20 * time.Second}

	fmt.Printf("%-7s %8s %8s %8s %8s %8s %8s %7s %7s %7s\n", "users", "req", "req/s", "p50ms", "p95ms", "p99ms", "max ms", "5xx", "429", "neterr")
	for _, n := range stages {
		clearLimits(ctx, rc)
		res := runStage(client, *base, tokens[:n], *dur, *think)
		report(n, res, *dur)
		perOp(res)
	}
	fmt.Println("note: the load generator, API, Postgres, Redis and Mongo all share this machine's CPU")
}

func makeUsers(ctx context.Context, db *pgxpool.Pool, n int) []string {
	_, _ = db.Exec(ctx, `DELETE FROM users WHERE email LIKE 'load-%@example.test'`)
	rows, err := db.Query(ctx, `
		WITH u AS (
		  INSERT INTO users (email, password_hash, date_of_birth, is_verified)
		  SELECT 'load-' || g || '@example.test', 'x', DATE '1992-03-04', TRUE FROM generate_series(1,$1) g
		  RETURNING id, email),
		p AS (
		  INSERT INTO profiles (user_id, display_name, intro, gender, tags, mode, location, location_updated_at)
		  SELECT id, 'Load ' || split_part(email,'@',1), 'hello there', 'woman', '{music,travel}', 'open',
		         ST_SetSRID(ST_MakePoint(8.54 + random()*0.2, 47.36 + random()*0.2),4326)::geography, NOW()
		  FROM u RETURNING user_id)
		SELECT user_id::text FROM p`, n)
	check(err)
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		check(rows.Scan(&id))
		ids = append(ids, id)
	}
	check(rows.Err())
	return ids
}

func clearLimits(ctx context.Context, rc *redis.Client) {
	var cursor uint64
	for {
		keys, next, err := rc.Scan(ctx, cursor, "rl:*", 1000).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			rc.Del(ctx, keys...)
		}
		if cursor = next; cursor == 0 {
			return
		}
	}
}

// rng is a tiny xorshift generator: load shaping needs no cryptographic
// randomness, and each virtual user owns one so there is no shared lock.
type rng uint64

func (r *rng) next() uint64 {
	x := uint64(*r)
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*r = rng(x)
	return x
}

func (r *rng) Intn(n int) int       { return int(r.next() % uint64(n)) }
func (r *rng) Int63n(n int64) int64 { return int64(r.next() % uint64(n)) }
func (r *rng) Float64() float64     { return float64(r.next()%1_000_000) / 1_000_000 }

func pick(r *rng) op {
	t := 0
	for _, o := range mix {
		t += o.weight
	}
	x := r.Intn(t)
	for _, o := range mix {
		if x -= o.weight; x < 0 {
			return o
		}
	}
	return mix[0]
}

func runStage(c *http.Client, base string, tokens []string, dur, think time.Duration) []result {
	var mu sync.Mutex
	var all []result
	var wg sync.WaitGroup
	stop := time.Now().Add(dur)
	var started atomic.Int64
	for i, tok := range tokens {
		wg.Add(1)
		go func(i int, tok string) {
			defer wg.Done()
			r := rng(uint64(i)*7919 + uint64(time.Now().UnixNano()) | 1)
			r.next()
			time.Sleep(time.Duration(r.Int63n(int64(think) * 2))) // stagger the start
			var local []result
			for time.Now().Before(stop) {
				o := pick(&r)
				req, _ := http.NewRequest("GET", base+o.path, nil)
				req.Header.Set("Authorization", "Bearer "+tok)
				t0 := time.Now()
				resp, err := c.Do(req)
				st := 0
				if err == nil {
					st = resp.StatusCode
					_, _ = copyDiscard(resp)
					resp.Body.Close()
				}
				local = append(local, result{o.name, st, time.Since(t0)})
				started.Add(1)
				time.Sleep(time.Duration(float64(think) * (0.5 + r.Float64())))
			}
			mu.Lock()
			all = append(all, local...)
			mu.Unlock()
		}(i, tok)
	}
	wg.Wait()
	return all
}

func copyDiscard(r *http.Response) (int64, error) {
	buf := make([]byte, 32<<10)
	var n int64
	for {
		m, err := r.Body.Read(buf)
		n += int64(m)
		if err != nil {
			return n, nil
		}
	}
}

func perOp(rs []result) {
	by := map[string][]time.Duration{}
	for _, r := range rs {
		by[r.op] = append(by[r.op], r.latency)
	}
	for _, o := range mix {
		l := by[o.name]
		if len(l) == 0 {
			continue
		}
		sort.Slice(l, func(i, j int) bool { return l[i] < l[j] })
		fmt.Printf("    %-8s n=%-6d p50 %7.0f ms   p95 %7.0f ms\n", o.name, len(l),
			float64(l[len(l)/2].Microseconds())/1000, float64(l[int(float64(len(l)-1)*.95)].Microseconds())/1000)
	}
}

func report(users int, rs []result, dur time.Duration) {
	var lat []time.Duration
	var e5, e429, ne int
	for _, r := range rs {
		switch {
		case r.status == 0:
			ne++
		case r.status >= 500:
			e5++
		case r.status == 429:
			e429++
		}
		lat = append(lat, r.latency)
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	q := func(p float64) float64 {
		if len(lat) == 0 {
			return 0
		}
		return float64(lat[int(float64(len(lat)-1)*p)].Microseconds()) / 1000
	}
	mx := 0.0
	if len(lat) > 0 {
		mx = float64(lat[len(lat)-1].Microseconds()) / 1000
	}
	fmt.Printf("%-7d %8d %8.0f %8.0f %8.0f %8.0f %8.0f %7d %7d %7d\n", users, len(rs), float64(len(rs))/dur.Seconds(), q(.5), q(.95), q(.99), mx, e5, e429, ne)
}

func check(err error) {
	if err != nil {
		fatal("%v", err)
	}
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}
