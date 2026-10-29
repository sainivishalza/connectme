package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"testing"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Logs are read by more people than the database is. Whatever a realistic run
// of requests writes to the log must not contain a password, a login token, an
// Authorization value, an email address, a reset or verification token, or
// message ciphertext.
func TestRequestsDoNotLogSecrets(t *testing.T) {
	e := needChat(t)
	var logs lockedBuffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	email := fmt.Sprintf("logcheck%d@example.com", rand.Intn(1e9))
	pw := "Secret-Pw-9-Value!"
	if c, b := authRegister(e, email, pw); c >= 300 {
		t.Fatalf("register: %d %s", c, b)
	}
	e.call("POST", "/auth/forgot-password", map[string]string{"email": email}, "anon")
	c, b := authLogin(e, email, pw)
	if c != 200 {
		t.Fatalf("login: %d %s", c, b)
	}
	var tok struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if err := json.Unmarshal(b, &tok); err != nil || tok.Access == "" {
		t.Fatalf("login body: %s", b)
	}
	// A garbage request and one with a bad token, to exercise the error paths.
	e.raw("POST", "/auth/login", []byte("{not json"), "anon")
	e.fire("GET", "/users/me", nil, tok.Access+"x", nil)

	// An encrypted message between two matched people.
	a, bb, mid := e.matched()
	_ = bb
	cipher := "Q0lQSEVSVEVYVE1BUktFUg==" // base64 of CIPHERTEXTMARKER
	e.call("POST", "/chat/matches/"+mid+"/messages", map[string]any{"type": "text", "ciphertext": cipher, "burn_after_s": 0}, a)

	out := logs.String()
	for name, secret := range map[string]string{
		"password": pw, "access token": tok.Access, "refresh token": tok.Refresh,
		"email address": email, "ciphertext": cipher, "ciphertext (plain)": "CIPHERTEXTMARKER",
	} {
		if secret != "" && strings.Contains(out, secret) {
			t.Errorf("the log contains the %s", name)
		}
	}
	for _, key := range []string{"token=", "body=", "Authorization", "password="} {
		if strings.Contains(out, key) {
			t.Errorf("the log has a %q field:\n%s", key, firstLineWith(out, key))
		}
	}
}

func firstLineWith(s, sub string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			if len(l) > 160 {
				l = l[:160] + "..."
			}
			return l
		}
	}
	return ""
}
