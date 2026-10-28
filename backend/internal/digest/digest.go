// Package digest sends an opt-in weekly summary email: how many people liked
// you, matched with you, followed you or replied to your posts and videos.
// Counts only: no names and no message text, so it is as private as a number.
package digest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"connectme/pkg/metrics"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Sender delivers a plain email.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

type Service struct {
	db     *pgxpool.Pool
	mail   Sender
	secret []byte
	appURL string
	now    func() time.Time
}

func NewService(db *pgxpool.Pool, m Sender, secret []byte, appURL string) *Service {
	return &Service{db: db, mail: m, secret: secret, appURL: strings.TrimRight(appURL, "/"), now: time.Now}
}

func (s *Service) Enabled(ctx context.Context, userID string) (bool, error) {
	var on bool
	err := s.db.QueryRow(ctx, `SELECT weekly_digest FROM users WHERE id=$1::uuid`, userID).Scan(&on)
	return on, err
}

func (s *Service) Set(ctx context.Context, userID string, on bool) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET weekly_digest=$2 WHERE id=$1::uuid`, userID, on)
	return err
}

// token is the proof behind the unsubscribe link: a signature of the user id.
func (s *Service) token(userID string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("digest-unsubscribe|" + userID))
	return userID + "." + hex.EncodeToString(m.Sum(nil))[:32]
}

var ErrBadToken = errors.New("invalid link")

// Unsubscribe switches the digest off for whoever the token names.
func (s *Service) Unsubscribe(ctx context.Context, token string) error {
	id, _, ok := strings.Cut(token, ".")
	if _, err := uuid.Parse(id); err != nil || !ok || !hmac.Equal([]byte(token), []byte(s.token(id))) {
		return ErrBadToken
	}
	return s.Set(ctx, id, false)
}

type counts struct{ likes, matches, followers, replies int }

func (s *Service) countsFor(ctx context.Context, userID string, since time.Time) (counts, error) {
	var c counts
	err := s.db.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM swipes WHERE target_id=$1::uuid AND action IN ('like','superlike') AND created_at > $2),
		  (SELECT COUNT(*) FROM matches WHERE (user1_id=$1::uuid OR user2_id=$1::uuid) AND created_at > $2),
		  (SELECT COUNT(*) FROM follows WHERE followee_id=$1::uuid AND created_at > $2),
		  (SELECT COUNT(*) FROM post_comments c JOIN posts p ON p.id=c.post_id WHERE p.author_id=$1::uuid AND c.author_id<>$1::uuid AND c.created_at > $2)
		  + (SELECT COUNT(*) FROM video_comments c JOIN videos v ON v.id=c.video_id WHERE v.owner_id=$1::uuid AND c.author_id<>$1::uuid AND c.created_at > $2)`,
		userID, since).Scan(&c.likes, &c.matches, &c.followers, &c.replies)
	return c, err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (s *Service) compose(c counts, userID string) (string, string) {
	var lines []string
	if c.likes > 0 {
		lines = append(lines, plural(c.likes, "person liked you", "people liked you"))
	}
	if c.matches > 0 {
		lines = append(lines, plural(c.matches, "new match", "new matches"))
	}
	if c.followers > 0 {
		lines = append(lines, plural(c.followers, "new follower", "new followers"))
	}
	if c.replies > 0 {
		lines = append(lines, plural(c.replies, "reply to your posts and videos", "replies to your posts and videos"))
	}
	body := "Here is your week on ConnectMe:\r\n\r\n- " + strings.Join(lines, "\r\n- ") + "\r\n\r\nOpen the app to see who.\r\n\r\n" +
		"You get this because you turned on the weekly summary. Turn it off in one tap:\r\n" + s.appURL + "/digest/unsubscribe?token=" + s.token(userID) + "\r\n"
	return "Your week on ConnectMe", body
}

// Run sends the digest to everyone who is due and has something to report. A
// member with nothing to report is skipped quietly (and not marked, so the
// next quiet week still counts from the same point).
func (s *Service) Run(ctx context.Context, limit int) (int, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, email, COALESCE(last_digest_at, created_at)
		  FROM users
		 WHERE weekly_digest AND is_verified AND NOT is_banned
		   AND (last_digest_at IS NULL OR last_digest_at <= $1)
		 ORDER BY COALESCE(last_digest_at, to_timestamp(0)) LIMIT $2`, s.now().Add(-7*24*time.Hour), limit)
	if err != nil {
		return 0, err
	}
	type due struct {
		id, email string
		since     time.Time
	}
	var list []due
	for rows.Next() {
		var d due
		if rows.Scan(&d.id, &d.email, &d.since) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	sent := 0
	for _, d := range list {
		since := d.since
		if min := s.now().Add(-7 * 24 * time.Hour); since.Before(min) {
			since = min
		}
		c, err := s.countsFor(ctx, d.id, since)
		if err != nil {
			return sent, err
		}
		if c == (counts{}) {
			continue
		}
		subject, body := s.compose(c, d.id)
		if err := s.mail.Send(ctx, d.email, subject, body); err != nil {
			slog.Warn("digest: send failed", "err", err)
			continue
		}
		if _, err := s.db.Exec(ctx, `UPDATE users SET last_digest_at=$2 WHERE id=$1::uuid`, d.id, s.now()); err != nil {
			return sent, err
		}
		sent++
		metrics.Event("digest_sent")
	}
	return sent, nil
}

// StartJob sends due digests every interval until ctx ends.
func (s *Service) StartJob(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			started := time.Now()
			_, err := s.Run(ctx, 200)
			metrics.Job("digest", time.Since(started), err)
			if err != nil && ctx.Err() == nil {
				slog.Error("digest: run", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Routes mounts /digest: the member's switch, and the one-tap unsubscribe link
// (public; the signed token is the proof).
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.MaxBody(1 << 10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "digest:"+name, n, w, middleware.ByUser)
	}
	authed := middleware.Auth(secret)
	r.With(authed, lim("read", 120, time.Minute)).Get("/me", func(w http.ResponseWriter, r *http.Request) {
		on, err := svc.Enabled(r.Context(), middleware.UserID(r.Context()))
		if err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		middleware.WriteJSON(w, http.StatusOK, map[string]bool{"enabled": on})
	})
	r.With(authed, lim("write", 30, time.Hour)).Put("/me", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
			middleware.WriteError(w, http.StatusBadRequest, "enabled is required")
			return
		}
		if err := svc.Set(r.Context(), middleware.UserID(r.Context()), *req.Enabled); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.With(middleware.RateLimit(rdb, "digest:unsub", 20, time.Hour, middleware.ByIP)).Post("/unsubscribe", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := svc.Unsubscribe(r.Context(), req.Token); err != nil {
			middleware.WriteError(w, http.StatusNotFound, "not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return r
}
