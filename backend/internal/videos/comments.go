package videos

import (
	"context"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/push"
	"connectme/internal/users"
	"connectme/pkg/utils"

	"github.com/google/uuid"
)

const (
	maxCommentsPerVideo = 300
	commentsPerDay      = 100
)

// SetNotifier tells a video's owner when someone comments (nil keeps it off).
func (s *Service) SetNotifier(n push.Notifier) { s.notifier = n }

// Comment is one reply under a short as the viewer may see it.
type Comment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	Name      string    `json:"name"`
	Ref       string    `json:"ref,omitempty"` // only for commenters shown in full
	Author    bool      `json:"author,omitempty"`
	Mine      bool      `json:"mine"`
	CreatedAt time.Time `json:"created_at"`
}

func cleanComment(s string) (string, error) {
	s = utils.StripTags(s)
	if n := utf8.RuneCountInString(s); n < 3 || n > 280 {
		return "", users.ValidationError{Msg: "a comment is 3 to 280 characters"}
	}
	for _, c := range s {
		if unicode.IsControl(c) && c != '\n' {
			return "", users.ValidationError{Msg: "the comment contains invalid characters"}
		}
	}
	return s, nil
}

// Comment adds a comment to a short the caller may watch.
func (s *Service) Comment(ctx context.Context, userID, videoID, body string) (*Comment, error) {
	a, err := s.check(ctx, userID, videoID)
	if err != nil {
		return nil, err
	}
	if a.kind != "short" || a.status != "live" {
		return nil, ErrNotFound
	}
	body, err = cleanComment(body)
	if err != nil {
		return nil, err
	}
	var onVideo, today int
	if err := s.db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM video_comments WHERE video_id=$1::uuid),
		       (SELECT COUNT(*) FROM video_comments WHERE author_id=$2::uuid AND created_at > NOW() - INTERVAL '24 hours')`,
		videoID, userID).Scan(&onVideo, &today); err != nil {
		return nil, err
	}
	if onVideo >= maxCommentsPerVideo || today >= commentsPerDay {
		return nil, users.ValidationError{Msg: "you have reached the comment limit for now"}
	}
	if s.vetter != nil {
		if err := s.vetter.VetText(ctx, userID, "comment", body); err != nil {
			return nil, err
		}
	}
	c := Comment{Body: body, Mine: true, Author: userID == a.owner}
	if err := s.db.QueryRow(ctx, `INSERT INTO video_comments (video_id, author_id, body) VALUES ($1::uuid,$2::uuid,$3) RETURNING id::text, created_at`,
		videoID, userID, body).Scan(&c.ID, &c.CreatedAt); err != nil {
		return nil, err
	}
	c.Name, c.Ref = s.commenter(ctx, userID)
	if s.notifier != nil && a.owner != userID {
		s.notifier.Notify(ctx, a.owner, push.KindComment, videoID)
	}
	return &c, nil
}

// commenter names a person only if their profile is shown in full.
func (s *Service) commenter(ctx context.Context, userID string) (name, ref string) {
	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(display_name,''), public_ref::text FROM profiles
		 WHERE user_id=$1::uuid AND mode='open' AND NOT invisible`, userID).Scan(&name, &ref)
	if name == "" {
		return "Someone", ""
	}
	return name, ref
}

// Comments lists a short's comments oldest first. People blocked either way
// with the viewer are left out.
func (s *Service) Comments(ctx context.Context, viewerID, videoID string) ([]Comment, error) {
	a, err := s.check(ctx, viewerID, videoID)
	if err != nil {
		return nil, err
	}
	if a.kind != "short" {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(ctx, `
		SELECT c.id::text, c.body, c.author_id::text, c.created_at,
		       CASE WHEN p.mode = 'open' AND NOT p.invisible THEN COALESCE(p.display_name,'') ELSE '' END,
		       CASE WHEN p.mode = 'open' AND NOT p.invisible THEN p.public_ref::text ELSE '' END
		  FROM video_comments c
		  JOIN users u ON u.id = c.author_id AND NOT u.is_banned
		  JOIN profiles p ON p.user_id = c.author_id
		 WHERE c.video_id = $1::uuid AND (c.author_id = $2::uuid OR `+users.NotBlockedSQL("c.author_id", "$2::uuid")+`)
		 ORDER BY c.created_at, c.id LIMIT 200`, videoID, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		var author string
		if err := rows.Scan(&c.ID, &c.Body, &author, &c.CreatedAt, &c.Name, &c.Ref); err != nil {
			return nil, err
		}
		if c.Name == "" {
			c.Name, c.Ref = "Someone", ""
		}
		c.Mine, c.Author = author == viewerID, author == a.owner
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteComment removes a comment the caller wrote, or any comment on their own video.
func (s *Service) DeleteComment(ctx context.Context, userID, commentID string) error {
	if _, err := uuid.Parse(commentID); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `
		DELETE FROM video_comments c USING videos v
		 WHERE c.id = $1::uuid AND v.id = c.video_id AND (c.author_id = $2::uuid OR v.owner_id = $2::uuid)`, commentID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReportComment files a report against a comment's author with its text as evidence.
func (s *Service) ReportComment(ctx context.Context, userID, commentID, reason string) error {
	if _, err := uuid.Parse(commentID); err != nil {
		return ErrNotFound
	}
	switch reason {
	case "spam", "harassment", "inappropriate_content", "scam", "underage", "other":
	default:
		return users.ValidationError{Msg: "unknown reason"}
	}
	var videoID string
	if err := s.db.QueryRow(ctx, `SELECT video_id::text FROM video_comments WHERE id=$1::uuid`, commentID).Scan(&videoID); err != nil {
		return ErrNotFound
	}
	if _, err := s.check(ctx, userID, videoID); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason, details, evidence)
		SELECT $1::uuid, c.author_id, $3, 'video comment', jsonb_build_array(c.body)
		  FROM video_comments c WHERE c.id = $2::uuid AND c.author_id <> $1::uuid`, userID, commentID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
