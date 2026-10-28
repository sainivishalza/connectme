package games

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"

	"connectme/internal/users"
	"connectme/pkg/metrics"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrBadRequest = errors.New("invalid request")
)

type Service struct {
	db     *pgxpool.Pool
	vetter TextVetter
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// PromptOut is one prompt for a game.
type PromptOut struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func pick(list []string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		return list[0]
	}
	return list[n.Int64()]
}

// Prompt returns a random truth, dare or icebreaker.
func (s *Service) Prompt(kind string) (*PromptOut, error) {
	switch kind {
	case KindTruth:
		return &PromptOut{kind, pick(truths)}, nil
	case KindDare:
		return &PromptOut{kind, pick(dares)}, nil
	case KindIcebreaker:
		return &PromptOut{kind, pick(icebreakers)}, nil
	}
	return nil, users.ValidationError{Msg: "kind must be truth, dare or icebreaker"}
}

// SetInfo describes a quiz set in the list.
type SetInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Questions int    `json:"questions"`
}

func (s *Service) Sets() []SetInfo {
	out := make([]SetInfo, 0, len(Quizzes))
	for _, q := range Quizzes {
		out = append(out, SetInfo{q.ID, q.Title, len(q.Questions)})
	}
	return out
}

// QuestionView is a question as shown to the viewer; Mine is their own answer
// (-1 = unanswered). Theirs and Same are filled in only once both are done.
type QuestionView struct {
	Question
	Mine   int   `json:"mine"`
	Theirs *int  `json:"theirs,omitempty"`
	Same   *bool `json:"same,omitempty"`
}

// Quiz is the viewer's view of a quiz session.
type Quiz struct {
	ID         string         `json:"id"`
	MatchID    string         `json:"match_id"`
	SetID      string         `json:"set_id"`
	Title      string         `json:"title"`
	Questions  []QuestionView `json:"questions"`
	MyAnswered int            `json:"my_answered"`
	// OtherAnswered is a count only: what they chose stays hidden until you
	// have both finished.
	OtherAnswered int  `json:"other_answered"`
	Complete      bool `json:"complete"`
	// Score is the number of questions you answered the same way (set when Complete).
	Score *int `json:"score,omitempty"`
}

// member checks the caller belongs to an active, unblocked match and returns
// the other person's id. Games work in any active match, including an
// anonymous random chat: the answers reveal no identity.
func (s *Service) member(ctx context.Context, userID, matchID string) (other string, err error) {
	if _, perr := uuid.Parse(matchID); perr != nil {
		return "", ErrNotFound
	}
	err = s.db.QueryRow(ctx, `
		SELECT CASE WHEN m.user1_id=$1 THEN m.user2_id ELSE m.user1_id END::text
		  FROM matches m
		  JOIN users a ON a.id=m.user1_id AND NOT a.is_banned
		  JOIN users b ON b.id=m.user2_id AND NOT b.is_banned
		 WHERE m.id=$2::uuid AND m.status='matched' AND $1::uuid IN (m.user1_id, m.user2_id)
		   AND (m.expires_at IS NULL OR m.expires_at > NOW())
		   AND `+users.NotBlockedSQL("m.user1_id", "m.user2_id"), userID, matchID).Scan(&other)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return other, err
}

// Start begins a quiz in a match, or returns the one already in progress.
func (s *Service) Start(ctx context.Context, userID, matchID, setID string) (*Quiz, error) {
	set, ok := QuizByID(setID)
	if !ok {
		return nil, users.ValidationError{Msg: "unknown quiz"}
	}
	other, err := s.member(ctx, userID, matchID)
	if err != nil {
		return nil, err
	}
	// An unfinished quiz is resumed rather than replaced.
	if cur, err := s.Current(ctx, userID, matchID); err == nil && cur != nil && !cur.Complete {
		return cur, nil
	}
	var id string
	if err := s.db.QueryRow(ctx, `INSERT INTO quiz_sessions (match_id, set_id, started_by) VALUES ($1::uuid,$2,$3) RETURNING id::text`,
		matchID, set.ID, userID).Scan(&id); err != nil {
		return nil, err
	}
	_ = other
	metrics.Event("quiz_started")
	return s.view(ctx, userID, id)
}

