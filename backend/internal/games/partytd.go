package games

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Player is one person in a party game as the viewer may know them.
type Player struct {
	Label string `json:"label"`
	You   bool   `json:"you,omitempty"`
	Turn  bool   `json:"turn,omitempty"`
}

// PartyTD is a group Truth or Dare game as one player sees it.
type PartyTD struct {
	Active      bool     `json:"active"`
	Round       int      `json:"round,omitempty"`
	YourTurn    bool     `json:"your_turn,omitempty"`
	Phase       string   `json:"phase,omitempty"` // choose | answer
	Kind        string   `json:"kind,omitempty"`
	Prompt      string   `json:"prompt,omitempty"`
	SkipsLeft   int      `json:"skips_left"`
	CanTakeOver bool     `json:"can_take_over,omitempty"`
	Players     []Player `json:"players,omitempty"`
}

// partyRoom is where a game runs and who is in it.
type partyRoom struct {
	kind    string // group | masked
	members []string
	labels  map[string]string
}

// partyAccess proves the caller is in the room (a matched group chat or a live
// masked room) and returns its members with the labels the caller may see.
func (s *Service) partyAccess(ctx context.Context, userID, roomID string) (*partyRoom, error) {
	if _, err := uuid.Parse(roomID); err != nil {
		return nil, ErrNotFound
	}
	// A matched group chat: members of either group.
	rows, err := s.db.Query(ctx, `
		SELECT m.user_id::text, CASE WHEN p.mode='open' AND NOT p.invisible AND COALESCE(p.display_name,'') <> '' THEN p.display_name ELSE '' END
		  FROM group_matches gm JOIN group_members m ON m.group_id IN (gm.group_a, gm.group_b) JOIN profiles p ON p.user_id = m.user_id
		 WHERE gm.id=$1::uuid AND gm.status='matched' ORDER BY m.user_id`, roomID)
	if err != nil {
		return nil, err
	}
	room := &partyRoom{kind: "group", labels: map[string]string{}}
	anon := 0
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) != nil {
			continue
		}
		if name == "" {
			anon++
			name = "Member " + string(rune('A'+(anon-1)%26))
		}
		room.members = append(room.members, id)
		room.labels[id] = name
	}
	rows.Close()
	if len(room.members) > 0 {
		for _, m := range room.members {
			if m == userID {
				return room, nil
			}
		}
		return nil, ErrNotFound
	}
	// A masked room that is still running; labels are the made-up names.
	mrows, err := s.db.Query(ctx, `
		SELECT mm.user_id::text, mm.alias FROM masked_members mm JOIN masked_rooms r ON r.id = mm.room_id
		 WHERE mm.room_id=$1::uuid AND mm.left_at IS NULL AND r.ends_at > NOW() ORDER BY mm.user_id`, roomID)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	room = &partyRoom{kind: "masked", labels: map[string]string{}}
	in := false
	for mrows.Next() {
		var id, alias string
		if mrows.Scan(&id, &alias) == nil {
			room.members = append(room.members, id)
			room.labels[id] = alias
			in = in || id == userID
		}
	}
	if !in {
		return nil, ErrNotFound
	}
	return room, nil
}

func nextAfter(order []string, cur string) string {
	for i, id := range order {
		if id == cur {
			return order[(i+1)%len(order)]
		}
	}
	return order[0]
}

// partyView builds the state, repairing the order if people joined or left.
func (s *Service) partyView(ctx context.Context, userID, roomID string, room *partyRoom) (*PartyTD, error) {
	var turn, phase string
	var kind, prompt *string
	var round, skipsLeft int
	var idle time.Duration
	var order []string
	err := s.db.QueryRow(ctx, `
		SELECT turn_user::text, phase, kind, prompt, round, play_order::text[], GREATEST($3 - COALESCE((skips ->> $2)::int, 0), 0), NOW() - updated_at
		  FROM group_td WHERE room_id=$1::uuid`, roomID, userID, skipsPerPlayer).Scan(&turn, &phase, &kind, &prompt, &round, &order, &skipsLeft, &idle)
	if errors.Is(err, pgx.ErrNoRows) {
		return &PartyTD{Active: false, SkipsLeft: skipsPerPlayer}, nil
	}
	if err != nil {
		return nil, err
	}
	// If the player whose turn it is has left, the next member goes.
	present := map[string]bool{}
	for _, m := range room.members {
		present[m] = true
	}
	if !present[turn] {
		turn = nextAfter(room.members, turn)
		if _, err := s.db.Exec(ctx, `UPDATE group_td SET turn_user=$2::uuid, phase='choose', kind=NULL, prompt=NULL, updated_at=NOW() WHERE room_id=$1::uuid`, roomID, turn); err != nil {
			return nil, err
		}
		phase, kind, prompt, idle = "choose", nil, nil, 0
	}
	st := &PartyTD{Active: true, Round: round, YourTurn: turn == userID, Phase: phase, SkipsLeft: skipsLeft}
	if kind != nil {
		st.Kind = *kind
	}
	if prompt != nil {
		st.Prompt = *prompt
	}
	st.CanTakeOver = !st.YourTurn && idle > staleAfter
	for _, m := range room.members {
		st.Players = append(st.Players, Player{Label: room.labels[m], You: m == userID, Turn: m == turn})
	}
	return st, nil
}

