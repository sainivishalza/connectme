package games

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"time"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Light, kind and about nobody's body, money or private life.
var mostLikelyTo = []string{
	"Most likely to cook a feast for everyone",
	"Most likely to arrive early",
	"Most likely to start a dance floor",
	"Most likely to plan the whole trip",
	"Most likely to make friends with a stranger",
	"Most likely to still be talking at midnight",
	"Most likely to find the best café in any city",
	"Most likely to win the pub quiz",
	"Most likely to adopt a rescue dog",
	"Most likely to sing in the shower",
	"Most likely to organise the group photo",
	"Most likely to try the strangest thing on the menu",
	"Most likely to be calm when plans change",
	"Most likely to know a song for every moment",
	"Most likely to learn a new language for fun",
	"Most likely to be the best gift-giver",
	"Most likely to stay for the very last dance",
	"Most likely to make everyone laugh",
	"Most likely to take a spontaneous road trip",
	"Most likely to remember everyone's birthday",
	"Most likely to read a book in one sitting",
	"Most likely to turn a rainy day into an adventure",
	"Most likely to bake something for the group",
	"Most likely to give the best advice",
}

// MLTCandidate is one member who can be voted for. Names appear only for
// people shown in full; the rest are "Member".
type MLTCandidate struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Count *int   `json:"count,omitempty"`
}

// MLTState is a round as one member sees it.
type MLTState struct {
	Active     bool           `json:"active"`
	PollID     string         `json:"poll_id,omitempty"`
	Prompt     string         `json:"prompt,omitempty"`
	Closed     bool           `json:"closed"`
	Members    int            `json:"members"`
	Voted      int            `json:"voted"`
	MyVote     string         `json:"my_vote,omitempty"`
	Candidates []MLTCandidate `json:"candidates,omitempty"`
	EndsAt     *time.Time     `json:"ends_at,omitempty"`
}

// otherRoundRunning reports whether a round of a different game is open.
func (s *Service) otherRoundRunning(ctx context.Context, groupMatchID, mine string) bool {
	var n int
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM group_polls WHERE group_match_id=$1::uuid AND kind <> $2 AND closed_at IS NULL AND created_at > NOW() - make_interval(secs => $3)`,
		groupMatchID, mine, wyrOpenFor.Seconds()).Scan(&n)
	return n > 0
}

func (s *Service) mltCandidates(ctx context.Context, groupMatchID string) ([]MLTCandidate, error) {
	rows, err := s.db.Query(ctx, `
		SELECT p.public_ref::text, CASE WHEN p.mode='open' AND NOT p.invisible AND COALESCE(p.display_name,'') <> '' THEN p.display_name ELSE 'Member' END
		  FROM group_matches gm
		  JOIN group_members m ON m.group_id IN (gm.group_a, gm.group_b)
		  JOIN profiles p ON p.user_id = m.user_id
		 WHERE gm.id=$1::uuid ORDER BY p.public_ref`, groupMatchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MLTCandidate
	n := 0
	for rows.Next() {
		var c MLTCandidate
		if err := rows.Scan(&c.ID, &c.Label); err != nil {
			return nil, err
		}
		if c.Label == "Member" {
			n++
			c.Label = "Member " + string(rune('A'+(n-1)%26))
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MLTState returns the running round, or the one that just finished.
func (s *Service) MLTState(ctx context.Context, userID, groupMatchID string) (*MLTState, error) {
	members, err := s.wyrAccess(ctx, userID, groupMatchID)
	if err != nil {
		return nil, err
	}
	var id string
	var q int
	var created time.Time
	var closed *time.Time
	err = s.db.QueryRow(ctx, `
		SELECT id::text, question, created_at, closed_at FROM group_polls
		 WHERE group_match_id=$1::uuid AND kind='mlt' ORDER BY created_at DESC LIMIT 1`, groupMatchID).Scan(&id, &q, &created, &closed)
	if errors.Is(err, pgx.ErrNoRows) {
		return &MLTState{Members: members}, nil
	}
	if err != nil {
		return nil, err
	}
	var voted int
	var mine *string
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*)::int, (SELECT p.public_ref::text FROM group_poll_votes mv JOIN profiles p ON p.user_id = mv.voted_for WHERE mv.poll_id=$1::uuid AND mv.user_id=$2::uuid) FROM group_poll_votes WHERE poll_id=$1::uuid`, id, userID).Scan(&voted, &mine); err != nil {
		return nil, err
	}
	if closed == nil && (voted >= members || time.Since(created) > wyrOpenFor) {
		if _, err := s.db.Exec(ctx, `UPDATE group_polls SET closed_at=NOW() WHERE id=$1::uuid AND closed_at IS NULL`, id); err != nil {
			return nil, err
		}
		now := time.Now()
		closed = &now
	}
	if time.Since(created) > 6*time.Hour && closed != nil {
		return &MLTState{Members: members}, nil
	}
	st := &MLTState{Active: true, PollID: id, Prompt: mostLikelyTo[q%len(mostLikelyTo)], Closed: closed != nil, Members: members, Voted: voted}
	if mine != nil {
		st.MyVote = *mine
	}
	cands, err := s.mltCandidates(ctx, groupMatchID)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	if mine != nil || closed != nil {
		rows, err := s.db.Query(ctx, `SELECT p.public_ref::text, COUNT(*)::int FROM group_poll_votes v JOIN profiles p ON p.user_id = v.voted_for WHERE v.poll_id=$1::uuid GROUP BY p.public_ref`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var who string
			var n int
			if rows.Scan(&who, &n) == nil {
				counts[who] = n
			}
		}
		rows.Close()
		for i := range cands {
			n := counts[cands[i].ID]
			cands[i].Count = &n
		}
	}
	st.Candidates = cands
	if closed == nil {
		end := created.Add(wyrOpenFor)
		st.EndsAt = &end
	}
	return st, nil
}

