package live

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/push"
	"connectme/internal/users"
	"connectme/internal/wallet"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
	ErrNotAllowed   = errors.New("only verified members in Open mode can go live")
	ErrEnded        = errors.New("this stream has ended")
	ErrInsufficient = wallet.ErrInsufficient
)

const (
	// HostShareNum/Den: the host receives 70% of a gift, the platform keeps 30%.
	hostSharePct = 70
	maxDuration  = 6 * time.Hour
	tokenTTL     = 2 * time.Hour
)

// TextVetter screens public text; *users.Service satisfies it.
type TextVetter interface {
	VetText(ctx context.Context, userID, kind, text string) error
}

type Service struct {
	db       *pgxpool.Pool
	provider Provider
	vetter   TextVetter
	alerts   alertSource
	notify   push.Notifier
}

// alertSource says who follows a host (follows.Service satisfies it).
type alertSource interface {
	FollowerIDs(ctx context.Context, userID string) ([]string, error)
}

// SetAlerts makes Start tell a host's followers they went live. Either may be nil.
func (s *Service) SetAlerts(f alertSource, n push.Notifier) { s.alerts, s.notify = f, n }

// alertFollowers sends one "is live" push per follower. It runs off the
// request path and never fails the stream.
func (s *Service) alertFollowers(hostID, roomID string) {
	if s.alerts == nil || s.notify == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		ids, err := s.alerts.FollowerIDs(ctx, hostID)
		if err != nil {
			slog.Warn("live: follower lookup failed", "err", err)
			return
		}
		for _, id := range ids {
			s.notify.Notify(ctx, id, push.KindLive, roomID)
		}
	}()
}

// SetVetter enables screening of in-stream chat (nil disables it).
func (s *Service) SetVetter(v TextVetter) { s.vetter = v }

func NewService(db *pgxpool.Pool, p Provider) *Service { return &Service{db: db, provider: p} }

// Room is the public view of a stream. The host is shown by public ref and
// display name only, and only Open-mode members can host.
type Room struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	HostRef   string    `json:"host_ref"`
	HostName  string    `json:"host_name"`
	StartedAt time.Time `json:"started_at"`
	Mine      bool      `json:"mine"`
	Following bool      `json:"following,omitempty"` // the caller follows the host
	Topic     string    `json:"topic"`
	Theme     string    `json:"theme"`
	Pinned    string    `json:"pinned,omitempty"`
}

// JoinInfo is what a client needs to connect to the media server.
type JoinInfo struct {
	URL        string `json:"url"`
	Token      string `json:"token"`
	RoomName   string `json:"room"`
	CanPublish bool   `json:"can_publish"`
	Cohost     bool   `json:"cohost,omitempty"`
}

// sweep ends rooms that outlived the maximum duration or whose host can no
// longer broadcast (banned, deleted accounts cascade on their own).
func (s *Service) sweep(ctx context.Context) {
	// Public in-stream data does not outlive its room by long.
	if _, err := s.db.Exec(ctx, `DELETE FROM live_rooms WHERE status='ended' AND ended_at < NOW() - INTERVAL '24 hours'`); err != nil {
		slog.Warn("live: retention delete failed", "err", err)
	}
	rows, err := s.db.Query(ctx, `
		UPDATE live_rooms r SET status='ended', ended_at=NOW()
		 WHERE status='live' AND (created_at < NOW() - $1::interval
		       OR EXISTS (SELECT 1 FROM users u WHERE u.id=r.host_id AND u.is_banned))
		RETURNING id::text`, maxDuration.String())
	if err != nil {
		slog.Warn("live: sweep failed", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			go s.close(id)
		}
	}
}

func (s *Service) close(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := s.provider.Close(ctx, id); err != nil {
		slog.Warn("live: closing media room failed", "room", id, "err", err)
	}
}

// Start opens a room for the caller.
func (s *Service) Start(ctx context.Context, userID, title string) (*Room, *JoinInfo, error) {
	return s.StartWithTopic(ctx, userID, title, "chat")
}

