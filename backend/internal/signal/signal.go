// Package signal tells connected clients that something they are looking at
// has changed, so they can refetch it instead of asking every few seconds.
// A signal carries no data: only a topic name. The client then reads the
// state through the normal, authorised API, so nothing new can leak, and
// polling stays as the fallback when the realtime relay is unreachable.
package signal

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Prefix is the Redis channel prefix the relay subscribes to.
const Prefix = "sig:"

// TopicFor maps the path of a successful write to the topic whose watchers
// should refetch, or "" when the write is not something anyone watches.
func TopicFor(path string) string {
	p := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case len(p) >= 4 && p[0] == "live" && p[1] == "rooms" && p[3] == "join":
		return "" // a viewer arriving would wake every other viewer
	case len(p) >= 4 && p[0] == "live" && p[1] == "rooms":
		return "live:" + p[2]
	case len(p) >= 4 && p[0] == "games" && p[1] == "masked" && p[2] == "rooms":
		return "masked:" + p[3]
	case len(p) >= 3 && p[0] == "games" && (p[1] == "td" || p[1] == "party" || p[1] == "group"):
		return "game:" + p[2]
	}
	return ""
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(c int) { w.status = c; w.ResponseWriter.WriteHeader(c) }

// Notify publishes the topic of every successful write. A publish failure is
// ignored: the watchers' polling fallback picks the change up anyway.
func Notify(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			if sw.status/100 != 2 {
				return
			}
			if t := TopicFor(r.URL.Path); t != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				_ = rdb.Publish(ctx, Prefix+t, "1").Err()
			}
		})
	}
}

// Service answers whether someone may watch a topic.
type Service struct{ db *pgxpool.Pool }

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

var errNo = errors.New("no access")

// Allowed reports whether userID may receive signals for kind:id.
func (s *Service) Allowed(ctx context.Context, userID, kind, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return errNo
	}
	var q string
	switch kind {
	case "live":
		q = `SELECT EXISTS(SELECT 1 FROM live_rooms WHERE id=$2::uuid)`
	case "masked":
		q = `SELECT EXISTS(SELECT 1 FROM masked_members WHERE room_id=$2::uuid AND user_id=$1::uuid AND left_at IS NULL)`
	case "game":
		q = `SELECT EXISTS(SELECT 1 FROM matches m WHERE m.id=$2::uuid AND m.status='matched' AND $1::uuid IN (m.user1_id, m.user2_id)
		            AND ` + users.NotBlockedSQL("m.user1_id", "m.user2_id") + `)
		      OR EXISTS(SELECT 1 FROM group_matches gm WHERE gm.id=$2::uuid AND gm.status='matched'
		            AND EXISTS (SELECT 1 FROM group_members g WHERE g.group_id IN (gm.group_a, gm.group_b) AND g.user_id=$1::uuid))`
	default:
		return errNo
	}
	var ok bool
	if err := s.db.QueryRow(ctx, q, userID, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return errNo
	}
	return nil
}

// Routes mounts /signal: the relay asks here before it lets a socket watch.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret))
	r.With(middleware.RateLimit(rdb, "signal:access", 600, 15*time.Minute, middleware.ByUser)).
		Get("/{kind}/{id}/access", func(w http.ResponseWriter, req *http.Request) {
			err := svc.Allowed(req.Context(), middleware.UserID(req.Context()), chi.URLParam(req, "kind"), chi.URLParam(req, "id"))
			switch {
			case err == nil:
				w.WriteHeader(http.StatusNoContent)
			case errors.Is(err, errNo):
				middleware.WriteError(w, http.StatusNotFound, "not found")
			default:
				middleware.WriteError(w, http.StatusInternalServerError, "internal")
			}
		})
	return r
}
