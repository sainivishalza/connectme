package live

import (
	"context"
	"errors"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Requests and invites older than 30 minutes (written into each query below) are ignored.

// maxPendingRequests keeps a popular room's hand-raise list readable.
const maxPendingRequests = 30

// StageRequest is one person waiting to be let on stage.
type StageRequest struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
}

// StageState is what the caller sees about the stage: their own place in it,
// and, for the host and co-hosts, who is asking to come up.
type StageState struct {
	Me       string         `json:"me"` // none | requested | invited | on_stage
	Requests []StageRequest `json:"requests"`
	Guests   int            `json:"guests"`
	Max      int            `json:"max"`
}

// openMemberByRef finds a verified Open-mode member the actor may put on stage.
func (s *Service) openMemberByRef(ctx context.Context, actorID, ref string) (string, error) {
	if _, err := uuid.Parse(ref); err != nil {
		return "", ErrNotFound
	}
	var target string
	err := s.db.QueryRow(ctx, `
		SELECT p.user_id::text FROM profiles p JOIN users u ON u.id = p.user_id
		 WHERE p.public_ref = $1::uuid AND u.is_verified AND NOT u.is_banned AND p.mode = 'open' AND NOT p.invisible
		   AND `+users.NotBlockedSQL("p.user_id", "$2::uuid"), ref, actorID).Scan(&target)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return target, err
}

// canStand reports whether the caller may appear on camera: verified, not
// banned and shown in full (Open mode), never an anonymous or hidden profile.
func (s *Service) canStand(ctx context.Context, userID string) bool {
	var ok bool
	_ = s.db.QueryRow(ctx, `
		SELECT u.is_verified AND NOT u.is_banned AND p.mode = 'open' AND NOT p.invisible
		  FROM users u JOIN profiles p ON p.user_id = u.id WHERE u.id = $1::uuid`, userID).Scan(&ok)
	return ok
}

// addGuest makes target a co-host, up to the room's limit.
func (s *Service) addGuest(ctx context.Context, roomID, target string) error {
	var n int
	var already bool
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*)::int, COALESCE(BOOL_OR(user_id=$2::uuid), FALSE) FROM live_cohosts WHERE room_id=$1::uuid`,
		roomID, target).Scan(&n, &already); err != nil {
		return err
	}
	if already {
		return nil
	}
	if n >= maxCohosts {
		return users.ValidationError{Msg: "the stage is full"}
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO live_cohosts (room_id, user_id) VALUES ($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, roomID, target); err != nil {
		return err
	}
	go s.setPublish(roomID, target, true)
	return nil
}

func (s *Service) clearStageRow(ctx context.Context, roomID, userID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM live_stage_requests WHERE room_id=$1::uuid AND user_id=$2::uuid`, roomID, userID)
	return err
}

// RequestStage lets a viewer ask to join the people on camera. If the host has
// already invited them, asking accepts that invite.
func (s *Service) RequestStage(ctx context.Context, userID, roomID string) error {
	hostID, err := s.visible(ctx, userID, roomID)
	if err != nil {
		return err
	}
	if hostID == userID || s.isCohost(ctx, roomID, userID) {
		return users.ValidationError{Msg: "you are already on stage"}
	}
	if !s.canStand(ctx, userID) {
		return users.ValidationError{Msg: "only members shown in full (Open mode) can join on camera"}
	}
	var kind string
	err = s.db.QueryRow(ctx, `SELECT kind FROM live_stage_requests WHERE room_id=$1::uuid AND user_id=$2::uuid AND created_at > NOW() - INTERVAL '30 minutes'`,
		roomID, userID).Scan(&kind)
	if err == nil && kind == "invite" {
		if err := s.addGuest(ctx, roomID, userID); err != nil {
			return err
		}
		return s.clearStageRow(ctx, roomID, userID)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var pending int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*)::int FROM live_stage_requests WHERE room_id=$1::uuid AND kind='request' AND created_at > NOW() - INTERVAL '30 minutes'`,
		roomID).Scan(&pending); err != nil {
		return err
	}
	if pending >= maxPendingRequests {
		return users.ValidationError{Msg: "too many people are waiting; try again in a moment"}
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO live_stage_requests (room_id, user_id, kind) VALUES ($1::uuid,$2::uuid,'request')
		ON CONFLICT (room_id, user_id) DO UPDATE SET kind='request', created_at=NOW()`, roomID, userID)
	return err
}

