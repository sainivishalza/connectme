package games

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"math/big"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/users"
	"connectme/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	maskedMin      = 3
	maskedMax      = 5
	maskedWaitFor  = 20 * time.Second // with three waiting, start after this long
	maskedWaitTTL  = 2 * time.Minute
	maskedLifetime = 25 * time.Minute
	maskedKeep     = time.Hour
)

var (
	aliasAdj  = []string{"Quiet", "Curious", "Golden", "Gentle", "Brave", "Sleepy", "Bright", "Calm", "Clever", "Lucky", "Mellow", "Cosy", "Witty", "Sunny", "Midnight", "Velvet"}
	aliasNoun = []string{"Fox", "Owl", "Otter", "Heron", "Panda", "Falcon", "Lynx", "Robin", "Koala", "Badger", "Swan", "Wren", "Dolphin", "Hare", "Finch", "Moth"}
)

// TextVetter screens what members type into a masked room.
type TextVetter interface {
	VetText(ctx context.Context, userID, kind, text string) error
}

// SetVetter turns on screening of masked answers (nil disables it).
func (s *Service) SetVetter(v TextVetter) { s.vetter = v }

func pickOne(list []string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		return list[0]
	}
	return list[n.Int64()]
}

// MaskedStatus is where the caller stands in the masked-room queue.
type MaskedStatus struct {
	State   string `json:"state"` // idle | waiting | in_room
	RoomID  string `json:"room_id,omitempty"`
	Waiting int    `json:"waiting,omitempty"`
}

// MaskedJoin puts the caller in the queue, or back in the room they are in.
func (s *Service) MaskedJoin(ctx context.Context, userID string) (*MaskedStatus, error) {
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1::uuid`, userID).Scan(&ok); err != nil || !ok {
		return nil, ErrNotFound
	}
	if st := s.maskedRoomOf(ctx, userID); st != "" {
		return &MaskedStatus{State: "in_room", RoomID: st}, nil
	}
	if err := s.maskedCooldown(ctx, userID); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO masked_waiting (user_id) VALUES ($1::uuid) ON CONFLICT (user_id) DO UPDATE SET created_at = NOW()`, userID); err != nil {
		return nil, err
	}
	return s.MaskedStatus(ctx, userID)
}

// ErrCooldown means a member must wait before joining another masked room.
var ErrCooldown = errors.New("please wait a little before joining another masked room")

const (
	maskedLeaveCooldown  = 2 * time.Minute
	maskedReportedLimit  = 2 // different people reporting within a day
	maskedReportedWindow = 24 * time.Hour
)

