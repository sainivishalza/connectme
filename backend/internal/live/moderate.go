package live

import (
	"context"
	"errors"
	"time"

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
	var owns bool
	if err := s.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM live_rooms WHERE id=$1::uuid AND host_id=$2 AND status='live')`,
		roomID, hostID).Scan(&owns); err != nil {
		return err
	}
	if !owns {
		return ErrNotFound
	}
	var target string
	err := s.db.QueryRow(ctx, `SELECT user_id::text FROM profiles WHERE public_ref=$1::uuid`, ref).Scan(&target)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if target == hostID {
		return users.ValidationError{Msg: "you cannot moderate yourself"}
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