// InviteStage lets the host or a co-host ask a member to come on camera. If
// that member had already asked, they are let up straight away.
func (s *Service) InviteStage(ctx context.Context, actorID, roomID, ref string) error {
	if _, err := uuid.Parse(roomID); err != nil {
		return ErrNotFound
	}
	if ok, err := s.canRun(ctx, roomID, actorID); err != nil || !ok {
		if err != nil {
			return err
		}
		return ErrNotFound
	}
	target, err := s.openMemberByRef(ctx, actorID, ref)
	if err != nil {
		return err
	}
	if target == actorID || s.isCohost(ctx, roomID, target) {
		return users.ValidationError{Msg: "they are already on stage"}
	}
	var host string
	if err := s.db.QueryRow(ctx, `SELECT host_id::text FROM live_rooms WHERE id=$1::uuid`, roomID).Scan(&host); err != nil {
		return ErrNotFound
	}
	if target == host {
		return users.ValidationError{Msg: "that is the host"}
	}
	var kind string
	err = s.db.QueryRow(ctx, `SELECT kind FROM live_stage_requests WHERE room_id=$1::uuid AND user_id=$2::uuid`, roomID, target).Scan(&kind)
	if err == nil && kind == "request" {
		if err := s.addGuest(ctx, roomID, target); err != nil {
			return err
		}
		return s.clearStageRow(ctx, roomID, target)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO live_stage_requests (room_id, user_id, kind) VALUES ($1::uuid,$2::uuid,'invite')
		ON CONFLICT (room_id, user_id) DO UPDATE SET kind='invite', created_at=NOW()`, roomID, target)
	return err
}

// DecideStage is the host's (or a co-host's) answer to a raised hand.
func (s *Service) DecideStage(ctx context.Context, actorID, roomID, ref string, accept bool) error {
	if _, err := uuid.Parse(roomID); err != nil {
		return ErrNotFound
	}
	if ok, err := s.canRun(ctx, roomID, actorID); err != nil || !ok {
		if err != nil {
			return err
		}
		return ErrNotFound
	}
	target, err := s.openMemberByRef(ctx, actorID, ref)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM live_stage_requests WHERE room_id=$1::uuid AND user_id=$2::uuid AND kind='request'`, roomID, target)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if accept {
		return s.addGuest(ctx, roomID, target)
	}
	return nil
}

// RespondStage is a viewer's answer to the host's invite.
func (s *Service) RespondStage(ctx context.Context, userID, roomID string, accept bool) error {
	if _, err := s.visible(ctx, userID, roomID); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM live_stage_requests WHERE room_id=$1::uuid AND user_id=$2::uuid AND kind='invite' AND created_at > NOW() - INTERVAL '30 minutes'`, roomID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if accept {
		if !s.canStand(ctx, userID) {
			return users.ValidationError{Msg: "only members shown in full (Open mode) can join on camera"}
		}
		return s.addGuest(ctx, roomID, userID)
	}
	return nil
}

// LeaveStage withdraws a request, declines an invite, or steps down from the
// stage and goes back to watching.
func (s *Service) LeaveStage(ctx context.Context, userID, roomID string) error {
	if _, err := s.visible(ctx, userID, roomID); err != nil {
		return err
	}
	if err := s.clearStageRow(ctx, roomID, userID); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM live_cohosts WHERE room_id=$1::uuid AND user_id=$2::uuid`, roomID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		go s.setPublish(roomID, userID, false)
	}
	return nil
}

// Stage describes the stage for one caller.
func (s *Service) Stage(ctx context.Context, userID, roomID string) StageState {
	st := StageState{Me: "none", Requests: []StageRequest{}, Max: maxCohosts}
	hostID, err := s.visible(ctx, userID, roomID)
	if err != nil {
		return st
	}
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*)::int FROM live_cohosts WHERE room_id=$1::uuid`, roomID).Scan(&st.Guests)
	if hostID == userID || s.isCohost(ctx, roomID, userID) {
		st.Me = "on_stage"
	} else {
		var kind string
		if s.db.QueryRow(ctx, `SELECT kind FROM live_stage_requests WHERE room_id=$1::uuid AND user_id=$2::uuid AND created_at > NOW() - INTERVAL '30 minutes'`,
			roomID, userID).Scan(&kind) == nil {
			if kind == "invite" {
				st.Me = "invited"
			} else {
				st.Me = "requested"
			}
		}
	}
	if st.Me != "on_stage" {
		return st
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.public_ref::text, COALESCE(NULLIF(p.display_name,''),'Someone')
		  FROM live_stage_requests r JOIN profiles p ON p.user_id = r.user_id
		 WHERE r.room_id=$1::uuid AND r.kind='request' AND r.created_at > NOW() - INTERVAL '30 minutes'
		   AND `+users.NotBlockedSQL("r.user_id", "$2::uuid")+`
		 ORDER BY r.created_at LIMIT 30`, roomID, userID)
	if err != nil {
		return st
	}
	defer rows.Close()
	for rows.Next() {
		var q StageRequest
		if rows.Scan(&q.Ref, &q.Name) == nil {
			st.Requests = append(st.Requests, q)
		}
	}
	return st
}
