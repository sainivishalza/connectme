package push

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// serviceAccount is the part of a Google service-account key file we need.
type serviceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// FCM sends through Firebase Cloud Messaging's HTTP v1 API, authenticating
// with a service account (a signed JWT exchanged for a short-lived OAuth
// token, cached until shortly before it expires).
type FCM struct {
	ProjectID string
	APIBase   string // default https://fcm.googleapis.com
	HTTP      *http.Client

	sa  serviceAccount
	key *rsa.PrivateKey

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// NewFCM parses a service-account key (the JSON file's contents).
func NewFCM(projectID string, credentialsJSON []byte) (*FCM, error) {
	var sa serviceAccount
	if err := json.Unmarshal(credentialsJSON, &sa); err != nil {
		return nil, fmt.Errorf("fcm credentials: %w", err)
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" || sa.TokenURI == "" {
		return nil, errors.New("fcm credentials: client_email, private_key and token_uri are required")
	}
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return nil, errors.New("fcm credentials: private_key is not PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("fcm credentials: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("fcm credentials: private_key is not RSA")
	}
	return &FCM{ProjectID: projectID, sa: sa, key: rk}, nil
}

func (*FCM) Name() string { return "fcm" }

func (f *FCM) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (f *FCM) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token != "" && time.Until(f.expiry) > time.Minute {
		return f.token, nil
	}
	now := time.Now()
	assertion, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": f.sa.ClientEmail, "scope": fcmScope, "aud": f.sa.TokenURI,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}).SignedString(f.key)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.sa.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := f.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fcm token exchange: %s", resp.Status)
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &t); err != nil || t.AccessToken == "" {
		return "", errors.New("fcm token exchange: unreadable answer")
	}
	f.token, f.expiry = t.AccessToken, now.Add(time.Duration(t.ExpiresIn)*time.Second)
	return f.token, nil
}

func (f *FCM) Send(ctx context.Context, platform, token string, m Message) error {
	at, err := f.accessToken(ctx)
	if err != nil {
		return err
	}
	msg := map[string]any{
		"token":        token,
		"notification": map[string]string{"title": m.Title, "body": m.Body},
		"data":         m.Data,
	}
	android := map[string]any{"priority": "NORMAL"}
	apnsHeaders := map[string]string{"apns-priority": "5"}
	if m.HighPrio {
		android["priority"] = "HIGH"
		apnsHeaders["apns-priority"] = "10"
	}
	if m.TTLSeconds > 0 {
		android["ttl"] = strconv.Itoa(m.TTLSeconds) + "s"
		apnsHeaders["apns-expiration"] = strconv.FormatInt(time.Now().Add(time.Duration(m.TTLSeconds)*time.Second).Unix(), 10)
	}
	if m.Collapse != "" {
		android["collapse_key"] = m.Collapse
		apnsHeaders["apns-collapse-id"] = m.Collapse
	}
	msg["android"] = android
	msg["apns"] = map[string]any{"headers": apnsHeaders}
	body, err := json.Marshal(map[string]any{"message": msg})
	if err != nil {
		return err
	}
	base := f.APIBase
	if base == "" {
		base = "https://fcm.googleapis.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/v1/projects/"+url.PathEscape(f.ProjectID)+"/messages:send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+at)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	// A token the provider no longer knows is not an outage: drop it.
	var e struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	code := ""
	for _, d := range e.Error.Details {
		if d.ErrorCode != "" {
			code = d.ErrorCode
		}
	}
	// Only an explicit "unregistered" deletes a token: a 400 may be our own
	// payload bug, and must never wipe every device.
	if resp.StatusCode == http.StatusNotFound || code == "UNREGISTERED" {
		return ErrUnregistered
	}
	return fmt.Errorf("fcm send: %s", resp.Status)
}
