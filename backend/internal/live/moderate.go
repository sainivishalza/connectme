package live

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Moderate lets a room's host mute (no chat) or kick (no access) a viewer.
// Both are scoped to the room and disappear with it.
func (s *Service) Moderate(ctx context.Context, hostID, roomID, ref, action string) error {
	if action != "mute" && action != "kick" {
		return users.ValidationError{Msg: "action must be mute or kick"}
	}
	if _, err := uuid.Parse(roomID); err != nil {
		return ErrNotFound
	}
	if _, err := uuid.Parse(ref); err != nil {
		return ErrNotFound
	}
	owns, err := s.canRun(ctx, roomID, hostID)
	if err != nil {
		return err
	}
	if !owns {
		return ErrNotFound
	}
	var target string
	err = s.db.QueryRow(ctx, `SELECT user_id::text FROM profiles WHERE public_ref=$1::uuid`, ref).Scan(&target)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if target == hostID {
		return users.ValidationError{Msg: "you cannot moderate yourself"}
	}
	var roomHost string
	_ = s.db.QueryRow(ctx, `SELECT host_id::text FROM live_rooms WHERE id=$1::uuid`, roomID).Scan(&roomHost)
	if target == roomHost {
		return users.ValidationError{Msg: "you cannot moderate the host"}
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO live_room_bans (room_id, user_id, kind) VALUES ($1::uuid,$2,$3)
		ON CONFLICT (room_id, user_id) DO UPDATE SET kind =
		  CASE WHEN live_room_bans.kind='kick' THEN 'kick' ELSE EXCLUDED.kind END`,
		roomID, target, action); err != nil {
		return err
	}
	if action == "kick" {
		go func() {
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.provider.Remove(c, roomID, target)
		}()
	}
	return nil
}

const maxPinRunes = 120

// Pin sets (or, with empty text, clears) the line pinned at the top of the
// caller's own live room. The text is screened like chat.
func (s *Service) Pin(ctx context.Context, hostID, roomID, text string) error {
	if _, err := uuid.Parse(roomID); err != nil {
		return ErrNotFound
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > maxPinRunes {
		return users.ValidationError{Msg: "a pinned message is at most 120 characters"}
	}
	for _, c := range text {
		if unicode.IsControl(c) {
			return users.ValidationError{Msg: "message contains invalid characters"}
		}
	}
	if text != "" && s.vetter != nil {
		if err := s.vetter.VetText(ctx, hostID, "live_chat", text); err != nil {
			return err
		}
	}
	ok, err := s.canRun(ctx, roomID, hostID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	_, err = s.db.Exec(ctx, `UPDATE live_rooms SET pinned=$2 WHERE id=$1::uuid`, roomID, text)
	return err
}

// Pinned returns the room's pinned line for someone who may watch it.
func (s *Service) Pinned(ctx context.Context, roomID string) string {
	var p string
	_ = s.db.QueryRow(ctx, `SELECT pinned FROM live_rooms WHERE id=$1::uuid`, roomID).Scan(&p)
	return p
}
