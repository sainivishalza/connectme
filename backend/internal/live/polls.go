package live

import (
	"context"
	"errors"
	"unicode"
	"unicode/utf8"

	"connectme/internal/users"
	"connectme/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Poll is the room's current question as one viewer sees it. Totals are shown
// live to everyone (it is a game for the audience); who voted for what is never
// stored in a way any response can show.
type Poll struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Counts   []int    `json:"counts"`
	MyChoice *int     `json:"my_choice,omitempty"`
	Closed   bool     `json:"closed"`
}

func cleanPollText(s string, max int, what string) (string, error) {
	s = utils.StripTags(s)
	n := utf8.RuneCountInString(s)
	if n < 1 || n > max {
		return "", users.ValidationError{Msg: what + " must be 1 to " + itoa(max) + " characters"}
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return "", users.ValidationError{Msg: what + " contains invalid characters"}
		}
	}
	return s, nil
}

func itoa(n int) string {
	if n == 80 {
		return "80"
	}
	return "30"
}

// StartPoll opens a poll in the caller's room (host or co-host). A poll that is
// still open must be closed first.
func (s *Service) StartPoll(ctx context.Context, userID, roomID, question string, options []string) (*Poll, error) {
	if _, err := uuid.Parse(roomID); err != nil {
		return nil, ErrNotFound
	}
	ok, err := s.canRun(ctx, roomID, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	q, err := cleanPollText(question, 80, "the question")
	if err != nil {
		return nil, err
	}
	if len(options) < 2 || len(options) > 4 {
		return nil, users.ValidationError{Msg: "give 2 to 4 answers"}
	}
	clean := make([]string, 0, len(options))
	for _, o := range options {
		c, err := cleanPollText(o, 30, "an answer")
		if err != nil {
			return nil, err
		}
		clean = append(clean, c)
	}
	if s.vetter != nil {
		all := q
		for _, c := range clean {
			all += " " + c
		}
		if err := s.vetter.VetText(ctx, userID, "live_chat", all); err != nil {
			return nil, err
		}
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO live_polls (room_id, question, options) VALUES ($1::uuid,$2,$3)`, roomID, q, clean); err != nil {
		return nil, users.ValidationError{Msg: "close the current poll first"}
	}
	return s.PollFor(ctx, userID, roomID)
}

// Vote records a viewer's choice; the first vote stands.
func (s *Service) Vote(ctx context.Context, userID, roomID, pollID string, choice int) (*Poll, error) {
	if _, err := s.visible(ctx, userID, roomID); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(pollID); err != nil {
		return nil, ErrNotFound
	}
	cur, err := s.PollFor(ctx, userID, roomID)
	if err != nil {
		return nil, err
	}
	if cur == nil || cur.ID != pollID || cur.Closed {
		return nil, users.ValidationError{Msg: "this poll has ended"}
	}
	if choice < 0 || choice >= len(cur.Options) {
		return nil, users.ValidationError{Msg: "choose one of the answers"}
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO live_poll_votes (poll_id, user_id, choice)
		SELECT p.id, $3::uuid, $4::smallint FROM live_polls p
		 WHERE p.id=$1::uuid AND p.room_id=$2::uuid AND p.closed_at IS NULL
		ON CONFLICT DO NOTHING`, pollID, roomID, userID, choice)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		p, err := s.PollFor(ctx, userID, roomID)
		if err != nil {
			return nil, err
		}
		if p == nil || p.ID != pollID || p.Closed {
			return nil, users.ValidationError{Msg: "this poll has ended"}
		}
		return p, nil
	}
	return s.PollFor(ctx, userID, roomID)
}

// ClosePoll ends the open poll (host or co-host); its result stays visible.
func (s *Service) ClosePoll(ctx context.Context, userID, roomID string) error {
	if _, err := uuid.Parse(roomID); err != nil {
		return ErrNotFound
	}
	ok, err := s.canRun(ctx, roomID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	_, err = s.db.Exec(ctx, `UPDATE live_polls SET closed_at=NOW() WHERE room_id=$1::uuid AND closed_at IS NULL`, roomID)
	return err
}

// PollFor returns the room's latest poll (open, or closed in the last 2 minutes), or nil.
func (s *Service) PollFor(ctx context.Context, userID, roomID string) (*Poll, error) {
	var p Poll
	var closedNull bool
	err := s.db.QueryRow(ctx, `
		SELECT id::text, question, options, closed_at IS NULL FROM live_polls
		 WHERE room_id=$1::uuid AND (closed_at IS NULL OR closed_at > NOW() - INTERVAL '2 minutes')
		 ORDER BY created_at DESC LIMIT 1`, roomID).Scan(&p.ID, &p.Question, &p.Options, &closedNull)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Closed = !closedNull
	p.Counts = make([]int, len(p.Options))
	rows, err := s.db.Query(ctx, `SELECT choice, COUNT(*)::int FROM live_poll_votes WHERE poll_id=$1::uuid GROUP BY choice`, p.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c, n int
		if rows.Scan(&c, &n) == nil && c < len(p.Counts) {
			p.Counts[c] = n
		}
	}
	rows.Close()
	var mine int
	if err := s.db.QueryRow(ctx, `SELECT choice FROM live_poll_votes WHERE poll_id=$1::uuid AND user_id=$2::uuid`, p.ID, userID).Scan(&mine); err == nil {
		p.MyChoice = &mine
	}
	return &p, nil
}