// maskedCooldown stops two kinds of griefing: leaving a room the moment it
// starts and queueing again at once (which wastes four other people's time),
// and carrying on in new rooms after several people have reported you today.
func (s *Service) maskedCooldown(ctx context.Context, userID string) error {
	var recentLeave, reporters int
	err := s.db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM masked_members m JOIN masked_rooms r ON r.id = m.room_id
		         WHERE m.user_id=$1::uuid AND m.left_at > NOW() - $2::interval AND r.ends_at > m.left_at)::int,
		       (SELECT COUNT(DISTINCT reporter_id) FROM reports
		         WHERE reported_id=$1::uuid AND details='masked room answer' AND created_at > NOW() - $3::interval)::int`,
		userID, maskedLeaveCooldown.String(), maskedReportedWindow.String()).Scan(&recentLeave, &reporters)
	if err != nil {
		return err
	}
	if recentLeave > 0 || reporters >= maskedReportedLimit {
		return ErrCooldown
	}
	return nil
}

func (s *Service) maskedRoomOf(ctx context.Context, userID string) string {
	var id string
	_ = s.db.QueryRow(ctx, `
		SELECT r.id::text FROM masked_members m JOIN masked_rooms r ON r.id=m.room_id
		 WHERE m.user_id=$1::uuid AND m.left_at IS NULL AND r.ends_at > NOW() ORDER BY r.created_at DESC LIMIT 1`, userID).Scan(&id)
	return id
}

// MaskedStatus reports the queue state and forms a room when enough people wait.
func (s *Service) MaskedStatus(ctx context.Context, userID string) (*MaskedStatus, error) {
	if id := s.maskedRoomOf(ctx, userID); id != "" {
		return &MaskedStatus{State: "in_room", RoomID: id}, nil
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM masked_waiting WHERE created_at < NOW() - $1::interval`, maskedWaitTTL.String()); err != nil {
		return nil, err
	}
	if err := s.formMaskedRoom(ctx); err != nil {
		return nil, err
	}
	if id := s.maskedRoomOf(ctx, userID); id != "" {
		return &MaskedStatus{State: "in_room", RoomID: id}, nil
	}
	var waiting int
	var mine bool
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*)::int, COALESCE(BOOL_OR(user_id=$1::uuid), FALSE) FROM masked_waiting`, userID).Scan(&waiting, &mine); err != nil {
		return nil, err
	}
	if !mine {
		return &MaskedStatus{State: "idle"}, nil
	}
	return &MaskedStatus{State: "waiting", Waiting: waiting}, nil
}

// formMaskedRoom seats waiting people together: five as soon as there are
// five; three or four once the oldest has waited a little. No two people who
// have blocked each other are seated together.
func (s *Service) formMaskedRoom(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, `SELECT user_id::text, created_at FROM masked_waiting ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 20`)
	if err != nil {
		return err
	}
	var queue []string
	var oldest time.Time
	for rows.Next() {
		var id string
		var at time.Time
		if rows.Scan(&id, &at) == nil {
			if len(queue) == 0 {
				oldest = at
			}
			queue = append(queue, id)
		}
	}
	rows.Close()
	if len(queue) < maskedMin || (len(queue) < maskedMax && time.Since(oldest) < maskedWaitFor) {
		return nil
	}
	var seat []string
	for _, cand := range queue {
		if len(seat) == maskedMax {
			break
		}
		var clash bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM blocks WHERE (blocker_id=$1::uuid AND blocked_id = ANY($2::uuid[])) OR (blocked_id=$1::uuid AND blocker_id = ANY($2::uuid[])))`,
			cand, append([]string{"00000000-0000-0000-0000-000000000000"}, seat...)).Scan(&clash); err != nil {
			return err
		}
		if !clash {
			seat = append(seat, cand)
		}
	}
	if len(seat) < maskedMin {
		return nil
	}
	var room string
	if err := tx.QueryRow(ctx, `INSERT INTO masked_rooms (ends_at) VALUES ($1) RETURNING id::text`, time.Now().Add(maskedLifetime)).Scan(&room); err != nil {
		return err
	}
	used := map[string]bool{}
	for _, u := range seat {
		alias := ""
		for tries := 0; tries < 20; tries++ {
			alias = pickOne(aliasAdj) + " " + pickOne(aliasNoun)
			if !used[alias] {
				break
			}
		}
		used[alias] = true
		if _, err := tx.Exec(ctx, `INSERT INTO masked_members (room_id, user_id, alias) VALUES ($1::uuid,$2::uuid,$3)`, room, u, alias); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM masked_waiting WHERE user_id = ANY($1::uuid[])`, seat); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MaskedLeave leaves the queue or the room.
func (s *Service) MaskedLeave(ctx context.Context, userID string) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM masked_waiting WHERE user_id=$1::uuid`, userID); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `UPDATE masked_members SET left_at=NOW() WHERE user_id=$1::uuid AND left_at IS NULL`, userID)
	return err
}

// MaskedAnswer is something said in a room, under a made-up name.
type MaskedAnswer struct {
	ID     string    `json:"id"`
	Alias  string    `json:"alias"`
	Kind   string    `json:"kind"`
	Prompt string    `json:"prompt"`
	Body   string    `json:"body"`
	Mine   bool      `json:"mine"`
	At     time.Time `json:"at"`
}

// MaskedRoom is a masked room as one member sees it: the game and what has been said.
type MaskedRoom struct {
	RoomID  string         `json:"room_id"`
	EndsAt  time.Time      `json:"ends_at"`
	Game    *PartyTD       `json:"game"`
	Answers []MaskedAnswer `json:"answers"`
	MyAlias string         `json:"my_alias"`
}

