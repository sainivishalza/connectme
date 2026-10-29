package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Actor is the authenticated staff member performing an action.
type Actor struct{ ID, Role string }

// Staff actions that are written to the audit log.
const (
	ActViewUser       = "view_user"
	ActViewPhoto      = "view_photo"
	ActBan            = "ban_user"
	ActUnban          = "unban_user"
	ActResolve        = "resolve_report"
	ActListAudit      = "view_audit_log"
	ActRemovePost     = "remove_post"
	ActRemoveVideo    = "remove_video"
	ActRemoveForum    = "remove_forum_post"
	ActApproveVideo   = "approve_video"
	ActRevokeSessions = "revoke_sessions"
	ActViewVideo      = "view_video"
)

// audit appends to the immutable staff log. A failure to record is returned:
// staff must not be able to act or look without leaving a trace.
func (s *Service) audit(ctx context.Context, a Actor, action, targetUserID, reportID string, detail map[string]any) error {
	d, _ := json.Marshal(detail)
	_, err := s.db.Exec(ctx, `
		INSERT INTO staff_audit_log (actor_id, actor_role, action, target_user_id, report_id, detail)
		VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid, NULLIF($5,'')::uuid, $6)`,
		a.ID, a.Role, action, targetUserID, reportID, d)
	return err
}

type StaffUser struct {
	Ref           string       `json:"ref"`
	DisplayName   string       `json:"display_name"`
	Bio           string       `json:"bio"`
	Intro         string       `json:"intro"`
	Mood          string       `json:"mood"`
	Tags          []string     `json:"tags"`
	Mode          string       `json:"mode"`
	Age           int          `json:"age"`
	PhotoIDs      []string     `json:"photo_ids"`
	Role          string       `json:"role"`
	Banned        bool         `json:"banned"`
	EmailVerified bool         `json:"email_verified"`
	IDVerified    bool         `json:"id_verified"`
	CreatedAt     time.Time    `json:"created_at"`
	Matches       int          `json:"matches"`
	Groups        int          `json:"groups"`
	ReportsAbout  int          `json:"reports_about"`
	OpenReports   int          `json:"open_reports"`
	ReportsFiled  int          `json:"reports_filed"`
	Recent        []ReportView `json:"recent_reports"`
}

