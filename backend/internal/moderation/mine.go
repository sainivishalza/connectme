package moderation

import (
	"context"
	"time"
)

// MyReport is what a reporter may see about a report they filed: the reason,
// when, and whether staff have looked. It never names the reported member.
type MyReport struct {
	ID        string    `json:"id"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	// Outcome is "pending", "action_taken" or "no_action".
	Outcome string `json:"outcome"`
}

// MyReports lists the caller's own reports, newest first.
func (s *Service) MyReports(ctx context.Context, userID string) ([]MyReport, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, reason, created_at,
		       CASE status WHEN 'actioned' THEN 'action_taken' WHEN 'dismissed' THEN 'no_action' ELSE 'pending' END
		  FROM reports WHERE reporter_id=$1::uuid ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MyReport{}
	for rows.Next() {
		var m MyReport
		if err := rows.Scan(&m.ID, &m.Reason, &m.CreatedAt, &m.Outcome); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
