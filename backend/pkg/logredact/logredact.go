// Package logredact wraps a slog.Handler so that fields which can hold secrets
// are never written: whatever a future log line passes, a field named like a
// password, token, secret, Authorization header, cookie, ciphertext or message
// body is replaced, and email addresses and phone numbers are masked.
package logredact

import (
	"context"
	"log/slog"
	"strings"

	"connectme/pkg/utils"
)

const redacted = "[redacted]"

var secretWords = []string{"password", "passwd", "secret", "token", "authorization", "cookie", "ciphertext", "credential", "apikey", "api_key", "otp", "code_hash"}

// exactSecret keys are redacted only when they match exactly, because the words
// are too common to match as a substring ("body" appears in many field names).
var exactSecret = map[string]bool{"body": true, "text": true, "message": false}

func isSecret(key string) bool {
	k := strings.ToLower(key)
	if exactSecret[k] {
		return true
	}
	for _, w := range secretWords {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

func scrub(a slog.Attr) slog.Attr {
	k := strings.ToLower(a.Key)
	switch {
	case a.Value.Kind() == slog.KindGroup:
		var out []any
		for _, g := range a.Value.Group() {
			out = append(out, scrub(g))
		}
		return slog.Group(a.Key, out...)
	case isSecret(a.Key):
		return slog.String(a.Key, redacted)
	case k == "email" || k == "to" && strings.Contains(a.Value.String(), "@"):
		return slog.String(a.Key, utils.MaskEmail(a.Value.String()))
	case k == "phone":
		return slog.String(a.Key, utils.MaskPhone(a.Value.String()))
	}
	return a
}

type handler struct{ next slog.Handler }

// Wrap returns h with secret fields redacted.
func Wrap(h slog.Handler) slog.Handler { return handler{h} }

func (h handler) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h handler) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(scrub(a))
		return true
	})
	return h.next.Handle(ctx, clean)
}

func (h handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = scrub(a)
	}
	return handler{h.next.WithAttrs(out)}
}

func (h handler) WithGroup(name string) slog.Handler { return handler{h.next.WithGroup(name)} }
