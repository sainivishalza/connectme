package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"connectme/internal/users"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// dummyHash makes a wrong email cost the same time as a wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not a real password"), bcrypt.DefaultCost)

// ReportedRoom is every open report about one masked room, newest line of
// evidence first. The room itself may already be gone.
type ReportedRoom struct {
	RoomID    string    `json:"room_id"`
	Reports   int       `json:"reports"`
	Reporters int       `json:"reporters"`
	Reasons   []string  `json:"reasons"`
	LastAt    time.Time `json:"last_at"`
	Evidence  []string  `json:"evidence"`
	ReportIDs []string  `json:"report_ids"`
}

// ReportedRooms lists masked rooms with open reports, most reported first.
func (s *Service) ReportedRooms(ctx context.Context) ([]ReportedRoom, error) {
	rows, err := s.db.Query(ctx, `
		SELECT ref_id::text, COUNT(*)::int, COUNT(DISTINCT reporter_id)::int,
		       array_agg(DISTINCT reason), MAX(created_at),
		       (array_agg(evidence ORDER BY created_at DESC))[1]::text,
		       array_agg(id::text ORDER BY created_at)
		  FROM reports
		 WHERE ref_kind = 'masked_room' AND status IN ('open','reviewing')
		 GROUP BY ref_id ORDER BY COUNT(*) DESC, MAX(created_at) DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportedRoom{}
	for rows.Next() {
		var r ReportedRoom
		var ev string
		if err := rows.Scan(&r.RoomID, &r.Reports, &r.Reporters, &r.Reasons, &r.LastAt, &ev, &r.ReportIDs); err != nil {
			return nil, err
		}
		r.Evidence = parseLines(ev)
		out = append(out, r)
	}
	return out, rows.Err()
}

// parseLines reads a JSON array of strings, tolerating anything else as empty.
func parseLines(raw string) []string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "[") {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	return out
}

// Appeal is one request to lift a ban, as staff see it.
type Appeal struct {
	ID        string    `json:"id"`
	UserRef   string    `json:"user_ref"`
	Name      string    `json:"display_name"`
	Message   string    `json:"message"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	// What the ban was about: the open and closed reports against them.
	ReportsAbout int `json:"reports_about"`
}

// ErrAppealRefused is returned for every way an appeal can be wrong (unknown
// email, wrong password, not banned) so the form cannot be used to find out
// which emails have accounts.
var ErrAppealRefused = errors.New("could not send the appeal")

// SubmitAppeal records an appeal from a banned member who proves they own the
// account. The answer never says which part was wrong.
func (s *Service) SubmitAppeal(ctx context.Context, email, password, message string) error {
	message = strings.TrimSpace(message)
	if n := len([]rune(message)); n < 10 || n > 1000 {
		return users.ValidationError{Msg: "tell us in 10 to 1000 characters why the ban should be lifted"}
	}
	email = strings.ToLower(strings.TrimSpace(email))
	var id, hash string
	var banned bool
	err := s.db.QueryRow(ctx, `SELECT id::text, password_hash, is_banned FROM users WHERE LOWER(email)=$1`, email).Scan(&id, &hash, &banned)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return ErrAppealRefused
	}
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || !banned {
		return ErrAppealRefused
	}
	_, err = s.db.Exec(ctx, `INSERT INTO appeals (user_id, message) VALUES ($1::uuid, $2) ON CONFLICT DO NOTHING`, id, message)
	return err
}

// Appeals lists appeals by status (default open), oldest first.
func (s *Service) Appeals(ctx context.Context, status string) ([]Appeal, error) {
	if status != "upheld" && status != "lifted" {
		status = "open"
	}
	rows, err := s.db.Query(ctx, `
		SELECT a.id::text, p.public_ref::text, COALESCE(p.display_name,''), a.message, a.status, a.created_at,
		       (SELECT COUNT(*) FROM reports r WHERE r.reported_id = a.user_id)::int
		  FROM appeals a JOIN profiles p ON p.user_id = a.user_id
		 WHERE a.status = $1 ORDER BY a.created_at LIMIT 100`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Appeal{}
	for rows.Next() {
		var a Appeal
		if err := rows.Scan(&a.ID, &a.UserRef, &a.Name, &a.Message, &a.Status, &a.CreatedAt, &a.ReportsAbout); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DecideAppeal lifts the ban (the same staff rules and audit entry as any
// unban) or upholds it, and records the reason the member is told.
func (s *Service) DecideAppeal(ctx context.Context, a Actor, id, decision, note string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	note = strings.TrimSpace(note)
	if n := len([]rune(note)); n < 5 || n > 300 {
		return users.ValidationError{Msg: "a reason of 5-300 characters is required"}
	}
	status := map[string]string{"lift": "lifted", "uphold": "upheld"}[decision]
	if status == "" {
		return users.ValidationError{Msg: "decision must be lift or uphold"}
	}
	var ref, userID string
	if err := s.db.QueryRow(ctx, `
		SELECT p.public_ref::text, a.user_id::text FROM appeals a JOIN profiles p ON p.user_id = a.user_id
		 WHERE a.id=$1::uuid AND a.status='open'`, id).Scan(&ref, &userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status == "lifted" {
		if err := s.SetBan(ctx, a, ref, false, "appeal: "+note); err != nil {
			return err
		}
	} else if err := s.audit(ctx, a, "appeal_upheld", userID, "", map[string]any{"appeal": id}); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE appeals SET status=$2, decision_note=$3, decided_at=NOW(), decided_by=$4::uuid WHERE id=$1::uuid AND status='open'`,
		id, status, note, a.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (h *Handler) reportedRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := h.svc.ReportedRooms(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

func (h *Handler) listAppeals(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Appeals(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"appeals": list})
}

func (h *Handler) decideAppeal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.DecideAppeal(r.Context(), actor(r), chi.URLParam(r, "id"), req.Decision, req.Note); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// submitAppeal is public: a banned member cannot sign in.
func (h *Handler) submitAppeal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Message  string `json:"message"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := h.svc.SubmitAppeal(r.Context(), req.Email, req.Password, req.Message)
	switch {
	case err == nil:
		middleware.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "received"})
	case errors.Is(err, ErrAppealRefused):
		middleware.WriteError(w, http.StatusForbidden, "we could not send this appeal: check the email and password, and that the account is banned")
	default:
		fail(w, err)
	}
}
