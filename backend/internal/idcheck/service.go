// Package idcheck is a human-reviewed ID check: the member photographs an ID
// document and a selfie holding a random code, a moderator compares them and
// decides, and the images are deleted as soon as the decision is made.
//
// What is and is not guaranteed: a reviewer looking at two photos is a real
// check against casual impersonation and against minors using a parent's
// details, but it is not forensic document verification and does not scale
// like a paid provider. The images never reach a public URL, are scanned for
// known abuse material before they are stored, are only readable by staff
// (every view is audited), and are removed on decision, on expiry and on
// account erasure.
package idcheck

import (
	"connectme/internal/csam"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
)

const (
	codeTTL        = 30 * time.Minute
	pendingTTL     = 7 * 24 * time.Hour
	maxRejections  = 3 // per 24 hours
	ActViewIDCheck = "view_id_check"
	ActIDDecision  = "id_check_decision"
)

// Scanner is the known-abuse-material check; it runs before anything is stored.
type Scanner = csam.Scanner

type Store interface {
	Put(ctx context.Context, id string, data []byte) error
	Get(ctx context.Context, id string) ([]byte, error)
	Delete(ctx context.Context, id string) error
}

type Service struct {
	db      *pgxpool.Pool
	store   Store
	scanner Scanner
	revoker csam.SessionRevoker
	now     func() time.Time
}

func NewService(db *pgxpool.Pool, st Store, sc Scanner) *Service {
	return &Service{db: db, store: st, scanner: sc, now: time.Now}
}

type Actor struct{ ID, Role string }

// Status is what a member sees about their own check.
type Status struct {
	State        string     `json:"state"` // none | awaiting | pending | verified | rejected
	Code         string     `json:"code,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	RejectReason string     `json:"reject_reason,omitempty"`
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04d", n.Int64()), nil
}

// StatusOf reports where the member stands.
func (s *Service) StatusOf(ctx context.Context, userID string) (*Status, error) {
	var verified bool
	if err := s.db.QueryRow(ctx, `SELECT id_verified_at IS NOT NULL FROM users WHERE id=$1::uuid`, userID).Scan(&verified); err != nil {
		return nil, err
	}
	if verified {
		return &Status{State: "verified"}, nil
	}
	var st, code string
	var exp time.Time
	var reason *string
	var reviewed *time.Time
	err := s.db.QueryRow(ctx, `
		SELECT status, code, expires_at, reject_reason, reviewed_at FROM id_checks
		 WHERE user_id=$1::uuid AND status IN ('awaiting','pending','rejected') ORDER BY created_at DESC LIMIT 1`, userID).
		Scan(&st, &code, &exp, &reason, &reviewed)
	if errors.Is(err, pgx.ErrNoRows) {
		return &Status{State: "none"}, nil
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	switch st {
	case "awaiting":
		if now.After(exp) {
			return &Status{State: "none"}, nil
		}
		return &Status{State: "awaiting", Code: code, ExpiresAt: &exp}, nil
	case "pending":
		return &Status{State: "pending"}, nil
	}
	if reviewed != nil && now.Sub(*reviewed) < 7*24*time.Hour && reason != nil {
		return &Status{State: "rejected", RejectReason: *reason}, nil
	}
	return &Status{State: "none"}, nil
}

// Start opens a check and returns the code to write on the paper.
func (s *Service) Start(ctx context.Context, userID string) (*Status, error) {
	var emailOK, verified bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified, id_verified_at IS NOT NULL FROM users WHERE id=$1::uuid AND NOT is_banned`, userID).Scan(&emailOK, &verified); err != nil {
		return nil, ErrNotFound
	}
	switch {
	case !emailOK:
		return nil, users.ValidationError{Msg: "verify your email first"}
	case verified:
		return nil, users.ValidationError{Msg: "you are already ID verified"}
	}
	var recent int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM id_checks WHERE user_id=$1::uuid AND status='rejected' AND reviewed_at > NOW() - INTERVAL '24 hours'`, userID).Scan(&recent); err != nil {
		return nil, err
	}
	if recent >= maxRejections {
		return nil, users.ValidationError{Msg: "too many checks were rejected today; try again tomorrow"}
	}
	if st, err := s.StatusOf(ctx, userID); err == nil {
		switch st.State {
		case "pending":
			return nil, users.ValidationError{Msg: "your check is already being reviewed"}
		case "awaiting":
			return st, nil
		}
	}
	// An expired code is replaced; the unique index keeps one open check per member.
	if _, err := s.db.Exec(ctx, `UPDATE id_checks SET status='expired' WHERE user_id=$1::uuid AND status='awaiting' AND expires_at < NOW()`, userID); err != nil {
		return nil, err
	}
	code, err := newCode()
	if err != nil {
		return nil, err
	}
	exp := s.now().Add(codeTTL)
	if _, err := s.db.Exec(ctx, `INSERT INTO id_checks (user_id, status, code, expires_at) VALUES ($1::uuid,'awaiting',$2,$3)`, userID, code, exp); err != nil {
		return s.StatusOf(ctx, userID) // lost a race with a second tap: show the open one
	}
	return &Status{State: "awaiting", Code: code, ExpiresAt: &exp}, nil
}

// Submit attaches the two photos to the member's open check.
func (s *Service) Submit(ctx context.Context, userID string, idPhoto, selfie []byte) error {
	var checkID string
	err := s.db.QueryRow(ctx, `SELECT id::text FROM id_checks WHERE user_id=$1::uuid AND status='awaiting' AND expires_at > NOW()`, userID).Scan(&checkID)
	if errors.Is(err, pgx.ErrNoRows) {
		return users.ValidationError{Msg: "start a check first; your code may have expired"}
	}
	if err != nil {
		return err
	}
	for _, img := range [][]byte{idPhoto, selfie} {
		meta := csam.ScanMeta{UserID: userID, Source: "id_check"}
		verdict, err := s.scanner.Scan(ctx, img, meta)
		if err != nil {
			slog.Warn("idcheck: scanner unavailable, upload refused", "check", checkID, "err", err)
			return users.ErrModerationUnavailable
		}
		if verdict.Match {
			slog.Error("idcheck: known-abuse match, upload blocked", "check", checkID)
			if err := csam.RecordHit(ctx, s.db, s.revoker, img, verdict, meta); err != nil {
				slog.Error("csam: could not record a match", "err", err)
			}
			return users.ValidationError{Msg: "upload rejected"}
		}
	}
	idKey, selfieKey := uuid.NewString(), uuid.NewString()
	if err := s.store.Put(ctx, idKey, idPhoto); err != nil {
		return err
	}
	if err := s.store.Put(ctx, selfieKey, selfie); err != nil {
		_ = s.store.Delete(ctx, idKey)
		return err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE id_checks SET status='pending', id_key=$2, selfie_key=$3, submitted_at=NOW()
		 WHERE id=$1::uuid AND status='awaiting' AND expires_at > NOW()`, checkID, idKey, selfieKey)
	if err != nil || tag.RowsAffected() == 0 {
		_ = s.store.Delete(ctx, idKey)
		_ = s.store.Delete(ctx, selfieKey)
		if err != nil {
			return err
		}
		return users.ValidationError{Msg: "start a check first; your code may have expired"}
	}
	return nil
}

