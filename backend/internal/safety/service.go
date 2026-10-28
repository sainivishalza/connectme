// Package safety is the member-side safety toolkit: trusted contacts and date
// plans with a check-in. It sends plain emails to people the member named and
// who confirmed they agree; it never contacts anyone else.
package safety

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/push"
	"connectme/internal/users"
	"connectme/pkg/metrics"
	"connectme/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

const (
	maxContacts    = 3
	maxPlansActive = 3
	maxExtensions  = 3
)

// Sender delivers a plain email. Failures are logged by the implementation.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

type Service struct {
	db       *pgxpool.Pool
	users    *users.Service
	mail     Sender
	notifier push.Notifier
	sms      SMS
	appURL   string
	now      func() time.Time
}

// NewService returns the toolkit. appURL is where a contact opens the
// confirmation link (the web app).
func NewService(db *pgxpool.Pool, u *users.Service, m Sender, appURL string) *Service {
	return &Service{db: db, users: u, mail: m, appURL: strings.TrimRight(appURL, "/"), now: time.Now}
}

// SetNotifier lets the service remind a member to check in (nil keeps it off).
func (s *Service) SetNotifier(n push.Notifier) { s.notifier = n }

func hash(token string) []byte { h := sha256.Sum256([]byte(token)); return h[:] }

func cleanLine(s string, max int, what string) (string, error) {
	s = strings.TrimSpace(utils.StripTags(s))
	n := utf8.RuneCountInString(s)
	if n < 1 || n > max {
		return "", users.ValidationError{Msg: fmt.Sprintf("%s must be 1 to %d characters", what, max)}
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return "", users.ValidationError{Msg: what + " contains invalid characters"}
		}
	}
	return s, nil
}

// ---- contacts ----

type Contact struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Confirmed bool      `json:"confirmed"`
	CreatedAt time.Time `json:"created_at"`
	// Phone is shown masked (last two digits); PhoneConfirmed is true once the
	// contact has read the texted code to the member.
	Phone          string `json:"phone,omitempty"`
	PhoneConfirmed bool   `json:"phone_confirmed"`
}

// AddContact records a trusted contact and emails them a confirmation link.
func (s *Service) AddContact(ctx context.Context, userID, name, email string) (*Contact, error) {
	name, err := cleanLine(name, 60, "name")
	if err != nil {
		return nil, err
	}
	addr, err := mail.ParseAddress(strings.TrimSpace(email))
	if err != nil || strings.ContainsAny(email, "\r\n") || len(addr.Address) > 254 {
		return nil, users.ValidationError{Msg: "enter a valid email address"}
	}
	var n int
	var own string
	if err := s.db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM trusted_contacts WHERE user_id=$1::uuid),
		       (SELECT lower(email) FROM users WHERE id=$1::uuid)`, userID).Scan(&n, &own); err != nil {
		return nil, err
	}
	if n >= maxContacts {
		return nil, users.ValidationError{Msg: "you can have up to three trusted contacts"}
	}
	if strings.EqualFold(addr.Address, own) {
		return nil, users.ValidationError{Msg: "choose someone else, not yourself"}
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	var c Contact
	err = s.db.QueryRow(ctx, `
		INSERT INTO trusted_contacts (user_id, name, email, token_hash) VALUES ($1::uuid,$2,lower($3),$4)
		ON CONFLICT (user_id, email) DO UPDATE SET name = EXCLUDED.name, token_hash = EXCLUDED.token_hash
		  WHERE trusted_contacts.confirmed_at IS NULL
		RETURNING id::text, name, email, confirmed_at IS NOT NULL, created_at`, userID, name, addr.Address, hash(token)).
		Scan(&c.ID, &c.Name, &c.Email, &c.Confirmed, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, users.ValidationError{Msg: "that person is already one of your trusted contacts"}
	}
	if err != nil {
		return nil, err
	}
	var who string
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(display_name,'') FROM profiles WHERE user_id=$1::uuid`, userID).Scan(&who)
	if who == "" {
		who = "A ConnectMe member"
	}
	body := fmt.Sprintf("%s would like to name you as someone they trust on ConnectMe.\r\n\r\n"+
		"If you agree, they can send you their plans when they go on a date, and you will be told if they do not check in afterwards. "+
		"You will only ever get messages they choose to send.\r\n\r\nTo agree, open:\r\n%s/safety/confirm?token=%s\r\n\r\n"+
		"If you do not know this person or do not want this, ignore this email and nothing will be sent to you.\r\n", who, s.appURL, token)
	if err := s.mail.Send(ctx, addr.Address, "Will you be a trusted contact on ConnectMe?", body); err != nil {
		slog.Warn("safety: confirmation email failed", "err", err)
	}
	return &c, nil
}

