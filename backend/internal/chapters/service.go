// Package chapters implements chapter-based profiles: a short sequence of
// narrative chapters about a person's life, shown only to people who may see
// their full profile.
package chapters

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

const MaxChapters = 5

type TextVetter interface {
	VetText(ctx context.Context, userID, kind, text string) error
}

type Service struct {
	db     *pgxpool.Pool
	users  *users.Service
	vetter TextVetter
}

func NewService(db *pgxpool.Pool, u *users.Service) *Service {
	return &Service{db: db, users: u, vetter: u}
}

type Chapter struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func clean(s string, min, max int, what string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < min || n > max {
		return "", users.ValidationError{Msg: what + " must be " + strconv.Itoa(min) + "-" + strconv.Itoa(max) + " characters"}
	}
	for _, c := range s {
		if unicode.IsControl(c) && c != '\n' {
			return "", users.ValidationError{Msg: what + " contains invalid characters"}
		}
	}
	return s, nil
}

// Replace stores the caller's chapters in the order given (an empty list
// clears them). The whole set is replaced in one transaction.
func (s *Service) Replace(ctx context.Context, userID string, in []Chapter) ([]Chapter, error) {
	if len(in) > MaxChapters {
		return nil, users.ValidationError{Msg: "a profile can have up to 5 chapters"}
	}
	out := make([]Chapter, 0, len(in))
	for _, c := range in {
		title, err := clean(c.Title, 3, 60, "chapter title")
		if err != nil {
			return nil, err
		}
		body, err := clean(c.Body, 10, 600, "chapter text")
		if err != nil {
			return nil, err
		}
		if s.vetter != nil {
			for _, t := range []string{title, body} {
				if err := s.vetter.VetText(ctx, userID, "bio", t); err != nil {
					return nil, err
				}
			}
		}
		out = append(out, Chapter{title, body})
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `DELETE FROM profile_chapters WHERE user_id=$1::uuid`, userID); err != nil {
		return nil, err
	}
	for i, c := range out {
		if _, err := tx.Exec(ctx, `INSERT INTO profile_chapters (user_id, position, title, body, updated_at) VALUES ($1::uuid,$2,$3,$4,$5)`,
			userID, i+1, c.Title, c.Body, time.Now()); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit(ctx)
}

func (s *Service) list(ctx context.Context, userID string) ([]Chapter, error) {
	rows, err := s.db.Query(ctx, `SELECT title, body FROM profile_chapters WHERE user_id=$1::uuid ORDER BY position`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chapter{}
	for rows.Next() {
		var c Chapter
		if err := rows.Scan(&c.Title, &c.Body); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Mine returns the caller's chapters.
func (s *Service) Mine(ctx context.Context, userID string) ([]Chapter, error) {
	return s.list(ctx, userID)
}

// For returns someone's chapters if the viewer may see their full profile;
// otherwise "not found", so anonymous profiles do not confirm any exist.
func (s *Service) For(ctx context.Context, viewerID, ref string) ([]Chapter, error) {
	if _, err := uuid.Parse(ref); err != nil {
		return nil, ErrNotFound
	}
	var owner string
	if err := s.db.QueryRow(ctx, `SELECT user_id::text FROM profiles WHERE public_ref=$1::uuid`, ref).Scan(&owner); err != nil {
		return nil, ErrNotFound
	}
	ok, err := s.users.SeesFull(ctx, viewerID, owner)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return s.list(ctx, owner)
}
