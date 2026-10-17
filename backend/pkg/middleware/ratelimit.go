package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectme/pkg/metrics"

	"github.com/redis/go-redis/v9"
)

// Atomic sliding window over a sorted set (scores are unix ms).
// Returns {allowed(0|1), retry_after_ms}.
var slidingWindow = redis.NewScript(`
local key, now, window, limit, member = KEYS[1], tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3]), ARGV[4]
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
if redis.call('ZCARD', key) >= limit then
  local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
  return {0, window - (now - tonumber(oldest[2]))}
end
redis.call('ZADD', key, now, member)
redis.call('PEXPIRE', key, window)
return {1, 0}
`)

// KeyFunc derives the limiter subject (user id, client IP, ...) for a request.
type KeyFunc func(r *http.Request) string

// ByUser limits per authenticated user; it falls back to the client IP so the
// limiter still protects routes reached before authentication.
func ByUser(r *http.Request) string {
	if id := UserID(r.Context()); id != "" {
		return "u:" + id
	}
	return ByIP(r)
}

// ByIP limits per client address. Behind a load balancer configure
// TRUSTED_PROXY_CIDRS so the real client (not the balancer) is used; by
// default X-Forwarded-For is ignored because clients can forge it.
func ByIP(r *http.Request) string { return "ip:" + ClientIP(r) }

// failsOpen reports whether a limiter protects only cheap, authenticated
// reads. Those keep working when Redis is down (a cache blip must not become
// a total outage); every other limiter (login, writes, payments, gifts...)
// fails closed so limits cannot be bypassed. The convention is the name's
// final segment: "read", "list" or "events".
func failsOpen(name string) bool {
	i := strings.LastIndexByte(name, ':')
	switch name[i+1:] {
	case "read", "list", "events":
		return true
	}
	return false
}

// RateLimit allows `limit` requests per `window` for each key. When Redis is
// unavailable it fails closed (503), except for read-only limiters (see
// failsOpen) which let the request through and count it in the
// `ratelimit_fail_open` event metric.
// `name` namespaces the counter, so each route group has its own budget.
func RateLimit(rdb redis.Scripter, name string, limit int, window time.Duration, key KeyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var nonce [8]byte
			_, _ = rand.Read(nonce[:])
			now := time.Now().UnixMilli()
			res, err := slidingWindow.Run(r.Context(), rdb,
				[]string{"rl:" + name + ":" + key(r)},
				now, window.Milliseconds(), limit, strconv.FormatInt(now, 10)+"-"+hex.EncodeToString(nonce[:]),
			).Int64Slice()
			if err != nil || len(res) != 2 {
				if err != nil && failsOpen(name) {
					metrics.Event("ratelimit_fail_open")
					w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
					next.ServeHTTP(w, r)
					return
				}
				WriteError(w, http.StatusServiceUnavailable, "service unavailable")
				return
			}
			if res[0] == 0 {
				secs := (res[1] + 999) / 1000
				if secs < 1 {
					secs = 1
				}
				metrics.RateLimited(name)
				w.Header().Set("Retry-After", strconv.FormatInt(secs, 10))
				WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			next.ServeHTTP(w, r)
		})
	}
}