// Topics are the categories a stream can be filed under.
var Topics = []string{"chat", "music", "talent", "gaming", "dating", "wellness", "learning"}

func validTopic(t string) bool {
	for _, x := range Topics {
		if x == t {
			return true
		}
	}
	return false
}

// StartWithTopic opens a room for the caller under a topic.
func (s *Service) StartWithTopic(ctx context.Context, userID, title, topic string) (*Room, *JoinInfo, error) {
	return s.StartThemed(ctx, userID, title, topic, "plain")
}

// Themes are the looks a room can have.
var Themes = []string{"plain", "sunset", "neon", "forest", "ocean", "velvet"}

func validTheme(t string) bool {
	for _, x := range Themes {
		if x == t {
			return true
		}
	}
	return false
}

// StartThemed opens a room with a topic and a look.
func (s *Service) StartThemed(ctx context.Context, userID, title, topic, theme string) (*Room, *JoinInfo, error) {
	if theme == "" {
		theme = "plain"
	}
	if !validTheme(theme) {
		return nil, nil, users.ValidationError{Msg: "unknown theme"}
	}
	if topic == "" {
		topic = "chat"
	}
	if !validTopic(topic) {
		return nil, nil, users.ValidationError{Msg: "unknown topic"}
	}
	title = strings.TrimSpace(title)
	if n := utf8.RuneCountInString(title); n < 3 || n > 80 {
		return nil, nil, users.ValidationError{Msg: "title must be 3-80 characters"}
	}
	for _, c := range title {
		if unicode.IsControl(c) {
			return nil, nil, users.ValidationError{Msg: "title contains invalid characters"}
		}
	}
	var ok bool
	err := s.db.QueryRow(ctx, `
		SELECT u.is_verified AND NOT u.is_banned AND p.mode='open'
		  FROM users u JOIN profiles p ON p.user_id=u.id WHERE u.id=$1`, userID).Scan(&ok)
	if err != nil || !ok {
		return nil, nil, ErrNotAllowed
	}
	s.sweep(ctx)
	var rm Room
	err = s.db.QueryRow(ctx, `
		WITH ins AS (
		  INSERT INTO live_rooms (host_id, title, topic, theme) VALUES ($1,$2,$3,$4)
		  ON CONFLICT (host_id) WHERE status='live' DO NOTHING
		  RETURNING id, host_id, title, topic, theme, created_at)
		SELECT i.id::text, i.title, p.public_ref::text, COALESCE(NULLIF(p.display_name,''),'Someone'), i.created_at, i.topic, i.theme
		  FROM ins i JOIN profiles p ON p.user_id=i.host_id`, userID, title, topic, theme).
		Scan(&rm.ID, &rm.Title, &rm.HostRef, &rm.HostName, &rm.StartedAt, &rm.Topic, &rm.Theme)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, users.ValidationError{Msg: "you are already live; end that stream first"}
	}
	if err != nil {
		return nil, nil, err
	}
	rm.Mine = true
	s.alertFollowers(userID, rm.ID)
	tok, err := s.provider.Token(rm.ID, userID, true, tokenTTL)
	if err != nil {
		return nil, nil, err
	}
	return &rm, &JoinInfo{URL: s.provider.URL(), Token: tok, RoomName: rm.ID, CanPublish: true}, nil
}

// List returns live rooms the caller may see (no banned hosts, no blocks in
// either direction).
func (s *Service) List(ctx context.Context, userID string) ([]Room, error) {
	return s.ListFiltered(ctx, userID, "", false)
}