// ViewUser returns the staff view of a profile: what moderation needs, and
// deliberately not email, phone, date of birth, coordinates or chat content.
func (s *Service) ViewUser(ctx context.Context, a Actor, ref string) (*StaffUser, error) {
	id, err := s.users.ResolveRef(ctx, ref)
	if errors.Is(err, users.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var u StaffUser
	var name, bio, intro, mood *string
	var photos []byte
	err = s.db.QueryRow(ctx, `
		SELECT p.public_ref::text, p.display_name, p.bio, p.intro, p.mood, p.tags, p.mode, p.photos,
		       date_part('year', age(x.date_of_birth))::int, x.role, x.is_banned, x.is_verified,
		       x.id_verified_at IS NOT NULL, x.created_at,
		       (SELECT COUNT(*) FROM matches m WHERE m.user1_id=x.id OR m.user2_id=x.id),
		       (SELECT COUNT(*) FROM group_members g WHERE g.user_id=x.id),
		       (SELECT COUNT(*) FROM reports r WHERE r.reported_id=x.id),
		       (SELECT COUNT(*) FROM reports r WHERE r.reported_id=x.id AND r.status IN ('open','reviewing')),
		       (SELECT COUNT(*) FROM reports r WHERE r.reporter_id=x.id)
		  FROM users x JOIN profiles p ON p.user_id = x.id WHERE x.id=$1::uuid`, id).
		Scan(&u.Ref, &name, &bio, &intro, &mood, &u.Tags, &u.Mode, &photos, &u.Age, &u.Role, &u.Banned,
			&u.EmailVerified, &u.IDVerified, &u.CreatedAt, &u.Matches, &u.Groups, &u.ReportsAbout,
			&u.OpenReports, &u.ReportsFiled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.DisplayName, u.Bio, u.Intro, u.Mood = deref(name), deref(bio), deref(intro), deref(mood)
	if u.Tags == nil {
		u.Tags = []string{}
	}
	var ph []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(photos, &ph)
	u.PhotoIDs = []string{}
	for _, p := range ph {
		u.PhotoIDs = append(u.PhotoIDs, p.ID)
	}
	if u.Recent, err = s.reportsFor(ctx, id, 10); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, a, ActViewUser, id, "", nil); err != nil {
		return nil, err
	}
	return &u, nil
}

// StaffPhoto returns a photo's bytes for review. Access is limited to users
// with an open report, so staff cannot browse arbitrary members' photos.
func (s *Service) StaffPhoto(ctx context.Context, a Actor, ref, photoID string) ([]byte, error) {
	if _, err := uuid.Parse(photoID); err != nil {
		return nil, ErrNotFound
	}
	id, err := s.users.ResolveRef(ctx, ref)
	if err != nil {
		return nil, ErrNotFound
	}
	filter, _ := json.Marshal([]map[string]string{{"id": photoID}})
	var ok bool
	err = s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM profiles p WHERE p.user_id=$1::uuid AND p.photos @> $2::jsonb)
		   AND EXISTS (SELECT 1 FROM reports r WHERE r.reported_id=$1::uuid AND r.status IN ('open','reviewing'))`,
		id, string(filter)).Scan(&ok)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	if err := s.audit(ctx, a, ActViewPhoto, id, "", map[string]any{"photo_id": photoID}); err != nil {
		return nil, err
	}
	b, err := s.users.ReadPhotoUnchecked(ctx, photoID)
	if errors.Is(err, users.ErrNotFound) {
		return nil, ErrNotFound
	}
	return b, err
}

// SetBan bans or unbans a user directly (outside a report). The same staff
// hierarchy applies as when resolving a report.
func (s *Service) SetBan(ctx context.Context, a Actor, ref string, ban bool, reason string) error {
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 5 || n > 300 {
		return users.ValidationError{Msg: "a reason of 5-300 characters is required"}
	}
	id, err := s.users.ResolveRef(ctx, ref)
	if errors.Is(err, users.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if id == a.ID {
		return ErrSelf
	}
	var role string
	if err := s.db.QueryRow(ctx, `SELECT role FROM users WHERE id=$1::uuid`, id).Scan(&role); err != nil {
		return ErrNotFound
	}
	if role != "user" && a.Role != "admin" {
		return ErrForbidden
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET is_banned=$2, updated_at=NOW() WHERE id=$1::uuid`, id, ban); err != nil {
		return err
	}
	if ban && s.calls != nil {
		s.calls.EndAllFor(ctx, id) // and so do their calls
	}
	act := ActBan
	if !ban {
		act = ActUnban
	}
	if err := s.audit(ctx, a, act, id, "", map[string]any{"reason": reason}); err != nil {
		return err
	}
	if ban {
		return s.revoker.RevokeAllForUser(ctx, id)
	}
	return nil
}

type Stats struct {
	OpenReports    int `json:"open_reports"`
	OldestOpenDays int `json:"oldest_open_days"`
	AutoFlags24h   int `json:"auto_flags_24h"`
	Reports24h     int `json:"user_reports_24h"`
	BannedUsers    int `json:"banned_users"`
	ActionsToday   int `json:"staff_actions_today"`
	HeldVideos     int `json:"held_videos"`
}

