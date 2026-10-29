package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// SMTPConfig configures SMTPMailer. Any SMTP relay works (SES, SendGrid,
// Postmark, a company relay).
type SMTPConfig struct {
	Host     string
	Port     int    // 587 (STARTTLS) by default; 465 uses implicit TLS
	Username string // optional
	Password string
	From     string // "ConnectMe <no-reply@example.com>"
	// AllowPlaintext permits an unencrypted connection (local test servers
	// only). Credentials are never sent without TLS regardless.
	AllowPlaintext bool
}

// SMTPMailer sends account emails over SMTP. Sending is asynchronous through
// a bounded queue so a slow relay never slows registration and the response
// time does not depend on whether an address already has an account. If the
// queue is full the message is dropped and logged (the user can request
// another).
type SMTPMailer struct {
	cfg  SMTPConfig
	from *mail.Address
	q    chan outgoing
}

type outgoing struct {
	to, subject, body string
}

// NewSMTPMailer validates the configuration and starts the send workers.
func NewSMTPMailer(cfg SMTPConfig) (*SMTPMailer, error) {
	if cfg.Host == "" || cfg.From == "" {
		return nil, errors.New("smtp: host and from are required")
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("smtp: invalid from address: %w", err)
	}
	m := &SMTPMailer{cfg: cfg, from: from, q: make(chan outgoing, 256)}
	for i := 0; i < 4; i++ {
		go m.worker()
	}
	return m, nil
}

func (m *SMTPMailer) worker() {
	for msg := range m.q {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if err := m.deliver(ctx, msg); err != nil {
			slog.Error("smtp: delivery failed", "err", err) // never log the body (it holds a token)
		}
		cancel()
	}
}

func (m *SMTPMailer) enqueue(to, subject, body string) error {
	addr, err := mail.ParseAddress(to)
	if err != nil || strings.ContainsAny(to, "\r\n") {
		return errors.New("smtp: invalid recipient")
	}
	select {
	case m.q <- outgoing{to: addr.Address, subject: subject, body: body}:
	default:
		slog.Warn("smtp: queue full, message dropped")
	}
	return nil
}

// Send queues a plain email (used by the safety toolkit).
func (m *SMTPMailer) Send(_ context.Context, to, subject, body string) error {
	return m.enqueue(to, subject, body)
}

func (m *SMTPMailer) SendVerification(_ context.Context, to, token string) error {
	return m.enqueue(to, "Verify your ConnectMe email",
		"Welcome to ConnectMe.\r\n\r\nYour verification code is:\r\n\r\n"+token+
			"\r\n\r\nEnter it in the app. If you did not create an account you can ignore this email.\r\n")
}

func (m *SMTPMailer) SendPasswordReset(_ context.Context, to, token string) error {
	return m.enqueue(to, "Reset your ConnectMe password",
		"We received a request to reset your password.\r\n\r\nYour reset code is:\r\n\r\n"+token+
			"\r\n\r\nIt expires soon and works once. If you did not ask for this, ignore this email; your password is unchanged.\r\n")
}

func (m *SMTPMailer) SendAlreadyRegistered(_ context.Context, to string) error {
	return m.enqueue(to, "Someone tried to sign up with your email",
		"Someone tried to create a ConnectMe account with this address, but you already have one.\r\n\r\n"+
			"If it was you, sign in or use \"Forgot password\". If not, no action is needed.\r\n")
}

func (m *SMTPMailer) deliver(ctx context.Context, msg outgoing) error {
	addr := net.JoinHostPort(m.cfg.Host, fmt.Sprint(m.cfg.Port))
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	tlsCfg := &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}
	if m.cfg.Port == 465 {
		conn = tls.Client(conn, tlsCfg)
	}
	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	encrypted := m.cfg.Port == 465
	if ok, _ := c.Extension("STARTTLS"); ok && !encrypted {
		if err := c.StartTLS(tlsCfg); err != nil {
			return err
		}
		encrypted = true
	}
	if !encrypted && !m.cfg.AllowPlaintext {
		return errors.New("smtp: server does not offer TLS and plaintext is not allowed")
	}
	if m.cfg.Username != "" {
		if !encrypted {
			return errors.New("smtp: refusing to send credentials without TLS")
		}
		if err := c.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(m.from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(msg.to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(buildMessage(m.from.String(), msg.to, msg.subject, msg.body, time.Now())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// buildMessage assembles the raw email: headers, a blank line, then the body.
func buildMessage(from, to, subject, body string, now time.Time) []byte {
	headers := "From: " + headerValue(from) + "\r\n" +
		"To: " + headerValue(to) + "\r\n" +
		"Subject: " + headerValue(subject) + "\r\n" +
		"Date: " + now.UTC().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n"
	return []byte(headers + body)
}

// headerValue makes a string safe for a single header line: every control
// character, line breaks included, becomes a space. A member controls their
// display name and it is part of some subjects, so a line break here would let
// them add headers or start the body early.
func headerValue(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f, r == 0x85, r == 0x2028, r == 0x2029:
			return ' '
		}
		return r
	}, s)
}