// ListFiltered is List narrowed to a topic and/or to hosts the caller follows.
func (s *Service) ListFiltered(ctx context.Context, userID, topic string, followingOnly bool) ([]Room, error) {
	if topic != "" && !validTopic(topic) {
		return nil, users.ValidationError{Msg: "unknown topic"}
	}
	s.sweep(ctx)
	rows, err := s.db.Query(ctx, `
		SELECT r.id::text, r.title, p.public_ref::text, COALESCE(NULLIF(p.display_name,''),'Someone'), r.created_at, r.host_id=$1,
		       EXISTS (SELECT 1 FROM follows f WHERE f.follower_id=$1 AND f.followee_id=r.host_id), r.topic, r.pinned, r.theme
		  FROM live_rooms r
		  JOIN users u ON u.id=r.host_id AND NOT u.is_banned
		  JOIN profiles p ON p.user_id=r.host_id
		 WHERE r.status='live'
		   AND ($2::text = '' OR r.topic = $2)
		   AND (NOT $3::boolean OR EXISTS (SELECT 1 FROM follows f WHERE f.follower_id=$1 AND f.followee_id=r.host_id))
		   AND NOT EXISTS (SELECT 1 FROM blocks b WHERE
		        (b.blocker_id=$1 AND b.blocked_id=r.host_id) OR (b.blocker_id=r.host_id AND b.blocked_id=$1))
		 ORDER BY EXISTS (SELECT 1 FROM follows f WHERE f.follower_id=$1 AND f.followee_id=r.host_id) DESC, r.created_at DESC LIMIT 50`, userID, topic, followingOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Room{}
	for rows.Next() {
		var r Room
		if err := rows.Scan(&r.ID, &r.Title, &r.HostRef, &r.HostName, &r.StartedAt, &r.Mine, &r.Following, &r.Topic, &r.Pinned, &r.Theme); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// visible loads a live room the caller is allowed to enter.
func (s *Service) visible(ctx context.Context, userID, roomID string) (hostID string, err error) {
	if _, perr := uuid.Parse(roomID); perr != nil {
		return "", ErrNotFound
	}
	var status string
	var banned bool
	err = s.db.QueryRow(ctx, `
		SELECT r.host_id::text, r.status, u.is_banned FROM live_rooms r JOIN users u ON u.id=r.host_id
		 WHERE r.id=$1::uuid AND NOT EXISTS (SELECT 1 FROM blocks b WHERE
		        (b.blocker_id=$2 AND b.blocked_id=r.host_id) OR (b.blocker_id=r.host_id AND b.blocked_id=$2))
		   AND NOT EXISTS (SELECT 1 FROM live_room_bans x WHERE x.room_id=r.id AND x.user_id=$2 AND x.kind='kick')`,
		roomID, userID).Scan(&hostID, &status, &banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if status != "live" || banned {
		return "", ErrEnded
	}
	return hostID, nil
}

// Join returns a subscribe-only token for a viewer (publish for the host).
func (s *Service) Join(ctx context.Context, userID, roomID string) (*JoinInfo, error) {
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, userID).Scan(&ok); err != nil || !ok {
		return nil, ErrForbidden
	}
	hostID, err := s.visible(ctx, userID, roomID)
	if err != nil {
		return nil, err
	}
	publish := hostID == userID || s.isCohost(ctx, roomID, userID)
	tok, err := s.provider.Token(roomID, userID, publish, tokenTTL)
	if err != nil {
		return nil, err
	}
	return &JoinInfo{URL: s.provider.URL(), Token: tok, RoomName: roomID, CanPublish: publish, Cohost: publish && hostID != userID}, nil
}

// End closes a room; only its host (or staff via moderation) may do so.
func (s *Service) End(ctx context.Context, userID, roomID string) error {
	if _, err := uuid.Parse(roomID); err != nil {
		return ErrNotFound
	}
	var host string
	err := s.db.QueryRow(ctx, `
		UPDATE live_rooms SET status='ended', ended_at=NOW()
		 WHERE id=$1::uuid AND host_id=$2 AND status='live' RETURNING host_id::text`, roomID, userID).Scan(&host)
	if errors.Is(err, pgx.ErrNoRows) {
		// Distinguish "not yours" from "already ended" without leaking others' rooms.
		var n int
		_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM live_rooms WHERE id=$1::uuid AND host_id=$2`, roomID, userID).Scan(&n)
		if n == 1 {
			return nil // already ended: idempotent
		}
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	go s.close(roomID)
	return nil
}
