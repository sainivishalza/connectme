package live

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
}

// JoinInfo is what a client needs to connect to the media server.
type JoinInfo struct {
	URL        string `json:"url"`
	Token      string `json:"token"`
	RoomName   string `json:"room"`
	CanPublish bool   `json:"can_publish"`
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
		  INSERT INTO live_rooms (host_id, title) VALUES ($1,$2)
		  ON CONFLICT (host_id) WHERE status='live' DO NOTHING
		  RETURNING id, host_id, title, created_at)
		SELECT i.id::text, i.title, p.public_ref::text, p.display_name, i.created_at
		  FROM ins i JOIN profiles p ON p.user_id=i.host_id`, userID, title).
		Scan(&rm.ID, &rm.Title, &rm.HostRef, &rm.HostName, &rm.StartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, users.ValidationError{Msg: "you are already live; end that stream first"}
	}
	if err != nil {
		return nil, nil, err
	}
	rm.Mine = true
	tok, err := s.provider.Token(rm.ID, userID, true, tokenTTL)
	if err != nil {
		return nil, nil, err
	}
	return &rm, &JoinInfo{URL: s.provider.URL(), Token: tok, RoomName: rm.ID, CanPublish: true}, nil
}

// List returns live rooms the caller may see (no banned hosts, no blocks in
// either direction).
func (s *Service) List(ctx context.Context, userID string) ([]Room, error) {
	s.sweep(ctx)
	rows, err := s.db.Query(ctx, `
		SELECT r.id::text, r.title, p.public_ref::text, p.display_name, r.created_at, r.host_id=$1
		  FROM live_rooms r
		  JOIN users u ON u.id=r.host_id AND NOT u.is_banned
		  JOIN profiles p ON p.user_id=r.host_id
		 WHERE r.status='live'
		   AND NOT EXISTS (SELECT 1 FROM blocks b WHERE
		        (b.blocker_id=$1 AND b.blocked_id=r.host_id) OR (b.blocker_id=r.host_id AND b.blocked_id=$1))
		 ORDER BY r.created_at DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Room{}
	for rows.Next() {
		var r Room
		if err := rows.Scan(&r.ID, &r.Title, &r.HostRef, &r.HostName, &r.StartedAt, &r.Mine); err != nil {
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
	publish := hostID == userID
	tok, err := s.provider.Token(roomID, userID, publish, tokenTTL)
	if err != nil {
		return nil, err
	}
	return &JoinInfo{URL: s.provider.URL(), Token: tok, RoomName: roomID, CanPublish: publish}, nil
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
