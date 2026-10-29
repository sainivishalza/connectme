// Package audio implements audio rooms: voice-only rooms on LiveKit where the
// host decides who may speak. Anonymous lounges show made-up aliases only.
//
// Privacy rule: a person's real display name is shown only in a named room and
// only when they are in Open mode; everyone else, always, gets an alias.
package audio

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/livekit"
	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrForbidden  = errors.New("forbidden")
	ErrNotAllowed = errors.New("verified members in Open or Incognito mode can use audio rooms")
	ErrEnded      = errors.New("this room has ended")
)

const (
	maxMembers  = 200
	maxSpeakers = 8
	maxDuration = 4 * time.Hour
	staleAfter  = 90 * time.Second
	tokenTTL    = 2 * time.Hour
)

// Permitter is implemented by providers that can change a connected
// participant's publish permission (livekit.Client does).
type Permitter interface {
	SetPublish(ctx context.Context, room, identity string, can bool) error
}

// TextVetter screens room titles; *users.Service satisfies it.
type TextVetter interface {
	VetText(ctx context.Context, userID, kind, text string) error
}

type Service struct {
	db       *pgxpool.Pool
	provider livekit.Provider
	vetter   TextVetter
}

func NewService(db *pgxpool.Pool, p livekit.Provider, v TextVetter) *Service {
	return &Service{db: db, provider: p, vetter: v}
}

type Room struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Anonymous bool      `json:"anonymous"`
	Topic     string    `json:"topic"`
	Speakers  int       `json:"speakers"`
	Listeners int       `json:"listeners"`
	StartedAt time.Time `json:"started_at"`
	Mine      bool      `json:"mine"`
}

type Member struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Role       string `json:"role"`
	HandRaised bool   `json:"hand_raised"`
	Me         bool   `json:"me"`
}

type Me struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

type JoinInfo struct {
	URL        string `json:"url"`
	Token      string `json:"token"`
	RoomName   string `json:"room"`
	CanPublish bool   `json:"can_publish"`
	Me         Me     `json:"me"`
}

var adjectives = []string{"Quiet", "Golden", "Brave", "Curious", "Gentle", "Sunny", "Midnight", "Velvet", "Wandering", "Cosy", "Bright", "Calm", "Lucky", "Misty", "Amber", "Silver"}
var animals = []string{"Otter", "Heron", "Fox", "Lynx", "Panda", "Finch", "Gecko", "Moth", "Koala", "Swan", "Hare", "Wren", "Seal", "Newt", "Robin", "Ibis"}

func pick(list []string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		return list[0]
	}
	return list[n.Int64()]
}

func newAlias() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(90))
	return pick(adjectives) + " " + pick(animals) + " " + strconv.FormatInt(n.Int64()+10, 10)
}

func (s *Service) closeMedia(room string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := s.provider.Close(ctx, room); err != nil {
		slog.Warn("audio: closing media room failed", "room", room, "err", err)
	}
}

