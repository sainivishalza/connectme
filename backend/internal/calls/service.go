// Package calls implements one-to-one voice and video calls between matched
// people. Media is carried by LiveKit (see package livekit for the provider); this
// package decides who may call whom, handles ringing, accept, decline and end,
// and mints short-lived room tokens.
//
// Privacy rules, because a call can reveal a face or a voice:
//   - the match must be active, and neither side blocked or banned;
//   - an anonymous random chat can be called only after BOTH people kept it;
//   - a VIDEO call in an incognito or hotel match needs mutual reveal first.
package calls

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectme/internal/livekit"
	"connectme/internal/push"
	"connectme/internal/users"
	"connectme/pkg/metrics"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// RingTime is how long a call rings before it counts as missed.
	RingTime = 45 * time.Second
	// MaxDuration caps one call.
	MaxDuration = 2 * time.Hour
	tokenTTL    = MaxDuration
)

var (
	ErrNotFound    = errors.New("not found")
	ErrForbidden   = errors.New("you cannot call this person")
	ErrBusy        = errors.New("one of you is already in a call")
	ErrNeedsKeep   = errors.New("keep chatting first: calls open once you both keep an anonymous chat")
	ErrNeedsReveal = errors.New("video calls with an anonymous match need you both to reveal first")
	ErrNotRinging  = errors.New("this call is no longer ringing")
)

type Service struct {
	db       *pgxpool.Pool
	users    *users.Service
	provider livekit.Provider
	notifier push.Notifier
	now      func() time.Time
}

// SetNotifier enables push notifications (nil keeps them off).
func (s *Service) SetNotifier(n push.Notifier) { s.notifier = n }

func NewService(db *pgxpool.Pool, u *users.Service, p livekit.Provider) *Service {
	return &Service{db: db, users: u, provider: p, now: time.Now}
}

// Call is the public view of a call.
type Call struct {
	ID      string               `json:"id"`
	MatchID string               `json:"match_id"`
	Video   bool                 `json:"video"`
	Status  string               `json:"status"`
	Mine    bool                 `json:"mine"` // true when the caller is the viewer
	Peer    *users.PublicProfile `json:"peer,omitempty"`
}

// Connect is what a client needs to join the media room.
type Connect struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	Room  string `json:"room"`
}

// Started is the response to placing a call.
type Started struct {
	Call    Call    `json:"call"`
	Connect Connect `json:"connect"`
}

func (s *Service) connect(callID, userID string) (Connect, error) {
	tok, err := s.provider.Token("call-"+callID, userID, true, tokenTTL)
	if err != nil {
		return Connect{}, err
	}
	return Connect{URL: s.provider.URL(), Token: tok, Room: "call-" + callID}, nil
}

func (s *Service) sweep(ctx context.Context) {
	if _, err := s.db.Exec(ctx, `UPDATE calls SET status='missed', ended_at=$1
		WHERE status='ringing' AND created_at < $2`, s.now(), s.now().Add(-RingTime)); err != nil {
		slog.Warn("calls: sweep missed", "err", err)
	}
	rows, err := s.db.Query(ctx, `UPDATE calls SET status='ended', ended_at=$1
		WHERE status='active' AND answered_at < $2 RETURNING id::text`, s.now(), s.now().Add(-MaxDuration))
	if err != nil {
		slog.Warn("calls: sweep long", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			s.closeRoom(id)
		}
	}
}

func (s *Service) closeRoom(callID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := s.provider.Close(ctx, "call-"+callID); err != nil {
			slog.Warn("calls: closing media room failed", "call", callID, "err", err)
		}
	}()
}

// Start places a call to the other person in a match.
func (s *Service) Start(ctx context.Context, callerID, matchID string, video bool) (*Started, error) {
	if _, err := uuid.Parse(matchID); err != nil {
		return nil, ErrNotFound
	}
	s.sweep(ctx)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		calleeID, mode, origin string
		keptN                  int
		verified               bool
		revealed               int
	)
	err = tx.QueryRow(ctx, `
		SELECT CASE WHEN m.user1_id=$1 THEN m.user2_id ELSE m.user1_id END::text, m.mode, m.origin,
		       cardinality(m.kept_by),
		       (SELECT is_verified AND NOT is_banned FROM users WHERE id=$1),
		       (SELECT COUNT(*) FROM reveal_consents r WHERE r.match_id=m.id)
		  FROM matches m
		  JOIN users a ON a.id=m.user1_id AND NOT a.is_banned
		  JOIN users b ON b.id=m.user2_id AND NOT b.is_banned
		 WHERE m.id=$2::uuid AND m.status='matched' AND $1::uuid IN (m.user1_id, m.user2_id)
		   AND (m.expires_at IS NULL OR m.expires_at > NOW())
		   AND `+users.NotBlockedSQL("m.user1_id", "m.user2_id"), callerID, matchID).
		Scan(&calleeID, &mode, &origin, &keptN, &verified, &revealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !verified {
		return nil, ErrForbidden
	}
	if origin == "random" && keptN < 2 {
		return nil, ErrNeedsKeep
	}
	if video && mode != "open" && revealed < 2 {
		return nil, ErrNeedsReveal
	}
	// One call at a time per person; lock both rows so two calls cannot race in.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id IN ($1,$2) ORDER BY id FOR UPDATE`, callerID, calleeID); err != nil {
		return nil, err
	}
	var busy bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM calls WHERE status IN ('ringing','active')
		                AND ($1::uuid IN (caller_id, callee_id) OR $2::uuid IN (caller_id, callee_id)))`,
		callerID, calleeID).Scan(&busy); err != nil {
		return nil, err
	}
	if busy {
		return nil, ErrBusy
	}
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO calls (match_id, caller_id, callee_id, video) VALUES ($1::uuid,$2,$3,$4) RETURNING id::text`,
		matchID, callerID, calleeID, video).Scan(&id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	conn, err := s.connect(id, callerID)
	if err != nil {
		return nil, err
	}
	metrics.Event("call_started")
	if s.notifier != nil {
		s.notifier.Notify(ctx, calleeID, push.KindCall, id)
	}
	call, err := s.view(ctx, callerID, id)
	if err != nil {
		return nil, err
	}
	return &Started{Call: *call, Connect: conn}, nil
}