// ConfirmContact is what the contact's link calls. It needs no sign-in: the
// unguessable token is the proof. It returns the member's first name for the
// thank-you message.
func (s *Service) ConfirmContact(ctx context.Context, token string) error {
	if len(token) < 20 || len(token) > 80 {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `UPDATE trusted_contacts SET confirmed_at = COALESCE(confirmed_at, NOW()) WHERE token_hash = $1`, hash(token))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) Contacts(ctx context.Context, userID string) ([]Contact, error) {
	rows, err := s.db.Query(ctx, `SELECT id::text, name, email, confirmed_at IS NOT NULL, created_at, COALESCE(phone,''), phone_confirmed_at IS NOT NULL FROM trusted_contacts WHERE user_id=$1::uuid ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Contact{}
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Confirmed, &c.CreatedAt, &c.Phone, &c.PhoneConfirmed); err != nil {
			return nil, err
		}
		c.Phone = maskPhone(c.Phone)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) RemoveContact(ctx context.Context, userID, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM trusted_contacts WHERE id=$1::uuid AND user_id=$2::uuid`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- plans ----

type Plan struct {
	ID        string    `json:"id"`
	Contact   string    `json:"contact"`
	Place     string    `json:"place"`
	MeetAt    time.Time `json:"meet_at"`
	CheckinBy time.Time `json:"checkin_by"`
	Who       string    `json:"who,omitempty"`
	Status    string    `json:"status"`
	CanExtend bool      `json:"can_extend"`
}

type PlanInput struct {
	ContactID string
	Place     string
	MeetAt    time.Time
	AfterMin  int    // check-in is due this many minutes after meeting (30 to 720)
	MatchID   string // optional
	ShareName bool   // include the match's name (only if they are shown in full)
}

func fmtTime(t time.Time) string { return t.UTC().Format("Mon 2 Jan, 15:04 MST") }

// CreatePlan records a plan and emails the contact.
func (s *Service) CreatePlan(ctx context.Context, userID string, in PlanInput) (*Plan, error) {
	place, err := cleanLine(in.Place, 120, "place")
	if err != nil {
		return nil, err
	}
	now := s.now()
	if in.MeetAt.Before(now.Add(-2*time.Hour)) || in.MeetAt.After(now.Add(30*24*time.Hour)) {
		return nil, users.ValidationError{Msg: "choose a time from now to 30 days ahead"}
	}
	if in.AfterMin < 30 || in.AfterMin > 720 {
		return nil, users.ValidationError{Msg: "check in between 30 minutes and 12 hours after you meet"}
	}
	if _, err := uuid.Parse(in.ContactID); err != nil {
		return nil, ErrNotFound
	}
	var cname, cemail string
	var confirmed bool
	err = s.db.QueryRow(ctx, `SELECT name, email, confirmed_at IS NOT NULL FROM trusted_contacts WHERE id=$1::uuid AND user_id=$2::uuid`, in.ContactID, userID).Scan(&cname, &cemail, &confirmed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !confirmed {
		return nil, users.ValidationError{Msg: "that contact has not confirmed yet"}
	}
	var active int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM date_plans WHERE user_id=$1::uuid AND status='active'`, userID).Scan(&active); err != nil {
		return nil, err
	}
	if active >= maxPlansActive {
		return nil, users.ValidationError{Msg: "you already have active plans; check in on one first"}
	}
	who, matchID := "", ""
	if in.MatchID != "" {
		other, err := s.otherOf(ctx, userID, in.MatchID)
		if err != nil {
			return nil, err
		}
		matchID = in.MatchID
		if in.ShareName {
			if ok, _ := s.users.SeesFull(ctx, userID, other); ok {
				_ = s.db.QueryRow(ctx, `SELECT COALESCE(display_name,'') FROM profiles WHERE user_id=$1::uuid`, other).Scan(&who)
			}
		}
	}
	by := in.MeetAt.Add(time.Duration(in.AfterMin) * time.Minute)
	var p Plan
	var mid any
	if matchID != "" {
		mid = matchID
	}
	err = s.db.QueryRow(ctx, `
		INSERT INTO date_plans (user_id, contact_id, match_id, place, meet_at, checkin_by, who)
		VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7)
		RETURNING id::text, place, meet_at, checkin_by, who, status`, userID, in.ContactID, mid, place, in.MeetAt, by, who).
		Scan(&p.ID, &p.Place, &p.MeetAt, &p.CheckinBy, &p.Who, &p.Status)
	if err != nil {
		return nil, err
	}
	p.Contact, p.CanExtend = cname, true
	name := s.memberName(ctx, userID)
	line := "They are meeting someone"
	if who != "" {
		line = "They are meeting " + who
	}
	body := fmt.Sprintf("%s has shared a date plan with you on ConnectMe.\r\n\r\n%s at:\r\n%s\r\nOn: %s\r\n\r\n"+
		"They have promised to check in by %s. If you have not heard from them by then, you will get another email.\r\n\r\n"+
		"You are receiving this because they named you as a trusted contact.\r\n", name, line, place, fmtTime(in.MeetAt), fmtTime(by))
	_ = s.mail.Send(ctx, cemail, name+" shared a date plan with you", body)
	metrics.Event("safety_plan_created")
	return &p, nil
}

func (s *Service) memberName(ctx context.Context, userID string) string {
	var n string
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(display_name,'') FROM profiles WHERE user_id=$1::uuid`, userID).Scan(&n)
	if n == "" {
		return "A ConnectMe member"
	}
	return n
}

func (s *Service) otherOf(ctx context.Context, userID, matchID string) (string, error) {
	if _, err := uuid.Parse(matchID); err != nil {
		return "", ErrNotFound
	}
	var other string
	err := s.db.QueryRow(ctx, `
		SELECT CASE WHEN user1_id=$2::uuid THEN user2_id ELSE user1_id END::text FROM matches
		 WHERE id=$1::uuid AND (user1_id=$2::uuid OR user2_id=$2::uuid)`, matchID, userID).Scan(&other)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return other, err
}

func (s *Service) Plans(ctx context.Context, userID string) ([]Plan, error) {
	rows, err := s.db.Query(ctx, `
		SELECT p.id::text, c.name, p.place, p.meet_at, p.checkin_by, p.who, p.status, p.extensions < $2
		  FROM date_plans p JOIN trusted_contacts c ON c.id = p.contact_id
		 WHERE p.user_id=$1::uuid ORDER BY p.created_at DESC LIMIT 20`, userID, maxExtensions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.Contact, &p.Place, &p.MeetAt, &p.CheckinBy, &p.Who, &p.Status, &p.CanExtend); err != nil {
			return nil, err
		}
		p.CanExtend = p.CanExtend && p.Status == "active"
		out = append(out, p)
	}
	return out, rows.Err()
}

