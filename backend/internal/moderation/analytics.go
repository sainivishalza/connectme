package moderation

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectme/internal/users"
	"connectme/pkg/middleware"
)

// Funnel counts the people who signed up in the period and how far they got.
// Every step is "of those, how many ever did X", so each number is at most
// the one before it. Aggregates only: nothing here identifies a member.
type Funnel struct {
	Days       int `json:"days"`
	Registered int `json:"registered"`
	Verified   int `json:"verified"`
	Onboarded  int `json:"onboarded"`
	WithPhoto  int `json:"with_photo"`
	Swiped     int `json:"swiped"`
	Matched    int `json:"matched"`
}

// DayCount is one day of activity.
type DayCount struct {
	Day      string `json:"day"`
	Signups  int    `json:"signups"`
	Swipes   int    `json:"swipes"`
	Matches  int    `json:"matches"`
	Reports  int    `json:"reports"`
	LiveRoom int    `json:"live_rooms"`
}

// ReasonCount is how many reports were filed for one reason in the period.
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// Analytics is the funnel, a per-day series and the reasons people report,
// all for the same period.
type Analytics struct {
	Funnel  Funnel        `json:"funnel"`
	Daily   []DayCount    `json:"daily"`
	Reasons []ReasonCount `json:"report_reasons"`
}

// Analytics reports how new members move through the app and what happens
// each day. days is clamped to 7..90.
func (s *Service) Analytics(ctx context.Context, days int) (*Analytics, error) {
	if days < 7 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	out := &Analytics{Funnel: Funnel{Days: days}, Daily: []DayCount{}, Reasons: []ReasonCount{}}
	err := s.db.QueryRow(ctx, `
		WITH cohort AS (SELECT u.id, u.is_verified, p.onboarded_at, p.photos
		                  FROM users u JOIN profiles p ON p.user_id = u.id
		                 WHERE u.created_at >= NOW() - make_interval(days => $1))
		SELECT COUNT(*)::int,
		       COUNT(*) FILTER (WHERE is_verified)::int,
		       COUNT(*) FILTER (WHERE is_verified AND onboarded_at IS NOT NULL)::int,
		       COUNT(*) FILTER (WHERE is_verified AND onboarded_at IS NOT NULL AND jsonb_array_length(photos) > 0)::int,
		       COUNT(*) FILTER (WHERE is_verified AND onboarded_at IS NOT NULL AND jsonb_array_length(photos) > 0
		                          AND EXISTS (SELECT 1 FROM swipes s WHERE s.actor_id = cohort.id))::int,
		       COUNT(*) FILTER (WHERE is_verified AND onboarded_at IS NOT NULL AND jsonb_array_length(photos) > 0
		                          AND EXISTS (SELECT 1 FROM swipes s WHERE s.actor_id = cohort.id)
		                          AND EXISTS (SELECT 1 FROM matches m WHERE m.status = 'matched' AND (m.user1_id = cohort.id OR m.user2_id = cohort.id)))::int
		  FROM cohort`, days).
		Scan(&out.Funnel.Registered, &out.Funnel.Verified, &out.Funnel.Onboarded, &out.Funnel.WithPhoto, &out.Funnel.Swiped, &out.Funnel.Matched)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT to_char(d, 'YYYY-MM-DD'),
		       (SELECT COUNT(*) FROM users WHERE created_at >= d AND created_at < d + INTERVAL '1 day')::int,
		       (SELECT COUNT(*) FROM swipes WHERE created_at >= d AND created_at < d + INTERVAL '1 day')::int,
		       (SELECT COUNT(*) FROM matches WHERE status = 'matched' AND created_at >= d AND created_at < d + INTERVAL '1 day')::int,
		       (SELECT COUNT(*) FROM reports WHERE reason <> 'auto_review' AND created_at >= d AND created_at < d + INTERVAL '1 day')::int,
		       (SELECT COUNT(*) FROM live_rooms WHERE created_at >= d AND created_at < d + INTERVAL '1 day')::int
		  FROM generate_series(date_trunc('day', NOW()) - make_interval(days => $1 - 1), date_trunc('day', NOW()), INTERVAL '1 day') d
		 ORDER BY d`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d DayCount
		if err := rows.Scan(&d.Day, &d.Signups, &d.Swipes, &d.Matches, &d.Reports, &d.LiveRoom); err != nil {
			return nil, err
		}
		out.Daily = append(out.Daily, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	rr, err := s.db.Query(ctx, `
		SELECT reason, COUNT(*)::int FROM reports
		 WHERE reason <> 'auto_review' AND created_at >= NOW() - make_interval(days => $1)
		 GROUP BY reason ORDER BY 2 DESC, 1 LIMIT 10`, days)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	for rr.Next() {
		var c ReasonCount
		if err := rr.Scan(&c.Reason, &c.Count); err != nil {
			return nil, err
		}
		out.Reasons = append(out.Reasons, c)
	}
	return out, rr.Err()
}

// MemberHit is one search result: enough to pick the right person, then open
// the normal staff view. No email, no date of birth, no location.
type MemberHit struct {
	Ref         string    `json:"ref"`
	DisplayName string    `json:"display_name"`
	Mode        string    `json:"mode"`
	Banned      bool      `json:"banned"`
	OpenReports int       `json:"open_reports"`
	JoinedAt    time.Time `json:"joined_at"`
}

// SearchMembers finds members by the start of their display name, or by an
// exact profile reference. Every search is written to the staff audit log.
func (s *Service) SearchMembers(ctx context.Context, a Actor, q string) ([]MemberHit, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 || len(q) > 60 {
		return nil, users.ValidationError{Msg: "type at least 2 characters"}
	}
	if err := s.audit(ctx, a, "member_search", "", "", map[string]any{"query": q}); err != nil {
		return nil, err
	}
	// Escape LIKE wildcards so "%" cannot list everyone.
	esc := strings.NewReplacer(`\`, `\`, `%`, `\%`, `_`, `\_`).Replace(q)
	rows, err := s.db.Query(ctx, `
		SELECT p.public_ref::text, COALESCE(p.display_name,''), p.mode, u.is_banned,
		       (SELECT COUNT(*) FROM reports r WHERE r.reported_id = u.id AND r.status IN ('open','reviewing'))::int, u.created_at
		  FROM profiles p JOIN users u ON u.id = p.user_id
		 WHERE p.public_ref::text = $2 OR lower(p.display_name) LIKE lower($1) || '%'
		 ORDER BY u.created_at DESC LIMIT 25`, esc, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MemberHit{}
	for rows.Next() {
		var h MemberHit
		if err := rows.Scan(&h.Ref, &h.DisplayName, &h.Mode, &h.Banned, &h.OpenReports, &h.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (h *Handler) analytics(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days == 0 {
		days = 30
	}
	a, err := h.svc.Analytics(r.Context(), days)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, a)
}

func (h *Handler) searchMembers(w http.ResponseWriter, r *http.Request) {
	hits, err := h.svc.SearchMembers(r.Context(), actor(r), r.URL.Query().Get("q"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"members": hits})
}
