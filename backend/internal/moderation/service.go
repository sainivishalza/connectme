// Package moderation implements block, report and moderator actions.
package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"connectme/internal/users"
	"connectme/internal/videos"
	"connectme/pkg/middleware"
	"connectme/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrSelf      = errors.New("you cannot do that to yourself")
	ErrForbidden = errors.New("forbidden")
)

var reasons = map[string]bool{
	"spam": true, "harassment": true, "fake_profile": true, "inappropriate_content": true,
	"underage": true, "scam": true, "other": true,
}

// SessionRevoker ends all of a user's sessions (satisfied by *auth.TokenStore).
type SessionRevoker interface {
	RevokeAllForUser(ctx context.Context, userID string) error
}

// StreamEnder lets moderation stop live streams (implemented by live.Service).
type StreamEnder interface {
	EndRoom(ctx context.Context, roomID string) (hostID string, err error)
	EndByHost(ctx context.Context, hostID string)
}

// VideoModerator is what staff need from the video feature (videos.Service).
type VideoModerator interface {
	StaffRemove(ctx context.Context, id string) (author, kind string, err error)
	Held(ctx context.Context) ([]videos.HeldVideo, error)
	Approve(ctx context.Context, id, reviewerID string) (author string, err error)
	StaffTicket(ctx context.Context, id string) (path, author string, err error)
}

// ForumRemover lets staff delete a forum thread or reply (implemented by forums.Service).
type ForumRemover interface {
	StaffRemove(ctx context.Context, kind, id string) (author string, err error)
}

// CallEnder lets moderation hang up a banned person's calls (implemented by calls.Service).
type CallEnder interface {
	EndAllFor(ctx context.Context, userID string)
}

type Service struct {
	db      *pgxpool.Pool
	users   *users.Service
	revoker SessionRevoker
	streams StreamEnder
	calls   CallEnder
	audio   CallEnder
	videos  VideoModerator
	forums  ForumRemover
}

// SetCalls enables ending calls on a ban (nil when calls are off).
func (s *Service) SetCalls(c CallEnder) { s.calls = c }

// SetForums enables staff removal of forum posts.
func (s *Service) SetForums(f ForumRemover) { s.forums = f }

// SetVideos enables staff removal of videos (nil when videos are off).
func (s *Service) SetVideos(v VideoModerator) { s.videos = v }

// SetAudio enables ending audio rooms on a ban (nil when audio is off).
func (s *Service) SetAudio(c CallEnder) { s.audio = c }

// SetStreams enables ending live streams from moderation (nil when live is off).
func (s *Service) SetStreams(e StreamEnder) { s.streams = e }

func NewService(db *pgxpool.Pool, u *users.Service, r SessionRevoker) *Service {
	return &Service{db: db, users: u, revoker: r}
}

type ReportRequest struct {
	Ref      string   `json:"ref"`
	Reason   string   `json:"reason"`
	Details  string   `json:"details"`
	Evidence []string `json:"evidence"` // decrypted messages supplied by the reporter (chats are E2E)
}

type ReportView struct {
	ID          string    `json:"id"`
	ReporterRef string    `json:"reporter_ref,omitempty"`
	ReportedRef string    `json:"reported_ref"`
	Reason      string    `json:"reason"`
	Details     string    `json:"details"`
	Evidence    []string  `json:"evidence"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`

	// Context for the reviewer (list view only).
	ReportedBanned bool   `json:"reported_banned"`
	ReportedRole   string `json:"reported_role,omitempty"`
}

