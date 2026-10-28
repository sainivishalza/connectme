package push

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"connectme/internal/users"
	"connectme/pkg/metrics"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Kind is what a notification is about.
type Kind string

const (
	KindMessage Kind = "message"
	KindMatch   Kind = "match"
	KindCall    Kind = "call"
	KindBooking Kind = "booking"
	KindLive    Kind = "live"
	KindComment Kind = "comment"
	KindSafety  Kind = "safety"
	KindReport  Kind = "report"
)

const (
	maxDevices = 10
	queueSize  = 512
	workers    = 4
	staleAfter = 90 * 24 * time.Hour
)

// Notifier is what feature packages depend on. A nil Notifier means push is
// not configured; every caller must treat it as a no-op.
type Notifier interface {
	Notify(ctx context.Context, userID string, kind Kind, ref string)
}

type job struct {
	user string
	kind Kind
	ref  string
}

type Service struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	provider Provider
	jobs     chan job
}

func NewService(db *pgxpool.Pool, rdb *redis.Client, p Provider) *Service {
	return &Service{db: db, rdb: rdb, provider: p, jobs: make(chan job, queueSize)}
}

// Start launches the delivery workers and the stale-token cleanup until ctx ends.
func (s *Service) Start(ctx context.Context) {
	for i := 0; i < workers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case j := <-s.jobs:
					c, cancel := context.WithTimeout(ctx, 20*time.Second)
					s.deliver(c, j)
					cancel()
				}
			}
		}()
	}
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			if _, err := s.db.Exec(ctx, `DELETE FROM push_devices WHERE last_seen < $1`, time.Now().Add(-staleAfter)); err != nil && ctx.Err() == nil {
				slog.Warn("push: stale token cleanup failed", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Notify queues a notification. It never blocks and never fails the caller:
// when the queue is full the notification is dropped (and counted).
func (s *Service) Notify(_ context.Context, userID string, kind Kind, ref string) {
	select {
	case s.jobs <- job{userID, kind, ref}:
	default:
		metrics.Event("push_dropped")
	}
}

// content is the whole of what a notification may say.
func content(kind Kind) Message {
	switch kind {
	case KindMessage:
		return Message{Title: "ConnectMe", Body: "You have a new message", TTLSeconds: 24 * 3600}
	case KindMatch:
		return Message{Title: "ConnectMe", Body: "You have a new match", TTLSeconds: 24 * 3600}
	case KindCall:
		return Message{Title: "ConnectMe", Body: "Incoming call", HighPrio: true, TTLSeconds: 45}
	case KindBooking:
		return Message{Title: "ConnectMe", Body: "Your booking was updated", TTLSeconds: 24 * 3600}
	case KindLive:
		return Message{Title: "ConnectMe", Body: "Someone you follow is live", TTLSeconds: 1800}
	case KindComment:
		return Message{Title: "ConnectMe", Body: "Someone replied to your post", TTLSeconds: 24 * 3600}
	case KindReport:
		return Message{Title: "ConnectMe", Body: "Your report was reviewed", TTLSeconds: 24 * 3600}
	case KindSafety:
		return Message{Title: "ConnectMe", Body: "Time to check in", HighPrio: true, TTLSeconds: 1800}
	}
	return Message{}
}

// dedupe is how long a repeat of the same (kind, ref) is swallowed: a burst of
// messages in one chat is one notification.
func dedupe(kind Kind) time.Duration {
	switch kind {
	case KindMessage:
		return 5 * time.Minute
	case KindCall:
		return 20 * time.Second
	case KindLive:
		return 30 * time.Minute
	case KindComment:
		return 10 * time.Minute
	}
	return time.Minute
}

type device struct{ id, platform, token string }

func (s *Service) deliver(ctx context.Context, j job) {
	msg := content(j.kind)
	if msg.Body == "" {
		return
	}
	if j.kind != KindCall && j.kind != KindSafety { // a safety reminder is never silenced
		if p, err := s.Prefs(ctx, j.user); err == nil && p.Quiet.Active(time.Now()) {
			metrics.Event("push_quiet")
			return
		}
	}
	rows, err := s.db.Query(ctx, `
		SELECT d.id::text, d.platform, d.token
		  FROM push_devices d
		  JOIN users u ON u.id = d.user_id AND NOT u.is_banned AND u.is_verified
		  LEFT JOIN push_prefs p ON p.user_id = d.user_id
		 WHERE d.user_id = $1::uuid
		   AND COALESCE(CASE $2::text WHEN 'message' THEN p.messages WHEN 'match' THEN p.matches
		                              WHEN 'call' THEN p.calls WHEN 'booking' THEN p.bookings WHEN 'live' THEN p.live WHEN 'comment' THEN p.comments END, TRUE)`, j.user, string(j.kind))
	if err != nil {
		slog.Warn("push: device lookup failed", "err", err)
		return
	}
	var devs []device
	for rows.Next() {
		var d device
		if rows.Scan(&d.id, &d.platform, &d.token) == nil {
			devs = append(devs, d)
		}
	}
	rows.Close()
	if len(devs) == 0 {
		return
	}
	if s.rdb != nil {
		ok, err := s.rdb.SetNX(ctx, "push:dedupe:"+j.user+":"+string(j.kind)+":"+j.ref, 1, dedupe(j.kind)).Result()
		if err == nil && !ok {
			metrics.Event("push_collapsed")
			return
		}
		// A Redis error fails open: better a duplicate than a missed call.
	}
	msg.Data = map[string]string{"kind": string(j.kind), "ref": j.ref}
	msg.Collapse = string(j.kind) + "-" + j.ref
	for _, d := range devs {
		err := s.provider.Send(ctx, d.platform, d.token, msg)
		switch {
		case err == nil:
			metrics.Event("push_sent")
		case errors.Is(err, ErrUnregistered):
			_, _ = s.db.Exec(ctx, `DELETE FROM push_devices WHERE id = $1::uuid`, d.id)
			metrics.Event("push_token_dropped")
		default:
			slog.Warn("push: send failed", "provider", s.provider.Name(), "err", err)
			metrics.Event("push_failed")
		}
	}
}

// ---- devices and preferences ----

var (
	ErrBadToken    = users.ValidationError{Msg: "that device token is not valid"}
	ErrBadPlatform = users.ValidationError{Msg: "platform must be android, ios or web"}
)

// Register stores a device token for the caller. A token belongs to whoever
// registered it last. At most ten devices are kept; the least recently seen go.
func (s *Service) Register(ctx context.Context, userID, platform, token string) error {
	token = strings.TrimSpace(token)
	if n := len(token); n < 16 || n > 1024 || strings.ContainsAny(token, " \r\n\t") {
		return ErrBadToken
	}
	if platform != "android" && platform != "ios" && platform != "web" {
		return ErrBadPlatform
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
		INSERT INTO push_devices (user_id, platform, token) VALUES ($1::uuid,$2,$3)
		ON CONFLICT (token) DO UPDATE SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform, last_seen = NOW()`,
		userID, platform, token); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM push_devices WHERE id IN (
		  SELECT id FROM push_devices WHERE user_id = $1::uuid ORDER BY last_seen DESC OFFSET $2)`, userID, maxDevices); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Unregister removes the caller's token (sign out).
func (s *Service) Unregister(ctx context.Context, userID, token string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM push_devices WHERE user_id = $1::uuid AND token = $2`, userID, token)
	return err
}

// UnregisterAll removes every device of a user (used when sessions are revoked).
func (s *Service) UnregisterAll(ctx context.Context, userID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM push_devices WHERE user_id = $1::uuid`, userID)
	return err
}

// Quiet is a daily window in which only incoming calls get through.
type Quiet struct {
	On    bool `json:"on"`
	Start int  `json:"start"`  // minutes after local midnight
	End   int  `json:"end"`    // minutes after local midnight
	TZMin int  `json:"tz_min"` // local time minus UTC, in minutes
}

// Active reports whether now falls inside the window.
func (q Quiet) Active(now time.Time) bool {
	if !q.On {
		return false
	}
	local := now.UTC().Hour()*60 + now.UTC().Minute() + q.TZMin
	m := ((local % 1440) + 1440) % 1440
	if q.Start <= q.End {
		return m >= q.Start && m < q.End
	}
	return m >= q.Start || m < q.End
}

type Prefs struct {
	Quiet    Quiet `json:"quiet"`
	Messages bool  `json:"messages"`
	Matches  bool  `json:"matches"`
	Calls    bool  `json:"calls"`
	Bookings bool  `json:"bookings"`
	Live     bool  `json:"live"`
	Comments bool  `json:"comments"`
}

func (s *Service) Prefs(ctx context.Context, userID string) (Prefs, error) {
	p := Prefs{Quiet: Quiet{Start: 1320, End: 480}, Messages: true, Matches: true, Calls: true, Bookings: true, Live: true, Comments: true}
	err := s.db.QueryRow(ctx, `SELECT messages, matches, calls, bookings, live, comments, quiet_on, quiet_start, quiet_end, tz_min FROM push_prefs WHERE user_id=$1::uuid`, userID).
		Scan(&p.Messages, &p.Matches, &p.Calls, &p.Bookings, &p.Live, &p.Comments, &p.Quiet.On, &p.Quiet.Start, &p.Quiet.End, &p.Quiet.TZMin)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return p, err
	}
	return p, nil
}

var ErrBadQuiet = users.ValidationError{Msg: "quiet hours must be times of day and a valid time zone offset"}

func (s *Service) SetPrefs(ctx context.Context, userID string, p Prefs) error {
	if q := p.Quiet; q.Start < 0 || q.Start > 1439 || q.End < 0 || q.End > 1439 || q.TZMin < -840 || q.TZMin > 840 {
		return ErrBadQuiet
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO push_prefs (user_id, messages, matches, calls, bookings, live, comments, quiet_on, quiet_start, quiet_end, tz_min) VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (user_id) DO UPDATE SET messages=$2, matches=$3, calls=$4, bookings=$5, live=$6, comments=$7,
		  quiet_on=$8, quiet_start=$9, quiet_end=$10, tz_min=$11, updated_at=NOW()`,
		userID, p.Messages, p.Matches, p.Calls, p.Bookings, p.Live, p.Comments, p.Quiet.On, p.Quiet.Start, p.Quiet.End, p.Quiet.TZMin)
	return err
}

// Devices reports how many devices the caller has registered.
func (s *Service) Devices(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM push_devices WHERE user_id=$1::uuid`, userID).Scan(&n)
	return n, err
}