func (s *Service) MaskedRoomState(ctx context.Context, userID, roomID string) (*MaskedRoom, error) {
	room, err := s.partyAccess(ctx, userID, roomID)
	if err != nil || room.kind != "masked" {
		return nil, ErrNotFound
	}
	out := &MaskedRoom{RoomID: roomID, MyAlias: room.labels[userID], Answers: []MaskedAnswer{}}
	if err := s.db.QueryRow(ctx, `SELECT ends_at FROM masked_rooms WHERE id=$1::uuid`, roomID).Scan(&out.EndsAt); err != nil {
		return nil, err
	}
	if out.Game, err = s.partyView(ctx, userID, roomID, room); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT * FROM (
		  SELECT a.id::text, COALESCE(m.alias,'Someone'), a.kind, a.prompt, a.body, a.user_id = $2::uuid, a.created_at
		    FROM masked_answers a LEFT JOIN masked_members m ON m.room_id=a.room_id AND m.user_id=a.user_id
		   WHERE a.room_id=$1::uuid ORDER BY a.created_at DESC LIMIT 30) t ORDER BY created_at`, roomID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a MaskedAnswer
		if err := rows.Scan(&a.ID, &a.Alias, &a.Kind, &a.Prompt, &a.Body, &a.Mine, &a.At); err != nil {
			return nil, err
		}
		out.Answers = append(out.Answers, a)
	}
	return out, rows.Err()
}

// MaskedFinish ends the caller's turn in a masked room. A truth needs a short
// written answer (screened); a dare is just marked done; a skip needs nothing.
func (s *Service) MaskedFinish(ctx context.Context, userID, roomID, text string, skip bool) (*MaskedRoom, error) {
	room, err := s.partyAccess(ctx, userID, roomID)
	if err != nil || room.kind != "masked" {
		return nil, ErrNotFound
	}
	var kind, prompt string
	err = s.db.QueryRow(ctx, `SELECT kind, prompt FROM group_td WHERE room_id=$1::uuid AND turn_user=$2::uuid AND phase='answer'`, roomID, userID).Scan(&kind, &prompt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, users.ValidationError{Msg: "there is nothing to finish right now"}
	}
	if err != nil {
		return nil, err
	}
	if !skip {
		text = utils.StripTags(text)
		n := utf8.RuneCountInString(text)
		if kind == KindTruth && n < 1 {
			return nil, users.ValidationError{Msg: "write a short answer, or skip"}
		}
		if n > 200 {
			return nil, users.ValidationError{Msg: "keep it to 200 characters"}
		}
		for _, c := range text {
			if unicode.IsControl(c) && c != '\n' {
				return nil, users.ValidationError{Msg: "the answer contains invalid characters"}
			}
		}
		if text != "" && s.vetter != nil {
			if err := s.vetter.VetText(ctx, userID, "masked_room", text); err != nil {
				return nil, err
			}
		}
		if _, err := s.db.Exec(ctx, `INSERT INTO masked_answers (room_id, user_id, kind, prompt, body) VALUES ($1::uuid,$2::uuid,$3,$4,$5)`, roomID, userID, kind, prompt, text); err != nil {
			return nil, err
		}
	}
	if _, err := s.PartyFinish(ctx, userID, roomID, skip); err != nil {
		return nil, err
	}
	return s.MaskedRoomState(ctx, userID, roomID)
}

// MaskedReport reports the author of an answer (staff see the text as evidence).
func (s *Service) MaskedReport(ctx context.Context, userID, roomID, answerID, reason string) error {
	if _, err := s.partyAccess(ctx, userID, roomID); err != nil {
		return err
	}
	if _, err := uuid.Parse(answerID); err != nil {
		return ErrNotFound
	}
	switch reason {
	case "spam", "harassment", "inappropriate_content", "scam", "underage", "other":
	default:
		return users.ValidationError{Msg: "unknown reason"}
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason, details, ref_kind, ref_id, evidence)
		SELECT $1::uuid, a.user_id, $4, 'masked room answer', 'masked_room', a.room_id,
		       -- the reported line first, then the room's recent lines for context:
		       -- the room is deleted an hour after it ends, the report is not
		       jsonb_build_array('REPORTED: ' || a.prompt || ' / ' || a.body) ||
		       COALESCE((SELECT jsonb_agg(x.line ORDER BY x.at) FROM (
		           SELECT m.alias || ': ' || b.prompt || ' / ' || b.body AS line, b.created_at AS at
		             FROM masked_answers b JOIN masked_members m ON m.room_id=b.room_id AND m.user_id=b.user_id
		            WHERE b.room_id=a.room_id ORDER BY b.created_at DESC LIMIT 30) x), '[]'::jsonb)
		  FROM masked_answers a WHERE a.id=$3::uuid AND a.room_id=$2::uuid AND a.user_id <> $1::uuid`, userID, roomID, answerID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ExpireMasked removes rooms an hour after they ended.
func (s *Service) ExpireMasked(ctx context.Context) (int, error) {
	if _, err := s.db.Exec(ctx, `DELETE FROM group_td WHERE room_id IN (SELECT id FROM masked_rooms WHERE ends_at < NOW() - $1::interval)`, maskedKeep.String()); err != nil {
		return 0, err
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM masked_rooms WHERE ends_at < NOW() - $1::interval`, maskedKeep.String())
	return int(tag.RowsAffected()), err
}

// StartMaskedJob clears old masked rooms every interval until ctx ends.
func (s *Service) StartMaskedJob(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			if _, err := s.ExpireMasked(ctx); err != nil && ctx.Err() == nil {
				slog.Error("games: masked room cleanup", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
