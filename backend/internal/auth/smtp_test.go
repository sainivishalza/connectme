package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSMTPMailerValidatesConfigAndRecipients(t *testing.T) {
	if _, err := NewSMTPMailer(SMTPConfig{}); err == nil {
		t.Error("empty config accepted")
	}
	if _, err := NewSMTPMailer(SMTPConfig{Host: "h", From: "not an address"}); err == nil {
		t.Error("bad from accepted")
	}
	m, err := NewSMTPMailer(SMTPConfig{Host: "127.0.0.1", Port: 1, From: "ConnectMe <no-reply@example.com>"})
	if err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"", "nobody", "a@b.com\r\nBcc: evil@x.com", "a@b.com\nSubject: x"} {
		if err := m.SendVerification(context.Background(), to, "123456"); err == nil {
			t.Errorf("recipient %q accepted (header injection risk)", to)
		}
	}
}

// Opt-in: run MailHog (docker run --rm -p 1025:1025 -p 8025:8025 mailhog/mailhog)
// and set TEST_SMTP_HOST=127.0.0.1 TEST_SMTP_PORT=1025 TEST_MAILHOG_API=http://127.0.0.1:8025.
func TestSMTPMailerDeliversToRealServer(t *testing.T) {
	host, port, api := os.Getenv("TEST_SMTP_HOST"), os.Getenv("TEST_SMTP_PORT"), os.Getenv("TEST_MAILHOG_API")
	if host == "" || port == "" || api == "" {
		t.Skip("TEST_SMTP_HOST / TEST_SMTP_PORT / TEST_MAILHOG_API not set")
	}
	p, _ := strconv.Atoi(port)
	m, err := NewSMTPMailer(SMTPConfig{Host: host, Port: p, From: "ConnectMe <no-reply@example.com>", AllowPlaintext: true})
	if err != nil {
		t.Fatal(err)
	}
	to := "person" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.org"
	if err := m.SendPasswordReset(context.Background(), to, "RESET-TOKEN-777"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(api + "/api/v2/search?kind=to&query=" + to)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var out struct {
				Total int `json:"total"`
				Items []struct {
					Content struct {
						Body    string
						Headers map[string][]string
					}
				} `json:"items"`
			}
			if json.Unmarshal(b, &out) == nil && out.Total == 1 {
				body := out.Items[0].Content.Body
				if !strings.Contains(body, "RESET-TOKEN-777") {
					t.Errorf("token missing from body: %q", body)
				}
				if got := out.Items[0].Content.Headers["Subject"]; len(got) != 1 || !strings.Contains(got[0], "Reset") {
					t.Errorf("subject: %v", got)
				}
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("message never arrived at the SMTP server")
}

func TestSMTPMailerRefusesCredentialsWithoutTLS(t *testing.T) {
	host, port := os.Getenv("TEST_SMTP_HOST"), os.Getenv("TEST_SMTP_PORT")
	if host == "" || port == "" {
		t.Skip("needs the plaintext test SMTP server")
	}
	p, _ := strconv.Atoi(port)
	m, _ := NewSMTPMailer(SMTPConfig{Host: host, Port: p, From: "a@example.com", Username: "u", Password: "p", AllowPlaintext: true})
	err := m.deliver(context.Background(), outgoing{to: "x@example.org", subject: "s", body: "b"})
	if err == nil || !strings.Contains(err.Error(), "without TLS") {
		t.Errorf("credentials sent over plaintext: %v", err)
	}
}