func (s *Service) Stats(ctx context.Context) (*Stats, error) {
	var st Stats
	err := s.db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM reports WHERE status IN ('open','reviewing')),
		       COALESCE((SELECT EXTRACT(DAY FROM NOW() - MIN(created_at))::int FROM reports WHERE status IN ('open','reviewing')), 0),
		       (SELECT COUNT(*) FROM reports WHERE reason='auto_review' AND created_at > NOW() - INTERVAL '24 hours'),
		       (SELECT COUNT(*) FROM reports WHERE reason<>'auto_review' AND created_at > NOW() - INTERVAL '24 hours'),
		       (SELECT COUNT(*) FROM users WHERE is_banned),
		       (SELECT COUNT(*) FROM staff_audit_log WHERE created_at > date_trunc('day', NOW())),
		       (SELECT COUNT(*) FROM videos WHERE status = 'held')`).
		Scan(&st.OpenReports, &st.OldestOpenDays, &st.AutoFlags24h, &st.Reports24h, &st.BannedUsers, &st.ActionsToday, &st.HeldVideos)
	return &st, err
}

type AuditEntry struct {
	ID        int64          `json:"id"`
	At        time.Time      `json:"at"`
	ActorRef  string         `json:"actor_ref"`
	ActorRole string         `json:"actor_role"`
	Action    string         `json:"action"`
	TargetRef string         `json:"target_ref"`
	ReportID  string         `json:"report_id,omitempty"`
	Detail    map[string]any `json:"detail"`
}

// Audit lists the log, newest first (admins only; enforced by the router).
func (s *Service) Audit(ctx context.Context, a Actor, limit, offset int) ([]AuditEntry, error) {
	rows, err := s.db.Query(ctx, `
		SELECT l.id, l.created_at, COALESCE(ap.public_ref::text,''), l.actor_role, l.action,
		       COALESCE(tp.public_ref::text,''), COALESCE(l.report_id::text,''), l.detail
		  FROM staff_audit_log l
		  LEFT JOIN profiles ap ON ap.user_id = l.actor_id
		  LEFT JOIN profiles tp ON tp.user_id = l.target_user_id
		 ORDER BY l.id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var d []byte
		if err := rows.Scan(&e.ID, &e.At, &e.ActorRef, &e.ActorRole, &e.Action, &e.TargetRef, &e.ReportID, &d); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(d, &e.Detail)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reading the audit log is itself recorded.
	return out, s.audit(ctx, a, ActListAudit, "", "", map[string]any{"limit": limit, "offset": offset})
}

