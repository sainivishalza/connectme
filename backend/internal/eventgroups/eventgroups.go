// Package eventgroups is the board for people going to the same partner event.
// Having an RSVP is the ticket in: nobody else can read or write. Names appear
// only for members shown in full; everyone else is "Someone". The board is
// deleted a day after the event ends.
package eventgroups

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/users"
	"connectme/pkg/metrics"
	"connectme/pkg/middleware"
	"connectme/pkg/utils"

	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var ErrNotFound = errors.New("not found")

const (
	maxPerDay   = 60
	maxPerEvent = 500
	keepAfter   = 24 * time.Hour
)

type TextVetter interface {
	VetText(ctx context.Context, userID, kind, text string) error
}

type Service struct {
	db     *pgxpool.Pool
	vetter TextVetter
}

func NewService(db *pgxpool.Pool, v TextVetter) *Service { return &Service{db: db, vetter: v} }

type Message struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	Name      string    `json:"name"`
	Mine      bool      `json:"mine"`
	CreatedAt time.Time `json:"created_at"`
}

type Member struct {
	Name string `json:"name"`
	Ref  string `json:"ref,omitempty"`
}

type Board struct {
	EventID  string    `json:"event_id"`
	Title    string    `json:"title"`
	StartsAt time.Time `json:"starts_at"`
	Going    int       `json:"going"`
	Members  []Member  `json:"members"` // only those shown in full
	Messages []Message `json:"messages"`
}

// access checks the caller has a spot at a live event and returns its facts.
func (s *Service) access(ctx context.Context, userID, eventID string) (title string, starts time.Time, err error) {
	if _, perr := uuid.Parse(eventID); perr != nil {
		return "", time.Time{}, ErrNotFound
	}
	err = s.db.QueryRow(ctx, `
		SELECT e.title, e.starts_at FROM events e JOIN event_rsvps r ON r.event_id = e.id AND r.user_id = $2::uuid
		 WHERE e.id = $1::uuid AND e.status = 'scheduled' AND e.ends_at > NOW() - $3::interval`, eventID, userID, keepAfter.String()).Scan(&title, &starts)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, ErrNotFound
	}
	return title, starts, err
}

// Get returns the board: who is going and the latest messages.
func (s *Service) Get(ctx context.Context, userID, eventID string) (*Board, error) {
	title, starts, err := s.access(ctx, userID, eventID)
	if err != nil {
		return nil, err
	}
	b := &Board{EventID: eventID, Title: title, StartsAt: starts, Members: []Member{}, Messages: []Message{}}
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM event_rsvps WHERE event_id=$1::uuid`, eventID).Scan(&b.Going); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT COALESCE(p.display_name,''), p.public_ref::text
		  FROM event_rsvps r JOIN profiles p ON p.user_id = r.user_id JOIN users u ON u.id = r.user_id AND NOT u.is_banned
		 WHERE r.event_id=$1::uuid AND p.mode='open' AND NOT p.invisible AND r.user_id <> $2::uuid
		   AND `+users.NotBlockedSQL("r.user_id", "$2::uuid")+`
		 ORDER BY r.created_at LIMIT 50`, eventID, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m Member
		if rows.Scan(&m.Name, &m.Ref) == nil && m.Name != "" {
			b.Members = append(b.Members, m)
		}
	}
	rows.Close()
	mrows, err := s.db.Query(ctx, `
		SELECT * FROM (
		  SELECT m.id::text, m.body,
		         CASE WHEN p.mode='open' AND NOT p.invisible AND p.display_name IS NOT NULL THEN p.display_name ELSE 'Someone' END,
		         m.author_id = $2::uuid, m.created_at
		    FROM event_messages m JOIN profiles p ON p.user_id = m.author_id JOIN users u ON u.id = m.author_id AND NOT u.is_banned
		   WHERE m.event_id=$1::uuid AND (m.author_id = $2::uuid OR `+users.NotBlockedSQL("m.author_id", "$2::uuid")+`)
		   ORDER BY m.created_at DESC LIMIT 100) t ORDER BY created_at`, eventID, userID)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var m Message
		if err := mrows.Scan(&m.ID, &m.Body, &m.Name, &m.Mine, &m.CreatedAt); err != nil {
			return nil, err
		}
		b.Messages = append(b.Messages, m)
	}
	return b, mrows.Err()
}