// StartMLT begins a round if nothing else is running in the chat.
func (s *Service) StartMLT(ctx context.Context, userID, groupMatchID string) (*MLTState, error) {
	if _, err := s.wyrAccess(ctx, userID, groupMatchID); err != nil {
		return nil, err
	}
	cur, err := s.MLTState(ctx, userID, groupMatchID)
	if err != nil {
		return nil, err
	}
	if cur.Active && !cur.Closed {
		return nil, users.ValidationError{Msg: "a round is already running; vote on it or wait for it to finish"}
	}
	if s.otherRoundRunning(ctx, groupMatchID, "mlt") {
		return nil, users.ValidationError{Msg: "another game is running in this chat; let it finish first"}
	}
	rows, err := s.db.Query(ctx, `SELECT question FROM group_polls WHERE group_match_id=$1::uuid AND kind='mlt' ORDER BY created_at DESC LIMIT $2`, groupMatchID, wyrNoRepeatOf)
	if err != nil {
		return nil, err
	}
	recent := map[int]bool{}
	for rows.Next() {
		var q int
		if rows.Scan(&q) == nil {
			recent[q] = true
		}
	}
	rows.Close()
	var choices []int
	for i := range mostLikelyTo {
		if !recent[i] {
			choices = append(choices, i)
		}
	}
	if len(choices) == 0 {
		for i := range mostLikelyTo {
			choices = append(choices, i)
		}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(choices))))
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO group_polls (group_match_id, question, started_by, kind) VALUES ($1::uuid,$2,$3::uuid,'mlt')`,
		groupMatchID, choices[n.Int64()], userID); err != nil {
		return s.MLTState(ctx, userID, groupMatchID)
	}
	return s.MLTState(ctx, userID, groupMatchID)
}

// VoteMLT records who the caller picked. The first vote stands.
func (s *Service) VoteMLT(ctx context.Context, userID, groupMatchID, pollID, candidate string) (*MLTState, error) {
	if _, err := s.wyrAccess(ctx, userID, groupMatchID); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(pollID); err != nil {
		return nil, ErrNotFound
	}
	if _, err := uuid.Parse(candidate); err != nil {
		return nil, users.ValidationError{Msg: "choose someone in this group"}
	}
	cands, err := s.mltCandidates(ctx, groupMatchID)
	if err != nil {
		return nil, err
	}
	known := false
	for _, c := range cands {
		known = known || c.ID == candidate
	}
	if !known {
		return nil, users.ValidationError{Msg: "choose someone in this group"}
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO group_poll_votes (poll_id, user_id, choice, voted_for)
		SELECT p.id, $3::uuid, 'a', (SELECT user_id FROM profiles WHERE public_ref=$4::uuid) FROM group_polls p
		 WHERE p.id=$1::uuid AND p.group_match_id=$2::uuid AND p.kind='mlt' AND p.closed_at IS NULL AND p.created_at > NOW() - make_interval(secs => $5)
		ON CONFLICT DO NOTHING`, pollID, groupMatchID, userID, candidate, wyrOpenFor.Seconds())
	if err != nil {
		return nil, err
	}
	st, err := s.MLTState(ctx, userID, groupMatchID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 && (st.PollID != pollID || st.Closed) {
		return nil, users.ValidationError{Msg: "this round has finished"}
	}
	return st, nil
}
