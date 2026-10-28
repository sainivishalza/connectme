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

// Same content rules as the other prompts: adults who have just met, nothing
// sexual, no money, no personal details, no risk, no alcohol.
type wyrPair struct{ A, B string }

var wouldYouRather = []wyrPair{
	{"Travel anywhere for a week with no plan", "Travel somewhere close with a perfect plan"},
	{"Always arrive 15 minutes early", "Always arrive 15 minutes late"},
	{"Cook dinner for everyone", "Wash up for everyone"},
	{"Live by the sea", "Live in the mountains"},
	{"Have a rooftop with a view", "Have a garden with a hammock"},
	{"Only ever eat breakfast food", "Only ever eat dessert"},
	{"Spend a weekend at a music festival", "Spend a weekend in a quiet cabin"},
	{"Be able to speak every language", "Be able to play every instrument"},
	{"Wake up early and have a long morning", "Stay up late and have a long night"},
	{"Always have the perfect playlist", "Always have the perfect snack"},
	{"Go on a road trip with no map", "Go on a train trip with a great book"},
	{"Have a dog that understands you", "Have a cat that obeys you"},
	{"Win a pub quiz by yourself", "Win a board game as a team"},
	{"Visit the future for a day", "Visit the past for a day"},
	{"Have summer all year", "Have autumn all year"},
	{"Learn to surf", "Learn to ski"},
	{"Watch the film instead of reading the book", "Read the book instead of watching the film"},
	{"Have a picnic in the park", "Have a feast at a restaurant"},
	{"Be great at karaoke", "Be great at dancing"},
	{"Always be warm", "Always be cool"},
	{"Spend a day at a theme park", "Spend a day at a museum"},
	{"Explore a new city on foot", "Explore a new city by bike"},
	{"Have a personal chef", "Have a personal driver"},
	{"Hear the funniest joke ever once", "Hear a pretty good joke every day"},
	{"Take a cooking class together", "Take a dance class together"},
	{"Stay in and watch a series", "Go out and find an adventure"},
	{"Have pancakes every Sunday", "Have pizza every Friday"},
	{"Know how every film ends", "Know how every song goes"},
	{"Go camping under the stars", "Go glamping with proper beds"},
	{"Always find the perfect parking spot", "Always get the best seat"},
	{"Spend the evening at a quiz night", "Spend the evening at a comedy show"},
	{"Have a library in your home", "Have a cinema in your home"},
}

const (
	wyrOpenFor    = 10 * time.Minute // a round closes by itself after this long
	wyrNoRepeatOf = 12               // do not repeat any of the last N questions in a chat
)

// WYRState is a round as one member sees it. Totals appear once the member has
// voted or the round is over, so no one is nudged by the running count, and
// there is no field that says who voted for what.
type WYRState struct {
	Active   bool       `json:"active"`
	PollID   string     `json:"poll_id,omitempty"`
	A        string     `json:"a,omitempty"`
	B        string     `json:"b,omitempty"`
	Closed   bool       `json:"closed"`
	Members  int        `json:"members"`
	Voted    int        `json:"voted"`
	MyChoice string     `json:"my_choice,omitempty"`
	CountA   *int       `json:"count_a,omitempty"`
	CountB   *int       `json:"count_b,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`
}

