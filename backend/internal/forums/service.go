// Package forums implements topic boards (dating advice, travel, city boards)
// with threads and replies.
//
// Identity rule: an author is shown by display name only while in Open mode;
// anyone else appears under an alias derived from (thread, user, server
// secret), stable within a thread and unlinkable across threads. The mode is
// read at display time, so going Incognito later hides earlier posts too.
package forums

import (
	"context"
	"crypto/sha256"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrLimit      = errors.New("daily limit reached; try again tomorrow")
	ErrNotAllowed = errors.New("verified members only")
)

const (
	threadsPerDay = 10
	repliesPerDay = 60
	pageSize      = 20
	replyPage     = 50
)

type TextVetter interface {
	VetText(ctx context.Context, userID, kind, text string) error
}

type Service struct {
	db     *pgxpool.Pool
	vetter TextVetter
	secret []byte
}

func NewService(db *pgxpool.Pool, v TextVetter, secret []byte) *Service {
	return &Service{db: db, vetter: v, secret: secret}
}

var adjectives = []string{"Quiet", "Golden", "Brave", "Curious", "Gentle", "Sunny", "Midnight", "Velvet", "Wandering", "Cosy", "Bright", "Calm", "Lucky", "Misty", "Amber", "Silver"}
var animals = []string{"Otter", "Heron", "Fox", "Lynx", "Panda", "Finch", "Gecko", "Moth", "Koala", "Swan", "Hare", "Wren", "Seal", "Newt", "Robin", "Ibis"}

func (s *Service) alias(thread, user string) string {
	h := sha256.Sum256(append(append([]byte(thread+"|"+user+"|"), s.secret...), "forum"...))
	return adjectives[int(h[0])%len(adjectives)] + " " + animals[int(h[1])%len(animals)] + " " + strconv.Itoa(10+int(h[2])%90)
}

type Board struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Threads     int    `json:"threads"`
}

type Thread struct {
	ID          string    `json:"id"`
	Board       string    `json:"board"`
	Title       string    `json:"title"`
	Body        string    `json:"body,omitempty"`
	Author      string    `json:"author"`
	Replies     int       `json:"replies"`
	Mine        bool      `json:"mine"`
	CreatedAt   time.Time `json:"created_at"`
	LastReplyAt time.Time `json:"last_reply_at"`
}

type Reply struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	Author    string    `json:"author"`
	Mine      bool      `json:"mine"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) member(ctx context.Context, userID string) error {
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1::uuid`, userID).Scan(&ok); err != nil || !ok {
		return ErrNotAllowed
	}
	return nil
}

func clean(text string, min, max int, what string) (string, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n < min || n > max {
		return "", users.ValidationError{Msg: what + " must be " + strconv.Itoa(min) + "-" + strconv.Itoa(max) + " characters"}
	}
	for _, c := range text {
		if unicode.IsControl(c) && c != '\n' {
			return "", users.ValidationError{Msg: what + " contains invalid characters"}
		}
	}
	return text, nil
}

var slugRe = regexp.MustCompile(`^[a-z0-9-]{2,40}$`)