// view loads a call as one of its participants may see it.
func (s *Service) view(ctx context.Context, userID, callID string) (*Call, error) {
	var c Call
	var caller, callee string
	err := s.db.QueryRow(ctx, `
		SELECT id::text, match_id::text, video, status, caller_id::text, callee_id::text
		  FROM calls WHERE id=$1::uuid AND $2::uuid IN (caller_id, callee_id)`, callID, userID).
		Scan(&c.ID, &c.MatchID, &c.Video, &c.Status, &caller, &callee)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Mine = caller == userID
	other := caller
	if c.Mine {
		other = callee
	}
	if card, err := s.users.Card(ctx, userID, other, ""); err == nil {
		c.Peer = card
	}
	return &c, nil
}

// Get returns a call to either participant (the caller polls this to learn
// whether it was answered, declined or missed).
func (s *Service) Get(ctx context.Context, userID, callID string) (*Call, error) {
	if _, err := uuid.Parse(callID); err != nil {
		return nil, ErrNotFound
	}
	s.sweep(ctx)
	return s.view(ctx, userID, callID)
}

// Incoming returns the call currently ringing for the user, if any.
func (s *Service) Incoming(ctx context.Context, userID string) (*Call, error) {
	s.sweep(ctx)
	var id string
	err := s.db.QueryRow(ctx, `
		SELECT c.id::text FROM calls c
		  JOIN users u ON u.id=c.caller_id AND NOT u.is_banned
		 WHERE c.callee_id=$1 AND c.status='ringing' AND c.created_at >= $2
		   AND `+users.NotBlockedSQL("c.caller_id", "c.callee_id")+`
		 ORDER BY c.created_at DESC LIMIT 1`, userID, s.now().Add(-RingTime)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.view(ctx, userID, id)
}

// Accept answers a ringing call and returns the callee's room token.
func (s *Service) Accept(ctx context.Context, userID, callID string) (*Started, error) {
	if _, err := uuid.Parse(callID); err != nil {
		return nil, ErrNotFound
	}
	s.sweep(ctx)
	tag, err := s.db.Exec(ctx, `
		UPDATE calls SET status='active', answered_at=$3
		 WHERE id=$1::uuid AND callee_id=$2 AND status='ringing' AND created_at >= $4`,
		callID, userID, s.now(), s.now().Add(-RingTime))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if _, verr := s.view(ctx, userID, callID); verr != nil {
			return nil, ErrNotFound
		}
		return nil, ErrNotRinging
	}
	conn, err := s.connect(callID, userID)
	if err != nil {
		return nil, err
	}
	call, err := s.view(ctx, userID, callID)
	if err != nil {
		return nil, err
	}
	metrics.Event("call_answered")
	return &Started{Call: *call, Connect: conn}, nil
}

// Decline rejects a ringing call.
func (s *Service) Decline(ctx context.Context, userID, callID string) error {
	if _, err := uuid.Parse(callID); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE calls SET status='declined', ended_at=$3
		 WHERE id=$1::uuid AND callee_id=$2 AND status='ringing'`, callID, userID, s.now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	s.closeRoom(callID)
	return nil
}

// End hangs up (or, for the caller, cancels while ringing). Ending a call that
// already ended is a no-op.
func (s *Service) End(ctx context.Context, userID, callID string) error {
	if _, err := uuid.Parse(callID); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE calls SET status='ended', ended_at=$3
		 WHERE id=$1::uuid AND $2::uuid IN (caller_id, callee_id) AND status IN ('ringing','active')`,
		callID, userID, s.now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, verr := s.view(ctx, userID, callID); verr != nil {
			return ErrNotFound
		}
		return nil
	}
	s.closeRoom(callID)
	return nil
}

// EndAllFor ends every open call a user is in (used when they are banned).
func (s *Service) EndAllFor(ctx context.Context, userID string) {
	rows, err := s.db.Query(ctx, `
		UPDATE calls SET status='ended', ended_at=$2
		 WHERE status IN ('ringing','active') AND $1::uuid IN (caller_id, callee_id) RETURNING id::text`, userID, s.now())
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			s.closeRoom(id)
		}
	}
}