// wyrAccess proves the caller belongs to either group of a matched group chat
// and returns how many people are in it.
func (s *Service) wyrAccess(ctx context.Context, userID, groupMatchID string) (members int, err error) {
	if _, e := uuid.Parse(groupMatchID); e != nil {
		return 0, ErrNotFound
	}
	err = s.db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM group_members m2 WHERE m2.group_id IN (gm.group_a, gm.group_b))::int
		  FROM group_matches gm
		 WHERE gm.id = $1::uuid AND gm.status = 'matched'
		   AND EXISTS (SELECT 1 FROM group_members m WHERE m.group_id IN (gm.group_a, gm.group_b) AND m.user_id = $2::uuid)`,
		groupMatchID, userID).Scan(&members)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return members, err
}

// WYRState returns the running round, or the one that just finished, or
// {active:false}. A round that is full or has run out of time is closed here.
func (s *Service) WYRState(ctx context.Context, userID, groupMatchID string) (*WYRState, error) {
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
		 WHERE group_match_id=$1::uuid AND kind='wyr' ORDER BY created_at DESC LIMIT 1`, groupMatchID).Scan(&id, &q, &created, &closed)
	if errors.Is(err, pgx.ErrNoRows) {
		return &WYRState{Members: members}, nil
	}
	if err != nil {
		return nil, err
	}
	var voted, ca, cb int
	var mine *string
	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*)::int, COUNT(*) FILTER (WHERE choice='a')::int, COUNT(*) FILTER (WHERE choice='b')::int,
		       MAX(choice) FILTER (WHERE user_id=$2::uuid)
		  FROM group_poll_votes WHERE poll_id=$1::uuid`, id, userID).Scan(&voted, &ca, &cb, &mine); err != nil {
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
		return &WYRState{Members: members}, nil // an old finished round is not shown again
	}
	st := &WYRState{Active: true, PollID: id, A: wouldYouRather[q%len(wouldYouRather)].A, B: wouldYouRather[q%len(wouldYouRather)].B,
		Closed: closed != nil, Members: members, Voted: voted}
	if mine != nil {
		st.MyChoice = *mine
	}
	if mine != nil || closed != nil {
		st.CountA, st.CountB = &ca, &cb
	}
	if closed == nil {
		end := created.Add(wyrOpenFor)
		st.EndsAt = &end
	}
	return st, nil
}

// StartWYR begins a new round. A round that is still running must finish (or
// time out) first, so one member cannot keep skipping the others' votes.
func (s *Service) StartWYR(ctx context.Context, userID, groupMatchID string) (*WYRState, error) {
	if _, err := s.wyrAccess(ctx, userID, groupMatchID); err != nil {
		return nil, err
	}
	// Closes the previous round if it is full or expired.
	cur, err := s.WYRState(ctx, userID, groupMatchID)
	if err != nil {
		return nil, err
	}
	if cur.Active && !cur.Closed {
		return nil, users.ValidationError{Msg: "a round is already running; vote on it or wait for it to finish"}
	}
	if s.otherRoundRunning(ctx, groupMatchID, "wyr") {
		return nil, users.ValidationError{Msg: "another game is running in this chat; let it finish first"}
	}
	rows, err := s.db.Query(ctx, `SELECT question FROM group_polls WHERE group_match_id=$1::uuid AND kind='wyr' ORDER BY created_at DESC LIMIT $2`, groupMatchID, wyrNoRepeatOf)
	if err != nil {
		return nil, err
	}
	recent := map[int]bool{}
	for rows.Next() {
		var q int
		if err := rows.Scan(&q); err != nil {
			rows.Close()
			return nil, err
		}
		recent[q] = true
	}
	rows.Close()
	var choices []int
	for i := range wouldYouRather {
		if !recent[i] {
			choices = append(choices, i)
		}
	}
	if len(choices) == 0 {
		for i := range wouldYouRather {
			choices = append(choices, i)
		}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(choices))))
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO group_polls (group_match_id, question, started_by) VALUES ($1::uuid,$2,$3::uuid)`,
		groupMatchID, choices[n.Int64()], userID); err != nil {
		// Two members tapped "next" at once: the unique index keeps one round.
		return s.WYRState(ctx, userID, groupMatchID)
	}
	return s.WYRState(ctx, userID, groupMatchID)
}

// VoteWYR records the caller's vote on the running round. A vote cannot be
// changed, so totals cannot be flipped after the fact.
func (s *Service) VoteWYR(ctx context.Context, userID, groupMatchID, pollID, choice string) (*WYRState, error) {
	if choice != "a" && choice != "b" {
		return nil, users.ValidationError{Msg: "choice must be a or b"}
	}
	if _, err := s.wyrAccess(ctx, userID, groupMatchID); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(pollID); err != nil {
		return nil, ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO group_poll_votes (poll_id, user_id, choice)
		SELECT p.id, $3::uuid, $4 FROM group_polls p
		 WHERE p.id=$1::uuid AND p.group_match_id=$2::uuid AND p.closed_at IS NULL AND p.created_at > NOW() - make_interval(secs => $5)
		ON CONFLICT DO NOTHING`, pollID, groupMatchID, userID, choice, wyrOpenFor.Seconds())
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		st, err := s.WYRState(ctx, userID, groupMatchID)
		if err != nil {
			return nil, err
		}
		if st.PollID != pollID || st.Closed {
			return nil, users.ValidationError{Msg: "this round has finished"}
		}
		return st, nil // already voted: the first vote stands
	}
	return s.WYRState(ctx, userID, groupMatchID)
}