// QueueItem is what a reviewer sees. The declared birth date is there so the
// reviewer can compare it with the document and confirm the member is 18+.
type QueueItem struct {
	ID          string    `json:"id"`
	SubmittedAt time.Time `json:"submitted_at"`
	Code        string    `json:"code"`
	DisplayName string    `json:"display_name"`
	DeclaredDOB string    `json:"declared_birth_date"`
}

func (s *Service) writeAudit(ctx context.Context, tx pgx.Tx, a Actor, action, targetUser string, detail map[string]any) error {
	d, _ := json.Marshal(detail)
	_, err := tx.Exec(ctx, `
		INSERT INTO staff_audit_log (actor_id, actor_role, action, target_user_id, detail)
		VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid, $5)`, a.ID, a.Role, action, targetUser, d)
	return err
}

// Queue lists submitted checks, oldest first.
func (s *Service) Queue(ctx context.Context) ([]QueueItem, error) {
	rows, err := s.db.Query(ctx, `
		SELECT c.id::text, c.submitted_at, c.code, COALESCE(p.display_name,''), to_char(u.date_of_birth,'YYYY-MM-DD')
		  FROM id_checks c JOIN users u ON u.id = c.user_id LEFT JOIN profiles p ON p.user_id = c.user_id
		 WHERE c.status='pending' ORDER BY c.submitted_at LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueItem{}
	for rows.Next() {
		var q QueueItem
		if err := rows.Scan(&q.ID, &q.SubmittedAt, &q.Code, &q.DisplayName, &q.DeclaredDOB); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// Image returns one of the two photos of a pending check. The view is written
// to the audit log first: if that fails the image is not shown.
func (s *Service) Image(ctx context.Context, a Actor, checkID, which string) ([]byte, error) {
	if _, err := uuid.Parse(checkID); err != nil || (which != "id_photo" && which != "selfie") {
		return nil, ErrNotFound
	}
	var userID string
	var idKey, selfieKey *string
	err := s.db.QueryRow(ctx, `SELECT user_id::text, id_key, selfie_key FROM id_checks WHERE id=$1::uuid AND status='pending'`, checkID).Scan(&userID, &idKey, &selfieKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if userID == a.ID {
		return nil, ErrForbidden // nobody reviews their own document
	}
	key := idKey
	if which == "selfie" {
		key = selfieKey
	}
	if key == nil {
		return nil, ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := s.writeAudit(ctx, tx, a, ActViewIDCheck, userID, map[string]any{"check": checkID, "image": which}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.store.Get(ctx, *key)
}

var rejectReasons = map[string]bool{"code_missing": true, "face_mismatch": true, "unreadable": true, "underage": true, "bad_document": true, "other": true}

// Decide approves or rejects a pending check and deletes its images.
func (s *Service) Decide(ctx context.Context, a Actor, checkID, action, reason string) error {
	if _, err := uuid.Parse(checkID); err != nil {
		return ErrNotFound
	}
	switch action {
	case "approve":
		reason = ""
	case "reject":
		if !rejectReasons[reason] {
			return users.ValidationError{Msg: "choose a reason: code_missing, face_mismatch, unreadable, underage, bad_document or other"}
		}
	default:
		return users.ValidationError{Msg: "action must be approve or reject"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var userID string
	var idKey, selfieKey *string
	err = tx.QueryRow(ctx, `SELECT user_id::text, id_key, selfie_key FROM id_checks WHERE id=$1::uuid AND status='pending' FOR UPDATE`, checkID).Scan(&userID, &idKey, &selfieKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if userID == a.ID {
		return ErrForbidden
	}
	status := map[string]string{"approve": "approved", "reject": "rejected"}[action]
	if _, err := tx.Exec(ctx, `
		UPDATE id_checks SET status=$2, reviewed_by=$3::uuid, reviewed_at=NOW(), reject_reason=NULLIF($4,'') WHERE id=$1::uuid`,
		checkID, status, a.ID, reason); err != nil {
		return err
	}
	if action == "approve" {
		if _, err := tx.Exec(ctx, `UPDATE users SET id_verified_at=NOW() WHERE id=$1::uuid`, userID); err != nil {
			return err
		}
	}
	if err := s.writeAudit(ctx, tx, a, ActIDDecision, userID, map[string]any{"check": checkID, "action": action, "reason": reason}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.deleteFiles(ctx, checkID, idKey, selfieKey)
	return nil
}

// deleteFiles removes the images and clears the keys; a failure leaves the
// keys in place so the sweeper tries again.
func (s *Service) deleteFiles(ctx context.Context, checkID string, keys ...*string) {
	ok := true
	for _, k := range keys {
		if k != nil && *k != "" {
			if err := s.store.Delete(ctx, *k); err != nil {
				slog.Error("idcheck: could not delete an image, will retry", "check", checkID, "err", err)
				ok = false
			}
		}
	}
	if ok {
		_, _ = s.db.Exec(ctx, `UPDATE id_checks SET id_key=NULL, selfie_key=NULL WHERE id=$1::uuid`, checkID)
	}
}

// Sweep expires stale checks and deletes any image that outlived its check.
func (s *Service) Sweep(ctx context.Context) error {
	if _, err := s.db.Exec(ctx, `UPDATE id_checks SET status='expired' WHERE status='awaiting' AND expires_at < NOW()`); err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, `UPDATE id_checks SET status='expired' WHERE status='pending' AND submitted_at < NOW() - make_interval(secs => $1)`, pendingTTL.Seconds()); err != nil {
		return err
	}
	rows, err := s.db.Query(ctx, `SELECT id::text, id_key, selfie_key FROM id_checks WHERE status IN ('approved','rejected','expired') AND (id_key IS NOT NULL OR selfie_key IS NOT NULL) LIMIT 200`)
	if err != nil {
		return err
	}
	type leftover struct {
		id        string
		idk, selk *string
	}
	var left []leftover
	for rows.Next() {
		var l leftover
		if err := rows.Scan(&l.id, &l.idk, &l.selk); err != nil {
			rows.Close()
			return err
		}
		left = append(left, l)
	}
	rows.Close()
	for _, l := range left {
		s.deleteFiles(ctx, l.id, l.idk, l.selk)
	}
	return nil
}

// StartJob runs Sweep periodically until ctx ends.
func (s *Service) StartJob(ctx context.Context, every time.Duration) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := s.Sweep(ctx); err != nil {
					slog.Error("idcheck: sweep failed", "err", err)
				}
			}
		}
	}()
}

// PurgeFiles deletes every stored image of a member who is erasing their
// account (privacy.FilePurger).
func (s *Service) PurgeFiles(ctx context.Context, userID string) error {
	rows, err := s.db.Query(ctx, `SELECT id::text, id_key, selfie_key FROM id_checks WHERE user_id=$1::uuid AND (id_key IS NOT NULL OR selfie_key IS NOT NULL)`, userID)
	if err != nil {
		return err
	}
	type leftover struct {
		id        string
		idk, selk *string
	}
	var left []leftover
	for rows.Next() {
		var l leftover
		if err := rows.Scan(&l.id, &l.idk, &l.selk); err != nil {
			rows.Close()
			return err
		}
		left = append(left, l)
	}
	rows.Close()
	for _, l := range left {
		for _, k := range []*string{l.idk, l.selk} {
			if k != nil && *k != "" {
				if err := s.store.Delete(ctx, *k); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// SetSessionRevoker lets a scanner match end the member's sessions.
func (s *Service) SetSessionRevoker(r csam.SessionRevoker) { s.revoker = r }