// Finish ends a plan: "safe" (checked in) or "cancel" (the date is off). The
// contact is told either way; an alerted plan can still be marked safe, which
// tells the contact everything is fine.
func (s *Service) Finish(ctx context.Context, userID, id, how string) error {
	if how != "safe" && how != "cancel" {
		return users.ValidationError{Msg: "how must be safe or cancel"}
	}
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	status := map[string]string{"safe": "safe", "cancel": "cancelled"}[how]
	var cemail, place string
	var wasAlerted bool
	err := s.db.QueryRow(ctx, `
		WITH was AS (SELECT status = 'alerted' AS alerted FROM date_plans WHERE id=$1::uuid AND user_id=$2::uuid),
		     upd AS (
		       UPDATE date_plans p SET status=$3 FROM trusted_contacts c
		        WHERE p.id=$1::uuid AND p.user_id=$2::uuid AND p.status IN ('active','alerted') AND c.id = p.contact_id
		       RETURNING c.email, p.place)
		SELECT upd.email, upd.place, (SELECT alerted FROM was) FROM upd`, id, userID, status).Scan(&cemail, &place, &wasAlerted)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	// Whoever was alerted must also hear that all is well; otherwise only the
	// plan's own contact is told.
	to := []string{cemail}
	if wasAlerted {
		to = s.confirmedEmails(ctx, userID, cemail)
	}
	name := s.memberName(ctx, userID)
	if wasAlerted && how == "safe" {
		s.textAll(ctx, userID, name+" has checked in and is safe. You do not need to do anything.")
	}
	for _, addr := range to {
		if how == "safe" {
			_ = s.mail.Send(ctx, addr, name+" has checked in", name+" has checked in and is safe. You do not need to do anything.\r\n")
		} else {
			_ = s.mail.Send(ctx, addr, name+" cancelled their plan", name+" cancelled the plan to meet at "+place+". You do not need to do anything.\r\n")
		}
	}
	return nil
}