func (s *Service) reportsFor(ctx context.Context, userID string, limit int) ([]ReportView, error) {
	rows, err := s.db.Query(ctx, `
		SELECT r.id::text, COALESCE(rp.public_ref::text,''), tp.public_ref::text,
		       r.reason, COALESCE(r.details,''), COALESCE(r.evidence,'[]'::jsonb), r.status, r.created_at
		  FROM reports r
		  JOIN profiles tp ON tp.user_id = r.reported_id
		  LEFT JOIN profiles rp ON rp.user_id = r.reporter_id
		 WHERE r.reported_id=$1::uuid ORDER BY r.created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportView{}
	for rows.Next() {
		var v ReportView
		var ev []byte
		if err := rows.Scan(&v.ID, &v.ReporterRef, &v.ReportedRef, &v.Reason, &v.Details, &ev, &v.Status, &v.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(ev, &v.Evidence)
		out = append(out, v)
	}
	return out, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RemovePost deletes a post (moment, universe or missed connection). The reason
// is mandatory and the action is audited against the author.
func (s *Service) RemovePost(ctx context.Context, a Actor, postID, reason string) error {
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 5 || n > 300 {
		return users.ValidationError{Msg: "a reason of 5-300 characters is required"}
	}
	if _, err := uuid.Parse(postID); err != nil {
		return ErrNotFound
	}
	var author, kind string
	if err := s.db.QueryRow(ctx, `DELETE FROM posts WHERE id=$1::uuid RETURNING author_id::text, kind`, postID).Scan(&author, &kind); err != nil {
		return ErrNotFound
	}
	return s.audit(ctx, a, ActRemovePost, author, "", map[string]any{"reason": reason, "kind": kind})
}

// RemoveVideo deletes a short or reel. The reason is mandatory and the action
// is audited against the author.
func (s *Service) RemoveVideo(ctx context.Context, a Actor, videoID, reason string) error {
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 5 || n > 300 {
		return users.ValidationError{Msg: "a reason of 5-300 characters is required"}
	}
	if s.videos == nil {
		return ErrNotFound
	}
	author, kind, err := s.videos.StaffRemove(ctx, videoID)
	if err != nil {
		return ErrNotFound
	}
	return s.audit(ctx, a, ActRemoveVideo, author, "", map[string]any{"reason": reason, "kind": kind})
}

// RemoveForumPost deletes a forum thread or reply ("thread" or "reply"). The
// reason is mandatory and the action is audited against the author.
func (s *Service) RemoveForumPost(ctx context.Context, a Actor, kind, id, reason string) error {
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 5 || n > 300 {
		return users.ValidationError{Msg: "a reason of 5-300 characters is required"}
	}
	if s.forums == nil {
		return ErrNotFound
	}
	author, err := s.forums.StaffRemove(ctx, kind, id)
	if err != nil {
		return ErrNotFound
	}
	return s.audit(ctx, a, ActRemoveForum, author, "", map[string]any{"reason": reason, "kind": kind})
}

// QueueItem is one thing waiting for a moderator: an open report or a held
// video. Items are listed oldest first so nothing starves.
type QueueItem struct {
	Type      string    `json:"type"` // report | video
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	TargetRef string    `json:"target_ref"`
	Summary   string    `json:"summary"`
	Source    string    `json:"source"` // user_report | automatic | scanner
	Seconds   int       `json:"seconds,omitempty"`
	Kind      string    `json:"kind,omitempty"`
}

// Queue merges open reports and held videos into one list.
func (s *Service) Queue(ctx context.Context) ([]QueueItem, error) {
	reports, err := s.List(ctx, "open", 100, 0)
	if err != nil {
		return nil, err
	}
	out := make([]QueueItem, 0, len(reports))
	for _, r := range reports {
		src := "user_report"
		if r.Reason == "auto_review" {
			src = "automatic"
		}
		sum := r.Reason
		if r.Details != "" {
			sum += ": " + r.Details
		}
		out = append(out, QueueItem{Type: "report", ID: r.ID, CreatedAt: r.CreatedAt, TargetRef: r.ReportedRef, Summary: sum, Source: src})
	}
	if s.videos != nil {
		held, err := s.videos.Held(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range held {
			sum := "video held for review"
			if v.Note != "" {
				sum += ": " + v.Note
			}
			out = append(out, QueueItem{Type: "video", ID: v.ID, CreatedAt: v.CreatedAt, TargetRef: v.OwnerRef, Summary: sum, Source: "scanner", Seconds: v.Seconds, Kind: v.Kind})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// ApproveVideo publishes a held clip after review; audited against the author.
func (s *Service) ApproveVideo(ctx context.Context, a Actor, videoID string) error {
	if s.videos == nil {
		return ErrNotFound
	}
	author, err := s.videos.Approve(ctx, videoID, a.ID)
	if err != nil {
		return ErrNotFound
	}
	return s.audit(ctx, a, ActApproveVideo, author, "", map[string]any{"video": videoID})
}

// VideoTicket lets a moderator watch any clip. Watching personal content is
// itself audited, and the audit entry is written before the link is returned.
func (s *Service) VideoTicket(ctx context.Context, a Actor, videoID string) (string, error) {
	if s.videos == nil {
		return "", ErrNotFound
	}
	path, author, err := s.videos.StaffTicket(ctx, videoID)
	if err != nil {
		return "", ErrNotFound
	}
	if err := s.audit(ctx, a, ActViewVideo, author, "", map[string]any{"video": videoID}); err != nil {
		return "", err
	}
	return path, nil
}

// RevokeSessions signs a user out everywhere and voids their access tokens at
// once (admin only). Use it after changing someone's role directly in the
// database, or when an account is suspected stolen but should not be banned:
// the role inside an access token is trusted until the token expires, so a
// demotion is not effective until the sessions are revoked.
func (s *Service) RevokeSessions(ctx context.Context, a Actor, ref, reason string) error {
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 5 || n > 300 {
		return users.ValidationError{Msg: "a reason of 5-300 characters is required"}
	}
	id, err := s.users.ResolveRef(ctx, ref)
	if err != nil {
		return ErrNotFound
	}
	if err := s.audit(ctx, a, ActRevokeSessions, id, "", map[string]any{"reason": reason}); err != nil {
		return err
	}
	return s.revoker.RevokeAllForUser(ctx, id)
}