// Current returns the latest quiz in the match, or nil if there was none.
func (s *Service) Current(ctx context.Context, userID, matchID string) (*Quiz, error) {
	if _, err := s.member(ctx, userID, matchID); err != nil {
		return nil, err
	}
	var id string
	err := s.db.QueryRow(ctx, `SELECT id::text FROM quiz_sessions WHERE match_id=$1::uuid ORDER BY created_at DESC LIMIT 1`, matchID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.view(ctx, userID, id)
}

// Answer records the caller's choice for one question (they may change it
// until they and the other person have both finished).
func (s *Service) Answer(ctx context.Context, userID, sessionID string, question, choice int) (*Quiz, error) {
	if _, err := uuid.Parse(sessionID); err != nil {
		return nil, ErrNotFound
	}
	var matchID, setID string
	if err := s.db.QueryRow(ctx, `SELECT match_id::text, set_id FROM quiz_sessions WHERE id=$1::uuid`, sessionID).Scan(&matchID, &setID); err != nil {
		return nil, ErrNotFound
	}
	if _, err := s.member(ctx, userID, matchID); err != nil {
		return nil, err
	}
	set, ok := QuizByID(setID)
	if !ok || question < 0 || question >= len(set.Questions) || choice < 0 || choice >= len(set.Questions[question].Options) {
		return nil, users.ValidationError{Msg: "that question or option does not exist"}
	}
	cur, err := s.view(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if cur.Complete {
		return nil, users.ValidationError{Msg: "this quiz is finished"}
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO quiz_answers (session_id, user_id, question, choice) VALUES ($1::uuid,$2,$3,$4)
		ON CONFLICT (session_id, user_id, question) DO UPDATE SET choice=EXCLUDED.choice, answered_at=NOW()`,
		sessionID, userID, question, choice); err != nil {
		return nil, err
	}
	return s.view(ctx, userID, sessionID)
}

// view builds the viewer's picture of a session, revealing the other
// person's choices only when both have answered every question.
func (s *Service) view(ctx context.Context, userID, sessionID string) (*Quiz, error) {
	var q Quiz
	var matchID string
	err := s.db.QueryRow(ctx, `SELECT id::text, match_id::text, set_id FROM quiz_sessions WHERE id=$1::uuid`, sessionID).
		Scan(&q.ID, &matchID, &q.SetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	q.MatchID = matchID
	other, err := s.member(ctx, userID, matchID)
	if err != nil {
		return nil, err
	}
	set, ok := QuizByID(q.SetID)
	if !ok {
		return nil, ErrNotFound
	}
	q.Title = set.Title
	mine, theirs := map[int]int{}, map[int]int{}
	rows, err := s.db.Query(ctx, `SELECT user_id::text, question, choice FROM quiz_answers WHERE session_id=$1::uuid`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		var qn, ch int
		if err := rows.Scan(&uid, &qn, &ch); err != nil {
			return nil, err
		}
		if uid == userID {
			mine[qn] = ch
		} else if uid == other {
			theirs[qn] = ch
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	total := len(set.Questions)
	q.MyAnswered, q.OtherAnswered = len(mine), len(theirs)
	q.Complete = q.MyAnswered == total && q.OtherAnswered == total
	score := 0
	for i, qu := range set.Questions {
		v := QuestionView{Question: qu, Mine: -1}
		if c, ok := mine[i]; ok {
			v.Mine = c
		}
		if q.Complete {
			t := theirs[i]
			same := t == mine[i]
			v.Theirs, v.Same = &t, &same
			if same {
				score++
			}
		}
		q.Questions = append(q.Questions, v)
	}
	if q.Complete {
		q.Score = &score
	}
	return &q, nil
}
