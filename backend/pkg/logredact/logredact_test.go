package logredact

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func capture(t *testing.T, f func(l *slog.Logger)) string {
	t.Helper()
	var buf bytes.Buffer
	f(slog.New(Wrap(slog.NewTextHandler(&buf, nil))))
	return buf.String()
}

func TestSecretFieldsAreNeverWritten(t *testing.T) {
	out := capture(t, func(l *slog.Logger) {
		l.Info("event",
			"password", "hunter2-hunter2", "refresh_token", "rt-abc", "Authorization", "Bearer eyJabc",
			"ciphertext", "c2VjcmV0", "api_key", "sk_live_x", "body", "hello there", "text", "your code is 123456",
			"email", "anna@example.com", "phone", "+1 555 010 0042", "to", "bob@example.com", "kind", "verify_email")
	})
	for _, leak := range []string{"hunter2", "rt-abc", "eyJabc", "c2VjcmV0", "sk_live_x", "hello there", "123456", "anna@example.com", "bob@example.com", "0042 "} {
		if strings.Contains(out, leak) {
			t.Errorf("the log still contains %q: %s", leak, out)
		}
	}
	for _, keep := range []string{"kind=verify_email", "a***@example.com", "b***@example.com", "***42"} {
		if !strings.Contains(out, keep) {
			t.Errorf("the log lost %q: %s", keep, out)
		}
	}
}

func TestGroupsAndPreAttachedFieldsAreRedactedToo(t *testing.T) {
	out := capture(t, func(l *slog.Logger) {
		l.With("session_token", "tok-123").Info("hi", slog.Group("req", "authorization", "Bearer zzz", "path", "/users/me"))
	})
	if strings.Contains(out, "tok-123") || strings.Contains(out, "Bearer zzz") {
		t.Errorf("leak: %s", out)
	}
	if !strings.Contains(out, "/users/me") {
		t.Errorf("harmless fields must survive: %s", out)
	}
}

func TestOrdinaryFieldsPassThrough(t *testing.T) {
	out := capture(t, func(l *slog.Logger) { l.Warn("slow", "route", "/chat", "ms", 120, "err", "timeout") })
	for _, want := range []string{"route=/chat", "ms=120", "err=timeout"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}