// Extend gives the member more time before the contact is alerted (up to three
// times, 15 minutes to two hours each).
func (s *Service) Extend(ctx context.Context, userID, id string, minutes int) error {
	if minutes < 15 || minutes > 120 {
		return users.ValidationError{Msg: "add between 15 minutes and 2 hours"}
	}
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE date_plans SET checkin_by = checkin_by + make_interval(mins => $3), extensions = extensions + 1, reminded = FALSE
		 WHERE id=$1::uuid AND user_id=$2::uuid AND status='active' AND extensions < $4`, id, userID, minutes, maxExtensions)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Sweep reminds members whose check-in is near and emails the confirmed
// contacts of anyone whose deadline has passed. It returns how many plans alerted.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	// Reminder: 15 minutes before, once.
	rows, err := s.db.Query(ctx, `
		UPDATE date_plans SET reminded = TRUE
		 WHERE status='active' AND NOT reminded AND checkin_by <= $1::timestamptz + INTERVAL '15 minutes' AND checkin_by > $1::timestamptz
		RETURNING id::text, user_id::text`, s.now())
	if err != nil {
		return 0, err
	}
	var remind [][2]string
	for rows.Next() {
		var r [2]string
		if rows.Scan(&r[0], &r[1]) == nil {
			remind = append(remind, r)
		}
	}
	rows.Close()
	if s.notifier != nil {
		for _, r := range remind {
			s.notifier.Notify(ctx, r[1], push.KindSafety, r[0])
		}
	}
	// Overdue: alert exactly once (the status change is the lock).
	late, err := s.db.Query(ctx, `
		UPDATE date_plans p SET status='alerted', alerted_at=NOW()
		 WHERE p.status='active' AND p.checkin_by <= $1
		RETURNING p.user_id::text, (SELECT email FROM trusted_contacts WHERE id = p.contact_id), p.place, p.meet_at, p.checkin_by`, s.now())
	if err != nil {
		return 0, err
	}
	type alert struct {
		user, contactEmail, place string
		meet, by                  time.Time
	}
	var alerts []alert
	for late.Next() {
		var a alert
		if late.Scan(&a.user, &a.contactEmail, &a.place, &a.meet, &a.by) == nil {
			alerts = append(alerts, a)
		}
	}
	late.Close()
	for _, a := range alerts {
		name := s.memberName(ctx, a.user)
		body := fmt.Sprintf("%s said they would check in by %s after meeting someone at %s (%s), and they have not.\r\n\r\n"+
			"This may be nothing: a flat battery or a good evening. Please try to reach them by phone or message. "+
			"If you cannot and you are worried, contact the local emergency services and share the place above.\r\n", name, fmtTime(a.by), a.place, fmtTime(a.meet))
		// Everyone the member has confirmed is told, the plan's own contact
		// first: one person who does not read email in time must not be the
		// only safety net.
		for _, to := range s.confirmedEmails(ctx, a.user, a.contactEmail) {
			if err := s.mail.Send(ctx, to, name+" has not checked in", body); err != nil {
				slog.Error("safety: alert email failed", "err", err)
				continue
			}
			metrics.Event("safety_alert_sent")
		}
		s.textAll(ctx, a.user, name+" said they would check in by "+fmtTime(a.by)+" after meeting someone at "+a.place+", and has not. Please try to reach them. If you are worried, contact local emergency services.")
	}
	return len(alerts), nil
}

// confirmedEmails lists every confirmed contact of a member with the given
// address (the plan's own contact) first, so one person who does not read their
// mail in time is not the only safety net.
func (s *Service) confirmedEmails(ctx context.Context, userID, first string) []string {
	rows, err := s.db.Query(ctx, `
		SELECT email FROM trusted_contacts WHERE user_id=$1::uuid AND confirmed_at IS NOT NULL
		 ORDER BY (email = $2) DESC, created_at`, userID, first)
	if err != nil {
		slog.Error("safety: contacts", "err", err)
		return []string{first}
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return []string{first}
	}
	return out
}

// StartJob runs Sweep every interval until ctx ends.
func (s *Service) StartJob(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			started := time.Now()
			n, err := s.Sweep(ctx)
			metrics.Job("safety_sweep", time.Since(started), err)
			if err != nil && ctx.Err() == nil {
				slog.Error("safety: sweep", "err", err)
			}
			_ = n
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
