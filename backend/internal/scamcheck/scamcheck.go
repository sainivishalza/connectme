// Package scamcheck gives a member a second opinion on a few chat lines they
// choose to share. Chats are end-to-end encrypted, so the server never sees
// them unless the member sends lines here; they are passed to the AI service
// and neither stored nor logged. Nothing is blocked and nobody else is told.
package scamcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var ErrNotFound = errors.New("not found")
var ErrUnavailable = errors.New("the check is not available right now")

type Line struct {
	Who  string `json:"who"`
	Text string `json:"text"`
}

type Signal struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

type Result struct {
	Risk    string   `json:"risk"`
	Score   int      `json:"score"`
	Signals []Signal `json:"signals"`
	Advice  string   `json:"advice"`
}

// Checker is the AI service.
type Checker interface {
	Check(ctx context.Context, lines []Line) (*Result, error)
}

// HTTPChecker calls the AI service's /scam/check.
type HTTPChecker struct {
	BaseURL, Key string
	Client       *http.Client
}

func NewHTTPChecker(base, key string) *HTTPChecker {
	return &HTTPChecker{BaseURL: strings.TrimRight(base, "/"), Key: key, Client: &http.Client{Timeout: 6 * time.Second}}
}

func (h *HTTPChecker) Check(ctx context.Context, lines []Line) (*Result, error) {
	if h.BaseURL == "" {
		return nil, ErrUnavailable
	}
	body, _ := json.Marshal(map[string]any{"lines": lines})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.BaseURL+"/scam/check", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.Key != "" {
		req.Header.Set("X-Internal-Key", h.Key)
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	var out Result
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&out) != nil {
		return nil, ErrUnavailable
	}
	if out.Signals == nil {
		out.Signals = []Signal{}
	}
	return &out, nil
}

type Service struct {
	db      *pgxpool.Pool
	checker Checker
}

func NewService(db *pgxpool.Pool, c Checker) *Service { return &Service{db: db, checker: c} }

func clean(in []Line) ([]Line, error) {
	if len(in) < 1 || len(in) > 10 {
		return nil, users.ValidationError{Msg: "share between 1 and 10 messages"}
	}
	out := make([]Line, 0, len(in))
	for _, l := range in {
		if l.Who != "me" && l.Who != "other" {
			return nil, users.ValidationError{Msg: "each message needs a sender of me or other"}
		}
		t := strings.Join(strings.Fields(l.Text), " ")
		if n := utf8.RuneCountInString(t); n < 1 || n > 300 {
			return nil, users.ValidationError{Msg: "each message must be 1-300 characters"}
		}
		for _, c := range t {
			if unicode.IsControl(c) {
				return nil, users.ValidationError{Msg: "a message contains invalid characters"}
			}
		}
		out = append(out, Line{Who: l.Who, Text: t})
	}
	return out, nil
}

// Check verifies the caller is in the conversation (a match, or a matched
// group chat) and asks the AI service.
func (s *Service) Check(ctx context.Context, userID, convID string, lines []Line) (*Result, error) {
	ls, err := clean(lines)
	if err != nil {
		return nil, err
	}
	var ok bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM matches m WHERE m.id=$1::uuid AND m.status='matched' AND $2::uuid IN (m.user1_id, m.user2_id))
		    OR EXISTS (SELECT 1 FROM group_matches gm JOIN group_members g ON g.group_id IN (gm.group_a, gm.group_b)
		                WHERE gm.id=$1::uuid AND gm.status='matched' AND g.user_id=$2::uuid)`, convID, userID).Scan(&ok); err != nil || !ok {
		return nil, ErrNotFound
	}
	if s.checker == nil {
		return nil, ErrUnavailable
	}
	return s.checker.Check(ctx, ls)
}

// Routes mounts /scamcheck.
func Routes(svc *Service, rdb *redis.Client, secret []byte) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Auth(secret), middleware.MaxBody(8<<10))
	r.With(middleware.RateLimit(rdb, "scamcheck:check", 30, time.Hour, middleware.ByUser)).Post("/{conv}", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Lines []Line `json:"lines"`
		}
		dec := json.NewDecoder(req.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		res, err := svc.Check(req.Context(), middleware.UserID(req.Context()), chi.URLParam(req, "conv"), body.Lines)
		var ve users.ValidationError
		switch {
		case err == nil:
			middleware.WriteJSON(w, http.StatusOK, res)
		case errors.As(err, &ve):
			middleware.WriteError(w, http.StatusUnprocessableEntity, ve.Msg)
		case errors.Is(err, ErrNotFound):
			middleware.WriteError(w, http.StatusNotFound, "not found")
		case errors.Is(err, ErrUnavailable):
			middleware.WriteError(w, http.StatusServiceUnavailable, err.Error())
		default:
			slog.Error("scamcheck: internal error", "err", err)
			middleware.WriteError(w, http.StatusInternalServerError, "internal error")
		}
	})
	return r
}
