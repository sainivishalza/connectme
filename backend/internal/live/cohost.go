package live

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// maxCohosts is how many guests can be on camera with the host.
const maxCohosts = 7

// publishPermitter is implemented by providers that can change a connected
// participant's publish permission (LiveKit does).
type publishPermitter interface {
	SetPublish(ctx context.Context, room, identity string, can bool) error
}

func (s *Service) setPublish(roomID, userID string, can bool) {
	pm, ok := s.provider.(publishPermitter)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pm.SetPublish(ctx, roomID, userID, can); err != nil {
		slog.Warn("live: permission change failed", "room", roomID, "err", err)
	}
}

// isCohost reports whether userID co-hosts the room.
func (s *Service) isCohost(ctx context.Context, roomID, userID string) bool {
	var ok bool
	_ = s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM live_cohosts WHERE room_id=$1::uuid AND user_id=$2::uuid)`, roomID, userID).Scan(&ok)
	return ok
}

// canRun reports whether userID hosts or co-hosts a live room.
func (s *Service) canRun(ctx context.Context, roomID, userID string) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM live_rooms r WHERE r.id=$1::uuid AND r.status='live'
		   AND (r.host_id=$2::uuid OR EXISTS (SELECT 1 FROM live_cohosts c WHERE c.room_id=r.id AND c.user_id=$2::uuid)))`,
		roomID, userID).Scan(&ok)
	return ok, err
}

// Cohost adds or removes a co-host. Only the host can; the person must be
// shown in full (Open mode), verified and not blocked either way with the host.
func (s *Service) Cohost(ctx context.Context, hostID, roomID, ref, action string) error {
	if action != "add" && action != "remove" {
		return users.ValidationError{Msg: "action must be add or remove"}
	}
	if _, err := uuid.Parse(roomID); err != nil {
		return ErrNotFound
	}
	var owns bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM live_rooms WHERE id=$1::uuid AND host_id=$2::uuid AND status='live')`,
		roomID, hostID).Scan(&owns); err != nil {
		return err
	}
	if !owns {
		return ErrNotFound
	}
	if _, err := uuid.Parse(ref); err != nil {
		return ErrNotFound
	}
	var target string
	err := s.db.QueryRow(ctx, `
		SELECT p.user_id::text FROM profiles p JOIN users u ON u.id = p.user_id
		 WHERE p.public_ref = $1::uuid AND u.is_verified AND NOT u.is_banned AND p.mode = 'open' AND NOT p.invisible
		   AND `+users.NotBlockedSQL("p.user_id", "$2::uuid"), ref, hostID).Scan(&target)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if target == hostID {
		return users.ValidationError{Msg: "you already host this room"}
	}
	if action == "remove" {
		if _, err := s.db.Exec(ctx, `DELETE FROM live_cohosts WHERE room_id=$1::uuid AND user_id=$2::uuid`, roomID, target); err != nil {
			return err
		}
		go s.setPublish(roomID, target, false)
		return nil
	}
	var n int
	var already bool
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*)::int, COALESCE(BOOL_OR(user_id=$2::uuid), FALSE) FROM live_cohosts WHERE room_id=$1::uuid`,
		roomID, target).Scan(&n, &already); err != nil {
		return err
	}
	if n >= maxCohosts && !already {
		return users.ValidationError{Msg: "the stage is full"}
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO live_cohosts (room_id, user_id) VALUES ($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, roomID, target); err != nil {
		return err
	}
	go s.setPublish(roomID, target, true)
	return nil
}

// Cohosts lists the display names of a room's co-hosts (Open mode only, by construction).
func (s *Service) Cohosts(ctx context.Context, roomID string) []string {
	rows, err := s.db.Query(ctx, `
		SELECT COALESCE(p.display_name,'') FROM live_cohosts c JOIN profiles p ON p.user_id = c.user_id
		 WHERE c.room_id=$1::uuid ORDER BY c.created_at`, roomID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil && n != "" {
			out = append(out, n)
		}
	}
	return out
}