// Post adds a message to the board.
func (s *Service) Post(ctx context.Context, userID, eventID, body string) error {
	if _, _, err := s.access(ctx, userID, eventID); err != nil {
		return err
	}
	body = utils.StripTags(body)
	if n := utf8.RuneCountInString(body); n < 1 || n > 280 {
		return users.ValidationError{Msg: "a message is 1 to 280 characters"}
	}
	for _, c := range body {
		if unicode.IsControl(c) && c != '\n' {
			return users.ValidationError{Msg: "the message contains invalid characters"}
		}
	}
	var today, total int
	if err := s.db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM event_messages WHERE author_id=$2::uuid AND created_at > NOW() - INTERVAL '24 hours'),
		       (SELECT COUNT(*) FROM event_messages WHERE event_id=$1::uuid)`, eventID, userID).Scan(&today, &total); err != nil {
		return err
	}
	if today >= maxPerDay || total >= maxPerEvent {
		return users.ValidationError{Msg: "this board is full for now"}
	}
	if s.vetter != nil {
		if err := s.vetter.VetText(ctx, userID, "event_group", body); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(ctx, `INSERT INTO event_messages (event_id, author_id, body) VALUES ($1::uuid,$2::uuid,$3)`, eventID, userID, body)
	return err
}

// Delete removes the caller's own message.
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM event_messages WHERE id=$1::uuid AND author_id=$2::uuid`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Report files a report against a message's author with its text as evidence.
func (s *Service) Report(ctx context.Context, userID, id, reason string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	switch reason {
	case "spam", "harassment", "inappropriate_content", "scam", "underage", "other":
	default:
		return users.ValidationError{Msg: "unknown reason"}
	}
	var eventID string
	if err := s.db.QueryRow(ctx, `SELECT event_id::text FROM event_messages WHERE id=$1::uuid`, id).Scan(&eventID); err != nil {
		return ErrNotFound
	}
	if _, _, err := s.access(ctx, userID, eventID); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason, details, evidence)
		SELECT $1::uuid, m.author_id, $3, 'event group message', jsonb_build_array(m.body)
		  FROM event_messages m WHERE m.id=$2::uuid AND m.author_id <> $1::uuid`, userID, id, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Expire deletes boards a day after their event ended.
func (s *Service) Expire(ctx context.Context) (int, error) {
	tag, err := s.db.Exec(ctx, `
		DELETE FROM event_messages m USING events e
		 WHERE e.id = m.event_id AND (e.ends_at < NOW() - $1::interval OR e.status <> 'scheduled')`, keepAfter.String())
	return int(tag.RowsAffected()), err
}

func (s *Service) StartJob(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			started := time.Now()
			n, err := s.Expire(ctx)
			metrics.Job("event_group_expiry", time.Since(started), err)
			metrics.JobItems("event_group_expiry", "deleted", n)
			if err != nil && ctx.Err() == nil {
				slog.Error("eventgroups: expiry", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Routes mounts /eventgroups.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(2<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "eventgroups:"+name, n, w, middleware.ByUser)
	}
	fail := func(w http.ResponseWriter, err error) {
		var ve users.ValidationError
		switch {
		case errors.As(err, &ve):
			middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
		case errors.Is(err, ErrNotFound):
			middleware.WriteError(w, http.StatusNotFound, "not found")
		default:
			slog.Error("eventgroups: internal error", "err", err)
			middleware.WriteError(w, http.StatusInternalServerError, "internal error")
		}
	}
	uid := func(r *http.Request) string { return middleware.UserID(r.Context()) }
	r.With(lim("read", 240, time.Minute)).Get("/{event}", func(w http.ResponseWriter, r *http.Request) {
		b, err := svc.Get(r.Context(), uid(r), chi.URLParam(r, "event"))
		if err != nil {
			fail(w, err)
			return
		}
		middleware.WriteJSON(w, http.StatusOK, b)
	})
	r.With(lim("write", 60, time.Hour)).Post("/{event}/messages", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := svc.Post(r.Context(), uid(r), chi.URLParam(r, "event"), req.Body); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.With(lim("write", 120, time.Hour)).Delete("/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := svc.Delete(r.Context(), uid(r), chi.URLParam(r, "id")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.With(lim("report", 30, time.Hour)).Post("/messages/{id}/report", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := svc.Report(r.Context(), uid(r), chi.URLParam(r, "id"), req.Reason); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return r
}
