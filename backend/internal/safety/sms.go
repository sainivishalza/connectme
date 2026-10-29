package safety

import (
	"connectme/pkg/utils"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"connectme/internal/users"
	"connectme/pkg/metrics"

	"github.com/google/uuid"
)

// SMS sends one text message. Nil means texts are not set up.
type SMS interface {
	Send(ctx context.Context, to, text string) error
}

// SetSMS turns text alerts on. Without it contacts are reached by email only.
func (s *Service) SetSMS(p SMS) { s.sms = p }

// LogSMS writes texts to the log instead of sending them (development).
type LogSMS struct{}

func (LogSMS) Send(_ context.Context, to, text string) error {
	if os.Getenv("DEV_LOG_SECRETS") != "true" {
		slog.Info("sms (not sent: no provider configured; set DEV_LOG_SECRETS=true to print it)", "to", utils.MaskPhone(to))
		return nil
	}
	slog.Info("sms (not sent: no provider configured)", "to", to, "text", text)
	return nil
}

// TwilioSMS sends through Twilio's REST API.
type TwilioSMS struct {
	AccountSID, AuthToken, From string
	Client                      *http.Client
	BaseURL                     string // tests point this at a stand-in
}

func (t TwilioSMS) Send(ctx context.Context, to, text string) error {
	base := t.BaseURL
	if base == "" {
		base = "https://api.twilio.com"
	}
	c := t.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	form := url.Values{"To": {to}, "From": {t.From}, "Body": {text}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", base, t.AccountSID), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(t.AccountSID, t.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("sms provider answered %d", resp.StatusCode)
	}
	return nil
}

var e164 = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

const (
	maxPhoneSends    = 3
	maxPhoneAttempts = 5
	phoneCodeTTL     = 15 * time.Minute
)

// maskPhone shows only the last two digits.
func maskPhone(p string) string {
	if len(p) < 4 {
		return ""
	}
	return "+" + strings.Repeat("*", len(p)-3) + p[len(p)-2:]
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// SetPhone attaches a mobile number to a contact and texts it a code. The
// contact must read the code to the member before the number is used.
func (s *Service) SetPhone(ctx context.Context, userID, contactID, phone string) error {
	if s.sms == nil {
		return users.ValidationError{Msg: "text messages are not available yet"}
	}
	phone = strings.Join(strings.Fields(phone), "")
	if !e164.MatchString(phone) {
		return users.ValidationError{Msg: "enter the number with its country code, like +14155550123"}
	}
	if _, err := uuid.Parse(contactID); err != nil {
		return ErrNotFound
	}
	code, err := newCode()
	if err != nil {
		return err
	}
	// At most three texts a day per contact; the counter resets after a day.
	tag, err := s.db.Exec(ctx, `
		UPDATE trusted_contacts SET phone=$3, phone_confirmed_at=NULL, phone_code_hash=$4, phone_code_expires=$5, phone_attempts=0,
		       phone_sends = CASE WHEN phone_sends_since IS NULL OR phone_sends_since < $6 THEN 1 ELSE phone_sends + 1 END,
		       phone_sends_since = CASE WHEN phone_sends_since IS NULL OR phone_sends_since < $6 THEN $7 ELSE phone_sends_since END
		 WHERE id=$1::uuid AND user_id=$2::uuid AND confirmed_at IS NOT NULL
		   AND (phone_sends_since IS NULL OR phone_sends_since < $6 OR phone_sends < $8)`,
		contactID, userID, phone, hash(code), s.now().Add(phoneCodeTTL), s.now().Add(-24*time.Hour), s.now(), maxPhoneSends)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists, confirmed bool
		_ = s.db.QueryRow(ctx, `SELECT TRUE, confirmed_at IS NOT NULL FROM trusted_contacts WHERE id=$1::uuid AND user_id=$2::uuid`, contactID, userID).Scan(&exists, &confirmed)
		switch {
		case !exists:
			return ErrNotFound
		case !confirmed:
			return users.ValidationError{Msg: "they need to agree by email first"}
		default:
			return users.ValidationError{Msg: "too many codes sent today; try again tomorrow"}
		}
	}
	who := s.memberName(ctx, userID)
	if err := s.sms.Send(ctx, phone, fmt.Sprintf("%s named you as someone they trust on ConnectMe. If you agree, read them this code: %s. If not, ignore this message.", who, code)); err != nil {
		slog.Warn("safety: confirmation text failed", "err", err)
		return users.ValidationError{Msg: "we could not send the text; check the number"}
	}
	return nil
}

// ConfirmPhone checks the code the contact read out. After five wrong tries
// the code is void and a new one must be sent.
func (s *Service) ConfirmPhone(ctx context.Context, userID, contactID, code string) error {
	if _, err := uuid.Parse(contactID); err != nil {
		return ErrNotFound
	}
	var want []byte
	var expires *time.Time
	var attempts int
	err := s.db.QueryRow(ctx, `
		SELECT phone_code_hash, phone_code_expires, phone_attempts FROM trusted_contacts
		 WHERE id=$1::uuid AND user_id=$2::uuid AND phone IS NOT NULL AND phone_confirmed_at IS NULL`, contactID, userID).Scan(&want, &expires, &attempts)
	if err != nil {
		return ErrNotFound
	}
	if want == nil || expires == nil || s.now().After(*expires) || attempts >= maxPhoneAttempts {
		return users.ValidationError{Msg: "that code has expired; send a new one"}
	}
	if subtle.ConstantTimeCompare(want, hash(strings.TrimSpace(code))) != 1 {
		_, _ = s.db.Exec(ctx, `UPDATE trusted_contacts SET phone_attempts = phone_attempts + 1 WHERE id=$1::uuid`, contactID)
		return users.ValidationError{Msg: "that code is not right"}
	}
	_, err = s.db.Exec(ctx, `UPDATE trusted_contacts SET phone_confirmed_at=NOW(), phone_code_hash=NULL, phone_code_expires=NULL WHERE id=$1::uuid`, contactID)
	return err
}

// RemovePhone forgets a contact's number.
func (s *Service) RemovePhone(ctx context.Context, userID, contactID string) error {
	if _, err := uuid.Parse(contactID); err != nil {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE trusted_contacts SET phone=NULL, phone_confirmed_at=NULL, phone_code_hash=NULL, phone_code_expires=NULL, phone_attempts=0
		 WHERE id=$1::uuid AND user_id=$2::uuid`, contactID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// confirmedPhones lists the numbers of a member's contacts who confirmed them.
func (s *Service) confirmedPhones(ctx context.Context, userID string) []string {
	rows, err := s.db.Query(ctx, `SELECT phone FROM trusted_contacts WHERE user_id=$1::uuid AND confirmed_at IS NOT NULL AND phone IS NOT NULL AND phone_confirmed_at IS NOT NULL ORDER BY created_at`, userID)
	if err != nil {
		slog.Error("safety: phones", "err", err)
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			out = append(out, p)
		}
	}
	return out
}

// textAll sends one short text to every confirmed number of a member.
func (s *Service) textAll(ctx context.Context, userID, text string) {
	if s.sms == nil {
		return
	}
	for _, p := range s.confirmedPhones(ctx, userID) {
		if err := s.sms.Send(ctx, p, text); err != nil {
			slog.Error("safety: alert text failed", "err", err)
			continue
		}
		metrics.Event("safety_alert_sms_sent")
	}
}

var _ = errors.New
