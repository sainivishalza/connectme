package live

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/users"

	"github.com/jackc/pgx/v5"
)

// Event is one public line in a stream: a chat message or a gift
// announcement. Senders who are not in Open mode appear as "Someone" so
// chatting never reveals a hidden identity.
type Event struct {
	ID   int64     `json:"id"`
	Ref  string    `json:"ref"`  // sender's public ref (lets the host mute or kick)
	Kind string    `json:"kind"` // chat | gift
	Name string    `json:"name"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

const maxChatRunes = 200

// Chat posts a message to a room the caller may watch.
func (s *Service) Chat(ctx context.Context, userID, roomID, text string) error {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n < 1 || n > maxChatRunes {
		return users.ValidationError{Msg: "message must be 1-200 characters"}
	}
	for _, c := range text {
		if unicode.IsControl(c) && c != '\n' {
			return users.ValidationError{Msg: "message contains invalid characters"}
		}
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, userID).Scan(&ok); err != nil || !ok {
		return ErrForbidden
	}
	if _, err := s.visible(ctx, userID, roomID); err != nil {
		return err
	}
	var muted bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM live_room_bans WHERE room_id=$1::uuid AND user_id=$2)`,
		roomID, userID).Scan(&muted); err != nil {
		return err
	}
	if muted {
		return ErrForbidden
	}
	if s.vetter != nil {
		if err := s.vetter.VetText(ctx, userID, "live_chat", text); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(ctx,
		`INSERT INTO live_events (room_id, user_id, kind, body) VALUES ($1::uuid,$2,'chat',$3)`, roomID, userID, text)
	return err
}

// Events returns events newer than `after` (oldest first, at most 100). With
// after == 0 it returns the most recent 30 so a late joiner sees context.
// Events from people the viewer has blocked, or who blocked them, are hidden.
func (s *Service) Events(ctx context.Context, userID, roomID string, after int64) ([]Event, error) {
	if _, err := s.visible(ctx, userID, roomID); err != nil {
		return nil, err
	}
	var rows pgx.Rows
	var err error
	if after == 0 {
		rows, err = s.db.Query(ctx, `
			SELECT * FROM (
			  SELECT e.id, p.public_ref::text, e.kind, CASE WHEN p.mode='open' THEN COALESCE(NULLIF(p.display_name,''),'Someone') ELSE 'Someone' END, COALESCE(e.body,''), e.created_at
			    FROM live_events e JOIN profiles p ON p.user_id=e.user_id
			   WHERE e.room_id=$1::uuid AND `+users.NotBlockedSQL("e.user_id", "$2::uuid")+`
			   ORDER BY e.id DESC LIMIT 30) t ORDER BY id ASC`, roomID, userID)
	} else {
		rows, err = s.db.Query(ctx, `
			SELECT e.id, p.public_ref::text, e.kind, CASE WHEN p.mode='open' THEN COALESCE(NULLIF(p.display_name,''),'Someone') ELSE 'Someone' END, COALESCE(e.body,''), e.created_at
			  FROM live_events e JOIN profiles p ON p.user_id=e.user_id
			 WHERE e.room_id=$1::uuid AND e.id > $3 AND `+users.NotBlockedSQL("e.user_id", "$2::uuid")+`
			 ORDER BY e.id ASC LIMIT 100`, roomID, userID, after)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Ref, &e.Kind, &e.Name, &e.Text, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EndRoom force-ends a room (used by staff) and returns its host.
func (s *Service) EndRoom(ctx context.Context, roomID string) (string, error) {
	var host string
	err := s.db.QueryRow(ctx, `
		UPDATE live_rooms SET status='ended', ended_at=NOW()
		 WHERE id=$1::uuid AND status='live' RETURNING host_id::text`, roomID).Scan(&host)
	if err != nil {
		return "", ErrNotFound
	}
	go s.close(roomID)
	return host, nil
}

// EndByHost ends every live room a user is hosting (used when they are banned).
func (s *Service) EndByHost(ctx context.Context, hostID string) {
	rows, err := s.db.Query(ctx, `
		UPDATE live_rooms SET status='ended', ended_at=NOW()
		 WHERE host_id=$1::uuid AND status='live' RETURNING id::text`, hostID)
	if err != nil {
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