// Boards lists every board with its thread count.
func (s *Service) Boards(ctx context.Context) ([]Board, error) {
	rows, err := s.db.Query(ctx, `
		SELECT b.slug, b.title, b.description, b.kind, COUNT(t.id)
		  FROM forum_boards b LEFT JOIN forum_threads t ON t.board = b.slug
		 GROUP BY b.slug ORDER BY b.position, b.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Board{}
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.Slug, &b.Title, &b.Description, &b.Kind, &b.Threads); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CreateBoard adds a board (admin only; the handler enforces the permission).
func (s *Service) CreateBoard(ctx context.Context, slug, title, desc, kind string) (*Board, error) {
	title, err := clean(title, 3, 60, "title")
	if err != nil {
		return nil, err
	}
	desc, err = clean(desc, 0, 200, "description")
	if err != nil {
		return nil, err
	}
	if kind != "advice" && kind != "travel" && kind != "city" {
		return nil, users.ValidationError{Msg: "kind must be advice, travel or city"}
	}
	var b Board
	err = s.db.QueryRow(ctx, `
		INSERT INTO forum_boards (slug, title, description, kind, position) VALUES ($1,$2,$3,$4,100)
		ON CONFLICT DO NOTHING RETURNING slug, title, description, kind`, slug, title, desc, kind).Scan(&b.Slug, &b.Title, &b.Description, &b.Kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, users.ValidationError{Msg: "that board already exists"}
	}
	if err != nil {
		return nil, users.ValidationError{Msg: "slug must be 2-40 lowercase letters, digits or hyphens"}
	}
	return &b, nil
}

const threadSelect = `
	SELECT t.id::text, t.board, t.title, t.body, t.replies, t.created_at, t.last_reply_at, t.author_id::text,
	       t.author_id = $1::uuid, p.mode, p.display_name
	  FROM forum_threads t JOIN profiles p ON p.user_id = t.author_id`

func (s *Service) scanThread(row pgx.Row, viewer string, withBody bool) (*Thread, error) {
	var t Thread
	var author, mode, name string
	if err := row.Scan(&t.ID, &t.Board, &t.Title, &t.Body, &t.Replies, &t.CreatedAt, &t.LastReplyAt, &author, &t.Mine, &mode, &name); err != nil {
		return nil, err
	}
	t.Author = s.label(t.ID, author, mode, name)
	if !withBody {
		t.Body = ""
	}
	return &t, nil
}

func (s *Service) label(thread, author, mode, name string) string {
	if mode == "open" {
		return name
	}
	return s.alias(thread, author)
}

// Threads lists a board, most recently active first.
func (s *Service) Threads(ctx context.Context, viewer, board string, before time.Time) ([]Thread, error) {
	if err := s.member(ctx, viewer); err != nil {
		return nil, err
	}
	if !slugRe.MatchString(board) {
		return nil, ErrNotFound
	}
	if before.IsZero() {
		before = time.Now().Add(time.Minute)
	}
	rows, err := s.db.Query(ctx, threadSelect+`
		 WHERE t.board = $2 AND t.last_reply_at < $3
		   AND `+users.NotBlockedSQL("t.author_id", "$1::uuid")+`
		 ORDER BY t.last_reply_at DESC LIMIT $4`, viewer, board, before, pageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Thread{}
	for rows.Next() {
		t, err := s.scanThread(rows, viewer, false)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Service) dailyCount(ctx context.Context, table, userID string) (int, error) {
	var n int
	var q string
	switch table {
	case "threads":
		q = `SELECT COUNT(*) FROM forum_threads WHERE author_id=$1::uuid AND created_at > NOW() - INTERVAL '24 hours'`
	default:
		q = `SELECT COUNT(*) FROM forum_replies WHERE author_id=$1::uuid AND created_at > NOW() - INTERVAL '24 hours'`
	}
	err := s.db.QueryRow(ctx, q, userID).Scan(&n)
	return n, err
}

// CreateThread starts a thread on a board.
func (s *Service) CreateThread(ctx context.Context, userID, board, title, body string) (*Thread, error) {
	if err := s.member(ctx, userID); err != nil {
		return nil, err
	}
	title, err := clean(title, 5, 120, "title")
	if err != nil {
		return nil, err
	}
	body, err = clean(body, 5, 2000, "text")
	if err != nil {
		return nil, err
	}
	if !slugRe.MatchString(board) {
		return nil, ErrNotFound
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM forum_boards WHERE slug=$1)`, board).Scan(&exists); err != nil || !exists {
		return nil, ErrNotFound
	}
	if n, err := s.dailyCount(ctx, "threads", userID); err != nil {
		return nil, err
	} else if n >= threadsPerDay {
		return nil, ErrLimit
	}
	if s.vetter != nil {
		for _, txt := range []string{title, body} {
			if err := s.vetter.VetText(ctx, userID, "post", txt); err != nil {
				return nil, err
			}
		}
	}
	id := uuid.NewString()
	if _, err := s.db.Exec(ctx, `INSERT INTO forum_threads (id, board, author_id, title, body) VALUES ($1::uuid,$2,$3::uuid,$4,$5)`,
		id, board, userID, title, body); err != nil {
		return nil, err
	}
	return s.Thread(ctx, userID, id)
}

