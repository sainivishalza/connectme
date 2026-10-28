package social

import (
	"context"
	"crypto/sha256"
	"time"

	"connectme/internal/push"
	"connectme/internal/users"

	"github.com/google/uuid"
)

const (
	maxCommentsPerPost = 300
	commentsPerDay     = 100
)

// Comment is one reply under a post as the viewer may see it.
//
// Who a commenter appears as follows the post: under an anonymous (universe)
// post everyone, the author included, is a made-up nickname that is stable
// within that post and different on every other post. Under a moment or
// community post a commenter is named only if their profile is shown in full;
// otherwise they are "Someone". No photos, ever.
type Comment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	Name      string    `json:"name"`
	Ref       string    `json:"ref,omitempty"`    // public ref, only for commenters shown in full
	Author    bool      `json:"author,omitempty"` // wrote the post
	Mine      bool      `json:"mine"`
	CreatedAt time.Time `json:"created_at"`
}

// aliasFor is a nickname that is the same for one person under one post and
// unlinkable across posts.
func aliasFor(postID, userID string) string {
	h := sha256.Sum256([]byte(postID + "|" + userID))
	return adjectives[int(h[0])%len(adjectives)] + " " + nouns[int(h[1])%len(nouns)]
}

// postFacts loads what comments need to know about a post the viewer can see.
func (s *Service) postFacts(ctx context.Context, viewerID, postID string) (kind, authorID, alias string, err error) {
	if _, perr := uuid.Parse(postID); perr != nil {
		return "", "", "", ErrNotFound
	}
	if err := s.canSee(ctx, viewerID, postID); err != nil {
		return "", "", "", err
	}
	err = s.db.QueryRow(ctx, `SELECT kind, author_id::text, COALESCE(alias,'') FROM posts WHERE id=$1::uuid`, postID).
		Scan(&kind, &authorID, &alias)
	return kind, authorID, alias, err
}

// Comment adds a comment to a post the caller can see.
func (s *Service) Comment(ctx context.Context, userID, postID, body string) (*Comment, error) {
	kind, authorID, alias, err := s.postFacts(ctx, userID, postID)
	if err != nil {
		return nil, err
	}
	if kind == KindMissed {
		return nil, users.ValidationError{Msg: "comments are not available on missed connections"}
	}
	body, err = cleanBody(body)
	if err != nil {
		return nil, err
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, userID).Scan(&ok); err != nil || !ok {
		return nil, ErrForbidden
	}
	var onPost, today int
	if err := s.db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM post_comments WHERE post_id=$1::uuid),
		       (SELECT COUNT(*) FROM post_comments WHERE author_id=$2::uuid AND created_at > NOW() - INTERVAL '24 hours')`,
		postID, userID).Scan(&onPost, &today); err != nil {
		return nil, err
	}
	if onPost >= maxCommentsPerPost || today >= commentsPerDay {
		return nil, ErrLimit
	}
	if s.vetter != nil {
		if err := s.vetter.VetText(ctx, userID, "comment", body); err != nil {
			return nil, err
		}
	}
	c := Comment{Body: body, Mine: true, Author: userID == authorID}
	if err := s.db.QueryRow(ctx, `INSERT INTO post_comments (post_id, author_id, body) VALUES ($1::uuid,$2::uuid,$3) RETURNING id::text, created_at`,
		postID, userID, body).Scan(&c.ID, &c.CreatedAt); err != nil {
		return nil, err
	}
	names, err := s.commenterNames(ctx, kind, postID, []string{userID})
	if err != nil {
		return nil, err
	}
	c.Name, c.Ref = names[userID].name, names[userID].ref
	if s.notifier != nil && authorID != userID {
		s.notifier.Notify(ctx, authorID, push.KindComment, postID)
	}
	if kind == KindUniverse && c.Author {
		c.Name = alias
	}
	return &c, nil
}

type who struct{ name, ref string }

// commenterNames resolves how each commenter appears under a post of this kind.
func (s *Service) commenterNames(ctx context.Context, kind, postID string, ids []string) (map[string]who, error) {
	out := map[string]who{}
	if kind == KindUniverse {
		for _, id := range ids {
			out[id] = who{name: aliasFor(postID, id)}
		}
		return out, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT user_id::text, COALESCE(display_name,''), public_ref::text
		  FROM profiles WHERE user_id = ANY($1::uuid[]) AND mode = 'open' AND NOT invisible`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var w who
		if err := rows.Scan(&id, &w.name, &w.ref); err != nil {
			return nil, err
		}
		out[id] = w
	}
	for _, id := range ids {
		if w, ok := out[id]; !ok || w.name == "" {
			out[id] = who{name: "Someone"}
		}
	}
	return out, rows.Err()
}

// Comments lists a post's comments oldest first, for a viewer who can see the
// post. People blocked either way with the viewer are left out.
func (s *Service) Comments(ctx context.Context, viewerID, postID string) ([]Comment, error) {
	kind, authorID, alias, err := s.postFacts(ctx, viewerID, postID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT c.id::text, c.body, c.author_id::text, c.created_at
		  FROM post_comments c JOIN users u ON u.id = c.author_id AND NOT u.is_banned
		 WHERE c.post_id = $1::uuid AND (c.author_id = $2::uuid OR `+users.NotBlockedSQL("c.author_id", "$2::uuid")+`)
		 ORDER BY c.created_at, c.id LIMIT 200`, postID, viewerID)
	if err != nil {
		return nil, err
	}
	type rec struct {
		c      Comment
		author string
	}
	var recs []rec
	var ids []string
	seen := map[string]bool{}
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.c.ID, &r.c.Body, &r.author, &r.c.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		recs = append(recs, r)
		if !seen[r.author] {
			seen[r.author] = true
			ids = append(ids, r.author)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	names, err := s.commenterNames(ctx, kind, postID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Comment, 0, len(recs))
	for _, r := range recs {
		c := r.c
		c.Name, c.Ref = names[r.author].name, names[r.author].ref
		c.Mine, c.Author = r.author == viewerID, r.author == authorID
		if kind == KindUniverse && c.Author {
			c.Name = alias
		}
		out = append(out, c)
	}
	return out, nil
}

// DeleteComment removes a comment the caller wrote, or any comment on the
// caller's own post.
func (s *Service) DeleteComment(ctx context.Context, userID, commentID string) error {
	if _, err := uuid.Parse(commentID); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `
		DELETE FROM post_comments c USING posts p
		 WHERE c.id = $1::uuid AND p.id = c.post_id AND (c.author_id = $2::uuid OR p.author_id = $2::uuid)`, commentID, userID)
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
	var postID string
	if err := s.db.QueryRow(ctx, `SELECT post_id::text FROM post_comments WHERE id=$1::uuid`, commentID).Scan(&postID); err != nil {
		return ErrNotFound
	}
	if err := s.canSee(ctx, userID, postID); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason, details, evidence)
		SELECT $1::uuid, c.author_id, $3, 'post comment', jsonb_build_array(c.body)
		  FROM post_comments c WHERE c.id = $2::uuid AND c.author_id <> $1::uuid`, userID, commentID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
