package games

import (
	"context"
	"errors"
	"time"

	"connectme/internal/users"

	"github.com/jackc/pgx/v5"
)

const (
	skipsPerPlayer = 2
	// staleAfter is how long the person whose turn it is can leave the game
	// idle before the other player may take over.
	staleAfter = 5 * time.Minute
)

// TDState is the game as one of the two players sees it.
type TDState struct {
	Active    bool   `json:"active"`
	Round     int    `json:"round,omitempty"`
	YourTurn  bool   `json:"your_turn,omitempty"`
	Phase     string `json:"phase,omitempty"` // choose | answer
	Kind      string `json:"kind,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	SkipsLeft int    `json:"skips_left"`
	// CanTakeOver is true for the waiting player once the other has been idle
	// for too long.
	CanTakeOver bool `json:"can_take_over,omitempty"`
}

func (s *Service) tdView(ctx context.Context, userID, matchID string) (*TDState, error) {
	var turn, phase string
	var kind, prompt *string
	var round int
	var skipsLeft int
	var idle time.Duration
	err := s.db.QueryRow(ctx, `
		SELECT turn_user::text, phase, kind, prompt, round,
		       GREATEST($3 - COALESCE((skips ->> $2)::int, 0), 0),
		       NOW() - updated_at
		  FROM td_games WHERE match_id = $1::uuid`, matchID, userID, skipsPerPlayer).
		Scan(&turn, &phase, &kind, &prompt, &round, &skipsLeft, &idle)
	if errors.Is(err, pgx.ErrNoRows) {
		return &TDState{Active: false, SkipsLeft: skipsPerPlayer}, nil
	}
	if err != nil {
		return nil, err
	}
	st := &TDState{Active: true, Round: round, YourTurn: turn == userID, Phase: phase, SkipsLeft: skipsLeft}
	if kind != nil {
		st.Kind = *kind
	}
	if prompt != nil {
		st.Prompt = *prompt
	}
	st.CanTakeOver = !st.YourTurn && idle > staleAfter
	return st, nil
}

// TDStart begins a game in a match (the starter goes first), or returns the
// one in progress.
func (s *Service) TDStart(ctx context.Context, userID, matchID string) (*TDState, error) {
	if _, err := s.member(ctx, userID, matchID); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO td_games (match_id, turn_user) VALUES ($1::uuid, $2::uuid) ON CONFLICT DO NOTHING`, matchID, userID); err != nil {
		return nil, err
	}
	return s.tdView(ctx, userID, matchID)
}

// TDState returns the current game, or an inactive state.
func (s *Service) TDState(ctx context.Context, userID, matchID string) (*TDState, error) {
	if _, err := s.member(ctx, userID, matchID); err != nil {
		return nil, err
	}
	return s.tdView(ctx, userID, matchID)
}

// TDPick draws a truth or a dare for the player whose turn it is.
func (s *Service) TDPick(ctx context.Context, userID, matchID, kind string) (*TDState, error) {
	if kind != KindTruth && kind != KindDare {
		return nil, users.ValidationError{Msg: "choose truth or dare"}
	}
	if _, err := s.member(ctx, userID, matchID); err != nil {
		return nil, err
	}
	p, err := s.Prompt(kind)
	if err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE td_games SET phase='answer', kind=$3, prompt=$4, updated_at=NOW()
		 WHERE match_id=$1::uuid AND turn_user=$2::uuid AND phase='choose'`, matchID, userID, kind, p.Text)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, users.ValidationError{Msg: "it is not your turn to choose"}
	}
	return s.tdView(ctx, userID, matchID)
}

// TDFinish ends the current turn: "done" after answering or doing the dare,
// "skip" to pass (limited), after which the other person is up.
func (s *Service) TDFinish(ctx context.Context, userID, matchID string, skip bool) (*TDState, error) {
	other, err := s.member(ctx, userID, matchID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var skips int
	err = tx.QueryRow(ctx, `
		SELECT COALESCE((skips ->> $2)::int, 0) FROM td_games
		 WHERE match_id=$1::uuid AND turn_user=$2::uuid AND phase='answer' FOR UPDATE`, matchID, userID).Scan(&skips)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, users.ValidationError{Msg: "there is nothing to finish right now"}
	}
	if err != nil {
		return nil, err
	}
	if skip && skips >= skipsPerPlayer {
		return nil, users.ValidationError{Msg: "you have used your skips"}
	}
	inc := 0
	if skip {
		inc = 1
	}
	if _, err := tx.Exec(ctx, `
		UPDATE td_games SET turn_user=$3::uuid, phase='choose', kind=NULL, prompt=NULL, round=round+1, updated_at=NOW(),
		       skips = jsonb_set(skips, ARRAY[$2::text], to_jsonb(COALESCE((skips ->> $2)::int, 0) + $4::int))
		 WHERE match_id=$1::uuid`, matchID, userID, other, inc); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.tdView(ctx, userID, matchID)
}

// TDTakeOver lets the waiting player move on when the other has been idle too
// long: it becomes their turn to choose, at no cost to either.
func (s *Service) TDTakeOver(ctx context.Context, userID, matchID string) (*TDState, error) {
	if _, err := s.member(ctx, userID, matchID); err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE td_games SET turn_user=$2::uuid, phase='choose', kind=NULL, prompt=NULL, updated_at=NOW()
		 WHERE match_id=$1::uuid AND turn_user<>$2::uuid AND updated_at < NOW() - $3::interval`, matchID, userID, staleAfter.String())
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, users.ValidationError{Msg: "the other player is still taking their turn"}
	}
	return s.tdView(ctx, userID, matchID)
}

// TDEnd stops the game (either player).
func (s *Service) TDEnd(ctx context.Context, userID, matchID string) error {
	if _, err := s.member(ctx, userID, matchID); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `DELETE FROM td_games WHERE match_id=$1::uuid`, matchID)
	return err
}