// Thread returns one thread with its body.
func (s *Service) Thread(ctx context.Context, viewer, id string) (*Thread, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrNotFound
	}
	if err := s.member(ctx, viewer); err != nil {
		return nil, err
	}
	t, err := s.scanThread(s.db.QueryRow(ctx, threadSelect+`
		 WHERE t.id = $2::uuid AND `+users.NotBlockedSQL("t.author_id", "$1::uuid"), viewer, id), viewer, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// Replies returns replies oldest first, after the given reply id.
func (s *Service) Replies(ctx context.Context, viewer, threadID string, after int64) ([]Reply, error) {
	if _, err := s.Thread(ctx, viewer, threadID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT r.id, r.body, r.created_at, r.author_id::text, r.author_id = $1::uuid, p.mode, p.display_name
		  FROM forum_replies r JOIN profiles p ON p.user_id = r.author_id
		 WHERE r.thread_id = $2::uuid AND r.id > $3
		   AND `+users.NotBlockedSQL("r.author_id", "$1::uuid")+`
		 ORDER BY r.id LIMIT $4`, viewer, threadID, after, replyPage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reply{}
	for rows.Next() {
		var r Reply
		var author, mode, name string
		if err := rows.Scan(&r.ID, &r.Body, &r.CreatedAt, &author, &r.Mine, &mode, &name); err != nil {
			return nil, err
		}
		r.Author = s.label(threadID, author, mode, name)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Reply adds a reply and bumps the thread.
func (s *Service) Reply(ctx context.Context, userID, threadID, body string) (*Reply, error) {
	if _, err := s.Thread(ctx, userID, threadID); err != nil {
		return nil, err
	}
	body, err := clean(body, 2, 1000, "reply")
	if err != nil {
		return nil, err
	}
	if n, err := s.dailyCount(ctx, "replies", userID); err != nil {
		return nil, err
	} else if n >= repliesPerDay {
		return nil, ErrLimit
	}
	if s.vetter != nil {
		if err := s.vetter.VetText(ctx, userID, "post", body); err != nil {
			return nil, err
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id int64
	var created time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO forum_replies (thread_id, author_id, body) VALUES ($1::uuid,$2::uuid,$3) RETURNING id, created_at`,
		threadID, userID, body).Scan(&id, &created); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE forum_threads SET replies = replies + 1, last_reply_at = NOW() WHERE id=$1::uuid`, threadID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &Reply{ID: id, Body: body, Author: "You", Mine: true, CreatedAt: created}, nil
}

// DeleteThread removes the caller's own thread.
func (s *Service) DeleteThread(ctx context.Context, userID, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM forum_threads WHERE id=$1::uuid AND author_id=$2::uuid`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteReply removes the caller's own reply.
func (s *Service) DeleteReply(ctx context.Context, userID string, id int64) error {
	var thread string
	err := s.db.QueryRow(ctx, `DELETE FROM forum_replies WHERE id=$1 AND author_id=$2::uuid RETURNING thread_id::text`, id, userID).Scan(&thread)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `UPDATE forum_threads SET replies = GREATEST(replies - 1, 0) WHERE id=$1::uuid`, thread)
	return err
}

// Report files a report about a thread (kind "thread") or reply ("reply").
func (s *Service) Report(ctx context.Context, userID, kind, id, reason string) error {
	switch reason {
	case "spam", "harassment", "inappropriate_content", "scam", "underage", "other":
	default:
		return users.ValidationError{Msg: "unknown reason"}
	}
	if err := s.member(ctx, userID); err != nil {
		return err
	}
	var tag int64
	switch kind {
	case "thread":
		if _, err := uuid.Parse(id); err != nil {
			return ErrNotFound
		}
		t, err := s.db.Exec(ctx, `
			INSERT INTO reports (reporter_id, reported_id, reason, details, evidence)
			SELECT $1::uuid, t.author_id, $3, 'forum thread', jsonb_build_array(t.title, t.body)
			  FROM forum_threads t WHERE t.id=$2::uuid AND t.author_id <> $1::uuid`, userID, id, reason)
		if err != nil {
			return err
		}
		tag = t.RowsAffected()
	case "reply":
		n, perr := strconv.ParseInt(id, 10, 64)
		if perr != nil {
			return ErrNotFound
		}
		t, err := s.db.Exec(ctx, `
			INSERT INTO reports (reporter_id, reported_id, reason, details, evidence)
			SELECT $1::uuid, r.author_id, $3, 'forum reply', jsonb_build_array(r.body)
			  FROM forum_replies r WHERE r.id=$2 AND r.author_id <> $1::uuid`, userID, n, reason)
		if err != nil {
			return err
		}
		tag = t.RowsAffected()
	default:
		return ErrNotFound
	}
	if tag == 0 {
		return ErrNotFound
	}
	return nil
}

// StaffRemove deletes a thread or reply for moderation; the caller audits it.
func (s *Service) StaffRemove(ctx context.Context, kind, id string) (author string, err error) {
	switch kind {
	case "thread":
		if _, perr := uuid.Parse(id); perr != nil {
			return "", ErrNotFound
		}
		err = s.db.QueryRow(ctx, `DELETE FROM forum_threads WHERE id=$1::uuid RETURNING author_id::text`, id).Scan(&author)
	case "reply":
		n, perr := strconv.ParseInt(id, 10, 64)
		if perr != nil {
			return "", ErrNotFound
		}
		var thread string
		err = s.db.QueryRow(ctx, `DELETE FROM forum_replies WHERE id=$1 RETURNING author_id::text, thread_id::text`, n).Scan(&author, &thread)
		if err == nil {
			_, _ = s.db.Exec(ctx, `UPDATE forum_threads SET replies = GREATEST(replies - 1, 0) WHERE id=$1::uuid`, thread)
		}
	default:
		return "", ErrNotFound
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return author, err
}
