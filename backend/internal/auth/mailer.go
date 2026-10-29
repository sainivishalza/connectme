package auth

import (
	"connectme/pkg/utils"
	"context"
	"log/slog"
	"os"
)

// Mailer delivers account emails. Implementations must not log tokens in production.
type Mailer interface {
	SendVerification(ctx context.Context, to, token string) error
	SendPasswordReset(ctx context.Context, to, token string) error
	SendAlreadyRegistered(ctx context.Context, to string) error
}

// LogMailer is the development mailer: it writes emails to the log.
// Addresses are masked and tokens and bodies stay out of the log unless a
// developer opts in with DEV_LOG_SECRETS=true; production never logs them.
type LogMailer struct{ Production bool }

// showDetails reports whether this process may print links, tokens and bodies.
func (m LogMailer) showDetails() bool { return !m.Production && os.Getenv("DEV_LOG_SECRETS") == "true" }

func (m LogMailer) log(kind, to, token string) error {
	if m.Production || token == "" {
		slog.Warn("email not sent: no mail provider configured", "kind", kind)
		return nil
	}
	if !m.showDetails() {
		slog.Info("DEV email (details hidden; set DEV_LOG_SECRETS=true to print the token)", "kind", kind, "to", utils.MaskEmail(to))
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
	if !m.showDetails() {
		slog.Info("DEV email (details hidden; set DEV_LOG_SECRETS=true to print the body)", "kind", "plain", "to", utils.MaskEmail(to))
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
