package push

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func testKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	return k, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func credentials(t *testing.T, tokenURI, pemKey string) []byte {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"client_email": "push@proj.iam.gserviceaccount.com", "private_key": pemKey, "token_uri": tokenURI})
	return b
}

func TestFCMSignsAssertionCachesTokenAndBuildsGenericPayload(t *testing.T) {
	key, pemKey := testKey(t)
	var exchanges, sends int32
	var lastBody map[string]any
	var lastAuth, lastPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			atomic.AddInt32(&exchanges, 1)
			b, _ := io.ReadAll(r.Body)
			form := string(b)
			if !strings.Contains(form, "grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Ajwt-bearer") {
				t.Errorf("grant type: %s", form)
			}
			assertion := strings.Split(strings.Split(form, "assertion=")[1], "&")[0]
			claims := jwt.MapClaims{}
			if _, err := jwt.ParseWithClaims(assertion, claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"})); err != nil {
				t.Errorf("assertion does not verify: %v", err)
			}
			if claims["iss"] != "push@proj.iam.gserviceaccount.com" || claims["scope"] != fcmScope {
				t.Errorf("claims: %v", claims)
			}
			_, _ = w.Write([]byte(`{"access_token":"ya29.token","expires_in":3600}`))
		case strings.HasSuffix(r.URL.Path, "/messages:send"):
			atomic.AddInt32(&sends, 1)
			lastAuth, lastPath = r.Header.Get("Authorization"), r.URL.Path
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &lastBody)
			_, _ = w.Write([]byte(`{"name":"projects/p/messages/1"}`))
		}
	}))
	defer srv.Close()

	f, err := NewFCM("my-project", credentials(t, srv.URL+"/token", pemKey))
	if err != nil {
		t.Fatal(err)
	}
	f.APIBase = srv.URL
	call := Message{Title: "ConnectMe", Body: "Incoming call", HighPrio: true, TTLSeconds: 45, Collapse: "call-1", Data: map[string]string{"kind": "call", "ref": "c1"}}
	for i := 0; i < 2; i++ {
		if err := f.Send(context.Background(), "android", "device-token", call); err != nil {
			t.Fatal(err)
		}
	}
	if exchanges != 1 || sends != 2 {
		t.Errorf("exchanges=%d sends=%d (token must be cached)", exchanges, sends)
	}
	if lastAuth != "Bearer ya29.token" || lastPath != "/v1/projects/my-project/messages:send" {
		t.Errorf("request: %q %q", lastAuth, lastPath)
	}
	m := lastBody["message"].(map[string]any)
	if m["token"] != "device-token" || m["notification"].(map[string]any)["body"] != "Incoming call" {
		t.Errorf("message: %v", m)
	}
	android := m["android"].(map[string]any)
	apns := m["apns"].(map[string]any)["headers"].(map[string]any)
	if android["priority"] != "HIGH" || android["ttl"] != "45s" || android["collapse_key"] != "call-1" || apns["apns-priority"] != "10" || apns["apns-collapse-id"] != "call-1" {
		t.Errorf("delivery hints: %v %v", android, apns)
	}
	if d := m["data"].(map[string]any); len(d) != 2 || d["kind"] != "call" || d["ref"] != "c1" {
		t.Errorf("data: %v", d)
	}
}

func TestFCMOnlyDeletesTokensTheProviderDisowns(t *testing.T) {
	_, pemKey := testKey(t)
	status, body := 200, `{}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	f, _ := NewFCM("p", credentials(t, srv.URL+"/token", pemKey))
	f.APIBase = srv.URL
	send := func() error { return f.Send(context.Background(), "ios", "tok", Message{Body: "x"}) }

	status, body = 404, `{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`
	if err := send(); !errors.Is(err, ErrUnregistered) {
		t.Errorf("unregistered: %v", err)
	}
	status, body = 400, `{"error":{"status":"INVALID_ARGUMENT"}}`
	if err := send(); err == nil || errors.Is(err, ErrUnregistered) {
		t.Errorf("a 400 may be our own payload bug and must not delete tokens: %v", err)
	}
	status, body = 500, `{}`
	if err := send(); err == nil || errors.Is(err, ErrUnregistered) {
		t.Errorf("outage must not delete tokens: %v", err)
	}
	status, body = 200, `{}`
	if err := send(); err != nil {
		t.Errorf("success: %v", err)
	}
}

func TestFCMRejectsBadCredentials(t *testing.T) {
	_, pemKey := testKey(t)
	for name, creds := range map[string][]byte{
		"not json":    []byte("nope"),
		"missing":     []byte(`{"client_email":"x"}`),
		"not pem":     credentials(t, "https://x/token", "garbage"),
		"empty":       nil,
		"wrong field": []byte(`{"client_email":"a","private_key":"` + strings.ReplaceAll(pemKey, "\n", `\n`) + `"}`),
	} {
		if _, err := NewFCM("p", creds); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestFCMTokenExchangeFailureIsAnError(t *testing.T) {
	_, pemKey := testKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 401) }))
	defer srv.Close()
	f, _ := NewFCM("p", credentials(t, srv.URL+"/token", pemKey))
	f.APIBase = srv.URL
	if err := f.Send(context.Background(), "ios", "tok", Message{Body: "x"}); err == nil || errors.Is(err, ErrUnregistered) {
		t.Errorf("exchange failure: %v", err)
	}
}

func TestContentIsGenericForEveryKind(t *testing.T) {
	for _, k := range []Kind{KindMessage, KindMatch, KindCall, KindBooking} {
		m := content(k)
		if m.Title != "ConnectMe" || m.Body == "" || len(m.Body) > 40 || strings.ContainsAny(m.Body, "{}%") {
			t.Errorf("%s: %+v", k, m)
		}
	}
	if content("nonsense").Body != "" {
		t.Error("unknown kinds must produce nothing")
	}
}