// sweep drops members that stopped polling, ends over-long rooms and rooms
// whose host is banned or gone, and deletes old ended rooms.
func (s *Service) sweep(ctx context.Context) {
	if _, err := s.db.Exec(ctx, `UPDATE audio_members SET left_at=NOW() WHERE left_at IS NULL AND last_seen < NOW() - $1::interval`, staleAfter.String()); err != nil {
		slog.Warn("audio: stale sweep failed", "err", err)
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM audio_rooms WHERE status='ended' AND ended_at < NOW() - INTERVAL '24 hours'`); err != nil {
		slog.Warn("audio: retention delete failed", "err", err)
	}
	rows, err := s.db.Query(ctx, `
		UPDATE audio_rooms r SET status='ended', ended_at=NOW()
		 WHERE status='live' AND (created_at < NOW() - $1::interval
		       OR EXISTS (SELECT 1 FROM users u WHERE u.id=r.host_id AND u.is_banned)
		       OR NOT EXISTS (SELECT 1 FROM audio_members m WHERE m.room_id=r.id AND m.role='host' AND m.left_at IS NULL))
		RETURNING id::text`, maxDuration.String())
	if err != nil {
		slog.Warn("audio: sweep failed", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			go s.closeMedia(id)
		}
	}
}

type profile struct {
	ok   bool
	mode string
	name string
}

func (s *Service) who(ctx context.Context, userID string) (profile, error) {
	var p profile
	err := s.db.QueryRow(ctx, `
		SELECT u.is_verified AND NOT u.is_banned, p.mode, p.display_name
		  FROM users u JOIN profiles p ON p.user_id=u.id WHERE u.id=$1`, userID).Scan(&p.ok, &p.mode, &p.name)
	if err != nil || !p.ok || (p.mode != "open" && p.mode != "incognito") {
		return p, ErrNotAllowed
	}
	return p, nil
}

func label(p profile, anonymous bool) string {
	if !anonymous && p.mode == "open" {
		return p.name
	}
	return newAlias()
}

// Start opens a room; the caller becomes its host.
func (s *Service) Start(ctx context.Context, userID, title string, anonymous bool) (*Room, *JoinInfo, error) {
	return s.StartWithTopic(ctx, userID, title, "open", anonymous)
}

// Topics are the subjects a voice room can be filed under.
var Topics = []string{"open", "love", "stories", "music", "advice", "nightowls", "confess"}

func validTopic(t string) bool {
	for _, x := range Topics {
		if x == t {
			return true
		}
	}
	return false
}

// StartWithTopic opens a room under a topic; the caller becomes its host.
func (s *Service) StartWithTopic(ctx context.Context, userID, title, topic string, anonymous bool) (*Room, *JoinInfo, error) {
	if topic == "" {
		topic = "open"
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
	p, err := s.who(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if p.mode == "incognito" {
		anonymous = true // never attach a name to an Incognito member
	}
	if s.vetter != nil {
		if err := s.vetter.VetText(ctx, userID, "group", title); err != nil {
			return nil, nil, err
		}
	}
	s.sweep(ctx)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var rm Room
	err = tx.QueryRow(ctx, `
		INSERT INTO audio_rooms (host_id, title, anonymous, topic) VALUES ($1,$2,$3,$4)
		ON CONFLICT (host_id) WHERE status='live' DO NOTHING
		RETURNING id::text, title, anonymous, created_at, topic`, userID, title, anonymous, topic).
		Scan(&rm.ID, &rm.Title, &rm.Anonymous, &rm.StartedAt, &rm.Topic)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, users.ValidationError{Msg: "you already host a room; end it first"}
	}
	if err != nil {
		return nil, nil, err
	}
	var memberID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO audio_members (room_id, user_id, role, alias) VALUES ($1::uuid,$2,'host',$3) RETURNING id::text`,
		rm.ID, userID, label(p, anonymous)).Scan(&memberID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	rm.Mine, rm.Speakers = true, 1
	tok, err := s.provider.Token(rm.ID, memberID, true, tokenTTL)
	if err != nil {
		return nil, nil, err
	}
	return &rm, &JoinInfo{URL: s.provider.URL(), Token: tok, RoomName: rm.ID, CanPublish: true, Me: Me{ID: memberID, Role: "host"}}, nil
}

// List returns live rooms the caller may enter.
func (s *Service) List(ctx context.Context, userID string) ([]Room, error) {
	return s.ListTopic(ctx, userID, "")
}

// ListTopic is List narrowed to one topic ("" = all).
func (s *Service) ListTopic(ctx context.Context, userID, topic string) ([]Room, error) {
	if topic != "" && !validTopic(topic) {
		return nil, users.ValidationError{Msg: "unknown topic"}
	}
	if _, err := s.who(ctx, userID); err != nil {
		return nil, err
	}
	s.sweep(ctx)
	rows, err := s.db.Query(ctx, `
		SELECT r.id::text, r.title, r.anonymous, r.created_at, r.host_id=$1, r.topic,
		       COUNT(m.id) FILTER (WHERE m.role IN ('host','speaker')),
		       COUNT(m.id) FILTER (WHERE m.role='listener')
		  FROM audio_rooms r
		  JOIN users u ON u.id=r.host_id AND NOT u.is_banned
		  LEFT JOIN audio_members m ON m.room_id=r.id AND m.left_at IS NULL
		 WHERE r.status='live' AND ($2::text = '' OR r.topic = $2)
		   AND NOT EXISTS (SELECT 1 FROM blocks b WHERE
		        (b.blocker_id=$1 AND b.blocked_id=r.host_id) OR (b.blocker_id=r.host_id AND b.blocked_id=$1))
		 GROUP BY r.id ORDER BY r.created_at DESC LIMIT 50`, userID, topic)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Room{}
	for rows.Next() {
		var r Room
		if err := rows.Scan(&r.ID, &r.Title, &r.Anonymous, &r.StartedAt, &r.Mine, &r.Topic, &r.Speakers, &r.Listeners); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func parse(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	return nil
}

// Join enters a room (or re-enters after dropping) and returns a token. Only
// the host and promoted speakers can publish.
func (s *Service) Join(ctx context.Context, userID, roomID string) (*JoinInfo, error) {
	if err := parse(roomID); err != nil {
		return nil, err
	}
	p, err := s.who(ctx, userID)
	if err != nil {
		return nil, err
	}
	var anonymous, banned bool
	var status string
	err = s.db.QueryRow(ctx, `
		SELECT r.anonymous, r.status, u.is_banned FROM audio_rooms r JOIN users u ON u.id=r.host_id
		 WHERE r.id=$1::uuid AND NOT EXISTS (SELECT 1 FROM blocks b WHERE
		        (b.blocker_id=$2 AND b.blocked_id=r.host_id) OR (b.blocker_id=r.host_id AND b.blocked_id=$2))`,
		roomID, userID).Scan(&anonymous, &status, &banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "live" || banned {
		return nil, ErrEnded
	}
	var active int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM audio_members WHERE room_id=$1::uuid AND left_at IS NULL`, roomID).Scan(&active); err != nil {
		return nil, err
	}
	var me Me
	var kicked bool
	err = s.db.QueryRow(ctx, `
		INSERT INTO audio_members (room_id, user_id, role, alias) VALUES ($1::uuid,$2,'listener',$3)
		ON CONFLICT (room_id, user_id) DO UPDATE SET left_at=NULL, last_seen=NOW()
		RETURNING id::text, role, kicked`, roomID, userID, label(p, anonymous)).Scan(&me.ID, &me.Role, &kicked)
	if err != nil {
		return nil, err
	}
	if kicked {
		return nil, ErrNotFound
	}
	if active >= maxMembers && me.Role == "listener" {
		_, _ = s.db.Exec(ctx, `UPDATE audio_members SET left_at=NOW() WHERE id=$1::uuid`, me.ID)
		return nil, users.ValidationError{Msg: "this room is full"}
	}
	publish := me.Role == "host" || me.Role == "speaker"
	tok, err := s.provider.Token(roomID, me.ID, publish, tokenTTL)
	if err != nil {
		return nil, err
	}
	return &JoinInfo{URL: s.provider.URL(), Token: tok, RoomName: roomID, CanPublish: publish, Me: me}, nil
}

// State lists who is in the room. Polling it doubles as the presence signal.
func (s *Service) State(ctx context.Context, userID, roomID string) (*Room, []Member, *Me, error) {
	if err := parse(roomID); err != nil {
		return nil, nil, nil, err
	}
	var me Me
	var rm Room
	var status, host string
	err := s.db.QueryRow(ctx, `
		UPDATE audio_members m SET last_seen=NOW() FROM audio_rooms r
		 WHERE m.room_id=r.id AND r.id=$1::uuid AND m.user_id=$2 AND m.left_at IS NULL AND NOT m.kicked
		RETURNING m.id::text, m.role, r.title, r.anonymous, r.status, r.created_at, r.host_id::text, r.topic`, roomID, userID).
		Scan(&me.ID, &me.Role, &rm.Title, &rm.Anonymous, &status, &rm.StartedAt, &host, &rm.Topic)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if status != "live" {
		return nil, nil, nil, ErrEnded
	}
	rm.ID, rm.Mine = roomID, host == userID
	rows, err := s.db.Query(ctx, `
		SELECT id::text, alias, role, hand_raised, user_id=$2 FROM audio_members
		 WHERE room_id=$1::uuid AND left_at IS NULL
		 ORDER BY (role='host') DESC, (role='speaker') DESC, hand_raised DESC, joined_at LIMIT $3`, roomID, userID, maxMembers)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	ms := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Label, &m.Role, &m.HandRaised, &m.Me); err != nil {
			return nil, nil, nil, err
		}
		if m.Role == "listener" {
			rm.Listeners++
		} else {
			rm.Speakers++
		}
		ms = append(ms, m)
	}
	return &rm, ms, &me, rows.Err()
}

// RaiseHand asks the host for the mic; listeners only.
func (s *Service) RaiseHand(ctx context.Context, userID, roomID string, up bool) error {
	if err := parse(roomID); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE audio_members SET hand_raised=$3 WHERE room_id=$1::uuid AND user_id=$2 AND role='listener'
		   AND left_at IS NULL AND EXISTS (SELECT 1 FROM audio_rooms WHERE id=$1::uuid AND status='live')`, roomID, userID, up)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) hostOwns(ctx context.Context, hostID, roomID string) error {
	if err := parse(roomID); err != nil {
		return err
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM audio_rooms WHERE id=$1::uuid AND host_id=$2 AND status='live')`, roomID, hostID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

func (s *Service) setPublish(roomID, memberID string, can bool) {
	pm, ok := s.provider.(Permitter)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pm.SetPublish(ctx, roomID, memberID, can); err != nil {
		slog.Warn("audio: permission change failed", "room", roomID, "err", err)
	}
}

// SetRole lets the host move one member between listener and speaker, or
// remove them. action is "promote", "demote" or "kick".
func (s *Service) SetRole(ctx context.Context, hostID, roomID, memberID, action string) error {
	if action != "promote" && action != "demote" && action != "kick" {
		return users.ValidationError{Msg: "action must be promote, demote or kick"}
	}
	if err := s.hostOwns(ctx, hostID, roomID); err != nil {
		return err
	}
	if err := parse(memberID); err != nil {
		return err
	}
	switch action {
	case "promote":
		var speakers int
		if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM audio_members WHERE room_id=$1::uuid AND left_at IS NULL AND role IN ('host','speaker')`, roomID).Scan(&speakers); err != nil {
			return err
		}
		if speakers >= maxSpeakers {
			return users.ValidationError{Msg: "the stage is full; ask a speaker to step down first"}
		}
		tag, err := s.db.Exec(ctx, `UPDATE audio_members SET role='speaker', hand_raised=FALSE WHERE id=$1::uuid AND room_id=$2::uuid AND role='listener' AND left_at IS NULL`, memberID, roomID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		go s.setPublish(roomID, memberID, true)
	case "demote":
		tag, err := s.db.Exec(ctx, `UPDATE audio_members SET role='listener' WHERE id=$1::uuid AND room_id=$2::uuid AND role='speaker' AND left_at IS NULL`, memberID, roomID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		go s.setPublish(roomID, memberID, false)
	case "kick":
		tag, err := s.db.Exec(ctx, `UPDATE audio_members SET kicked=TRUE, left_at=NOW() WHERE id=$1::uuid AND room_id=$2::uuid AND role<>'host'`, memberID, roomID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		go func() {
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.provider.Remove(c, roomID, memberID)
		}()
	}
	return nil
}

// Leave removes the caller; a host leaving ends the room.
func (s *Service) Leave(ctx context.Context, userID, roomID string) error {
	if err := parse(roomID); err != nil {
		return err
	}
	var role string
	err := s.db.QueryRow(ctx, `UPDATE audio_members SET left_at=NOW(), hand_raised=FALSE WHERE room_id=$1::uuid AND user_id=$2 AND left_at IS NULL RETURNING role`, roomID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // idempotent
	}
	if err != nil {
		return err
	}
	if role == "host" {
		return s.End(ctx, userID, roomID)
	}
	return nil
}

// End closes a room; only its host (or staff via a ban) may.
func (s *Service) End(ctx context.Context, userID, roomID string) error {
	if err := parse(roomID); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `UPDATE audio_rooms SET status='ended', ended_at=NOW() WHERE id=$1::uuid AND host_id=$2 AND status='live'`, roomID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var n int
		_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM audio_rooms WHERE id=$1::uuid AND host_id=$2`, roomID, userID).Scan(&n)
		if n == 1 {
			return nil
		}
		return ErrNotFound
	}
	go s.closeMedia(roomID)
	return nil
}

// EndAllFor ends every room a banned user hosts and removes them elsewhere.
func (s *Service) EndAllFor(ctx context.Context, userID string) {
	rows, err := s.db.Query(ctx, `UPDATE audio_rooms SET status='ended', ended_at=NOW() WHERE host_id=$1::uuid AND status='live' RETURNING id::text`, userID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				go s.closeMedia(id)
			}
		}
	}
	_, _ = s.db.Exec(ctx, `UPDATE audio_members SET left_at=NOW() WHERE user_id=$1::uuid AND left_at IS NULL`, userID)
}
