package auth

import (
	"context"
	"log/slog"
)

// Mailer delivers account emails. Implementations must not log tokens in production.
type Mailer interface {
	SendVerification(ctx context.Context, to, token string) error
	SendPasswordReset(ctx context.Context, to, token string) error
	SendAlreadyRegistered(ctx context.Context, to string) error
}

// LogMailer is the development mailer: it writes emails to the log.
// Tokens are only logged outside production.
type LogMailer struct{ Production bool }

func (m LogMailer) log(kind, to, token string) error {
	if m.Production || token == "" {
		slog.Warn("email not sent: no mail provider configured", "kind", kind)
		return nil
	}
	slog.Info("DEV email", "kind", kind, "to", to, "token", token)
	return nil
}

// Send writes a plain email to the log in development. The body can hold links
// meant for someone else, so production never logs it.
func (m LogMailer) Send(_ context.Context, to, subject, body string) error {
	if m.Production {
		slog.Warn("email not sent: no mail provider configured", "kind", "plain")
		return nil
	}
	slog.Info("DEV email", "kind", "plain", "to", to, "subject", subject, "body", body)
	return nil
}

func (m LogMailer) SendVerification(_ context.Context, to, token string) error {
	return m.log("verify_email", to, token)
}

func (m LogMailer) SendPasswordReset(_ context.Context, to, token string) error {
	return m.log("reset_password", to, token)
}

func (m LogMailer) SendAlreadyRegistered(_ context.Context, to string) error {
	return m.log("already_registered", to, "")
}