func (s *Service) PartyState(ctx context.Context, userID, roomID string) (*PartyTD, error) {
	room, err := s.partyAccess(ctx, userID, roomID)
	if err != nil {
		return nil, err
	}
	return s.partyView(ctx, userID, roomID, room)
}

// PartyStart begins a game in the room (the starter goes first).
func (s *Service) PartyStart(ctx context.Context, userID, roomID string) (*PartyTD, error) {
	room, err := s.partyAccess(ctx, userID, roomID)
	if err != nil {
		return nil, err
	}
	if len(room.members) < 3 {
		return nil, users.ValidationError{Msg: "a group game needs at least three people"}
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO group_td (room_id, play_order, turn_user) VALUES ($1::uuid, $2::uuid[], $3::uuid) ON CONFLICT DO NOTHING`,
		roomID, room.members, userID); err != nil {
		return nil, err
	}
	return s.partyView(ctx, userID, roomID, room)
}

// PartyPick draws a truth or a dare for the player whose turn it is.
func (s *Service) PartyPick(ctx context.Context, userID, roomID, kind string) (*PartyTD, error) {
	if kind != KindTruth && kind != KindDare {
		return nil, users.ValidationError{Msg: "choose truth or dare"}
	}
	room, err := s.partyAccess(ctx, userID, roomID)
	if err != nil {
		return nil, err
	}
	p, err := s.Prompt(kind)
	if err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `UPDATE group_td SET phase='answer', kind=$3, prompt=$4, updated_at=NOW() WHERE room_id=$1::uuid AND turn_user=$2::uuid AND phase='choose'`,
		roomID, userID, kind, p.Text)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, users.ValidationError{Msg: "it is not your turn to choose"}
	}
	return s.partyView(ctx, userID, roomID, room)
}

// advance moves the game to the next player. skip counts against the skipper.
func (s *Service) advance(ctx context.Context, roomID, from, next string, skipped bool) error {
	inc := 0
	if skipped {
		inc = 1
	}
	_, err := s.db.Exec(ctx, `
		UPDATE group_td SET turn_user=$3::uuid, phase='choose', kind=NULL, prompt=NULL, round=round+1, updated_at=NOW(),
		       skips = jsonb_set(skips, ARRAY[$2::text], to_jsonb(COALESCE((skips ->> $2)::int, 0) + $4::int))
		 WHERE room_id=$1::uuid`, roomID, from, next, inc)
	return err
}

// PartyFinish ends the player's turn: done, or skip (limited).
func (s *Service) PartyFinish(ctx context.Context, userID, roomID string, skip bool) (*PartyTD, error) {
	room, err := s.partyAccess(ctx, userID, roomID)
	if err != nil {
		return nil, err
	}
	var skips int
	err = s.db.QueryRow(ctx, `SELECT COALESCE((skips ->> $2)::int, 0) FROM group_td WHERE room_id=$1::uuid AND turn_user=$2::uuid AND phase='answer'`, roomID, userID).Scan(&skips)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, users.ValidationError{Msg: "there is nothing to finish right now"}
	}
	if err != nil {
		return nil, err
	}
	if skip && skips >= skipsPerPlayer {
		return nil, users.ValidationError{Msg: "you have used your skips"}
	}
	if err := s.advance(ctx, roomID, userID, nextAfter(room.members, userID), skip); err != nil {
		return nil, err
	}
	return s.partyView(ctx, userID, roomID, room)
}

// PartyTakeOver lets anyone else move the game on when the player has been
// idle too long; it costs the idle player nothing.
func (s *Service) PartyTakeOver(ctx context.Context, userID, roomID string) (*PartyTD, error) {
	room, err := s.partyAccess(ctx, userID, roomID)
	if err != nil {
		return nil, err
	}
	var turn string
	err = s.db.QueryRow(ctx, `SELECT turn_user::text FROM group_td WHERE room_id=$1::uuid AND turn_user<>$2::uuid AND updated_at < NOW() - $3::interval`,
		roomID, userID, staleAfter.String()).Scan(&turn)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, users.ValidationError{Msg: "the other player is still taking their turn"}
	}
	if err != nil {
		return nil, err
	}
	if err := s.advance(ctx, roomID, turn, nextAfter(room.members, turn), false); err != nil {
		return nil, err
	}
	return s.partyView(ctx, userID, roomID, room)
}

// PartyEnd stops the game for everyone.
func (s *Service) PartyEnd(ctx context.Context, userID, roomID string) error {
	if _, err := s.partyAccess(ctx, userID, roomID); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `DELETE FROM group_td WHERE room_id=$1::uuid`, roomID)
	return err
}

var _ = fmt.Sprintf
