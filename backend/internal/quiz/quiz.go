// Package quiz is quiz-first matching: answer questions, see how well you line
// up with someone before you see a photo. Answers are private; others only
// ever see a rounded percentage, and only once enough questions are shared.
package quiz

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Question struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Options []string `json:"options"`
}

var ErrBad = errors.New("unknown question or option")

// MaxChangesPerDay limits how many existing answers a member may change or
// remove in 24 hours (see migration 064): enough to correct a mistake or
// grow, too few to probe someone else's answers by flipping one at a time.
const MaxChangesPerDay = 6

var ErrTooManyChanges = errors.New("you can change up to 6 answers a day, try again tomorrow")

type Service struct{ db *pgxpool.Pool }

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// Mine returns the caller's answers keyed by question id (option index).
func (s *Service) Mine(ctx context.Context, userID string) (map[string]int, error) {
	rows, err := s.db.Query(ctx, `SELECT qid, choice FROM compat_answers WHERE user_id=$1::uuid`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var q string
		var c int
		if err := rows.Scan(&q, &c); err != nil {
			return nil, err
		}
		out[q] = c
	}
	return out, rows.Err()
}

// Answer records (or changes) answers; an option of -1 removes the answer.
func (s *Service) Answer(ctx context.Context, userID string, answers map[string]int) error {
	if len(answers) == 0 || len(answers) > len(Bank) {
		return ErrBad
	}
	byID := map[string]Question{}
	for _, q := range Bank {
		byID[q.ID] = q
	}
	for id, c := range answers {
		q, ok := byID[id]
		if !ok || c < -1 || c >= len(q.Options) {
			return ErrBad
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Serialise one member's writes so two requests cannot both pass the cap.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "quiz:"+userID); err != nil {
		return err
	}
	changes := 0
	for id, c := range answers {
		var old int
		err = tx.QueryRow(ctx, `SELECT choice FROM compat_answers WHERE user_id=$1::uuid AND qid=$2`, userID, id).Scan(&old)
		existed := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Only altering or removing an answer someone could already be
		// compared on counts; a first answer to a question is free.
		if existed && old != c {
			changes++
		}
		if c == -1 {
			_, err = tx.Exec(ctx, `DELETE FROM compat_answers WHERE user_id=$1::uuid AND qid=$2`, userID, id)
		} else {
			_, err = tx.Exec(ctx, `
				INSERT INTO compat_answers (user_id, qid, choice) VALUES ($1::uuid,$2,$3)
				ON CONFLICT (user_id, qid) DO UPDATE SET choice=$3, answered_at=NOW()`, userID, id, c)
		}
		if err != nil {
			return err
		}
	}
	if changes > 0 {
		var recent int
		if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM compat_changes WHERE user_id=$1::uuid AND changed_at > NOW() - INTERVAL '24 hours'`, userID).Scan(&recent); err != nil {
			return err
		}
		if recent+changes > MaxChangesPerDay {
			return ErrTooManyChanges
		}
		if _, err = tx.Exec(ctx, `INSERT INTO compat_changes (user_id) SELECT $1::uuid FROM generate_series(1, $2::int)`, userID, changes); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type Handler struct{ svc *Service }

// Routes mounts /quiz.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	h := &Handler{svc: svc}
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(4<<10))
	lim := func(name string, n int, w time.Duration) func(http.Handler) http.Handler {
		return middleware.RateLimit(rdb, "quiz:"+name, n, w, middleware.ByUser)
	}
	r.With(lim("read", 120, time.Minute)).Get("/", h.get)
	r.With(lim("write", 120, time.Hour)).Put("/answers", h.put)
	return r
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	mine, err := h.svc.Mine(r.Context(), middleware.UserID(r.Context()))
	if err != nil {
		slog.Error("quiz: read", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"questions": Bank, "answers": mine, "min_shared": users.QuizMinShared})
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Answers map[string]int `json:"answers"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.Answer(r.Context(), middleware.UserID(r.Context()), req.Answers); err != nil {
		if errors.Is(err, ErrBad) {
			middleware.WriteError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if errors.Is(err, ErrTooManyChanges) {
			middleware.WriteError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		slog.Error("quiz: write", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