// Block hides two people from each other everywhere and ends any match.
func (s *Service) Block(ctx context.Context, userID, ref string) error {
	target, err := s.target(ctx, userID, ref)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx,
		`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, userID, target); err != nil {
		return err
	}
	u1, u2 := userID, target
	if u2 < u1 {
		u1, u2 = u2, u1
	}
	if _, err := tx.Exec(ctx, `
		WITH m AS (UPDATE matches SET status='rejected' WHERE user1_id=$1 AND user2_id=$2 RETURNING id)
		DELETE FROM reveal_consents WHERE match_id IN (SELECT id FROM m)`, u1, u2); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) Unblock(ctx context.Context, userID, ref string) error {
	target, err := s.target(ctx, userID, ref)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `DELETE FROM blocks WHERE blocker_id=$1 AND blocked_id=$2`, userID, target)
	return err
}

func (s *Service) target(ctx context.Context, userID, ref string) (string, error) {
	id, err := s.users.ResolveRef(ctx, ref)
	if errors.Is(err, users.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if id == userID {
		return "", ErrSelf
	}
	return id, nil
}

// Report files a report for moderators. Reporters are never revealed to the
// reported user.
func (s *Service) Report(ctx context.Context, userID string, req ReportRequest) error {
	if !reasons[req.Reason] {
		return users.ValidationError{Msg: "unknown reason"}
	}
	target, err := s.target(ctx, userID, req.Ref)
	if err != nil {
		return err
	}
	details := middleware.SanitizeText(utils.StripTags(req.Details), 1000)
	if len(req.Evidence) > 20 {
		return users.ValidationError{Msg: "at most 20 evidence items"}
	}
	ev := make([]string, 0, len(req.Evidence))
	for _, e := range req.Evidence {
		ev = append(ev, middleware.SanitizeText(e, 500))
	}
	evJSON, _ := json.Marshal(ev)
	_, err = s.db.Exec(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason, details, evidence)
		VALUES ($1,$2,$3,$4,$5)`, userID, target, req.Reason, details, evJSON)
	return err
}

// List returns reports for moderators, oldest first.
func (s *Service) List(ctx context.Context, status string, limit, offset int) ([]ReportView, error) {
	rows, err := s.db.Query(ctx, `
		SELECT r.id::text, COALESCE(rp.public_ref::text,''), tp.public_ref::text,
		       r.reason, COALESCE(r.details,''), COALESCE(r.evidence,'[]'::jsonb), r.status, r.created_at,
		       u.is_banned, u.role
		  FROM reports r
		  JOIN profiles tp ON tp.user_id = r.reported_id
		  JOIN users u ON u.id = r.reported_id
		  LEFT JOIN profiles rp ON rp.user_id = r.reporter_id
		 WHERE r.status = $1 ORDER BY r.created_at LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportView{}
	for rows.Next() {
		var v ReportView
		var ev []byte
		if err := rows.Scan(&v.ID, &v.ReporterRef, &v.ReportedRef, &v.Reason, &v.Details, &ev, &v.Status, &v.CreatedAt,
			&v.ReportedBanned, &v.ReportedRole); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(ev, &v.Evidence)
		out = append(out, v)
	}
	return out, rows.Err()
}

// Resolve closes a report as dismissed or bans the reported user (ending all
// their sessions). Moderators cannot act on moderators or admins.
func (s *Service) Resolve(ctx context.Context, actorID, actorRole, reportID, action string) error {
	if _, err := uuid.Parse(reportID); err != nil {
		return ErrNotFound
	}
	if action != "dismiss" && action != "ban" {
		return users.ValidationError{Msg: "action must be dismiss or ban"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var reported, targetRole, status string
	err = tx.QueryRow(ctx, `
		SELECT r.reported_id::text, u.role, r.status FROM reports r JOIN users u ON u.id = r.reported_id
		 WHERE r.id=$1 FOR UPDATE OF r`, reportID).Scan(&reported, &targetRole, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "open" && status != "reviewing" {
		return users.ValidationError{Msg: "report already resolved"}
	}
	if action == "ban" {
		if (targetRole != "user" && actorRole != "admin") || reported == actorID {
			return ErrForbidden
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET is_banned=TRUE, updated_at=NOW() WHERE id=$1`, reported); err != nil {
			return err
		}
	}
	newStatus := map[string]string{"dismiss": "dismissed", "ban": "actioned"}[action]
	if _, err := tx.Exec(ctx,
		`UPDATE reports SET status=$2, resolved_by=$3, resolved_at=NOW() WHERE id=$1`, reportID, newStatus, actorID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := s.audit(ctx, Actor{ID: actorID, Role: actorRole}, ActResolve, reported, reportID,
		map[string]any{"action": action}); err != nil {
		return err
	}
	if action == "ban" {
		// After commit. Banned users are also rejected at refresh time, so a
		// failure here only delays session death by one access-token lifetime.
		return s.revoker.RevokeAllForUser(ctx, reported)
	}
	return nil
}
