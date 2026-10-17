package config

import (
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"ENV", "POSTGRES_URL", "REDIS_URL", "MONGO_URL", "JWT_SECRET",
		"JWT_ACCESS_EXPIRY", "JWT_REFRESH_EXPIRY", "STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET", "APP_ALLOWED_ORIGINS",
		"SMTP_HOST", "SMTP_PORT", "SMTP_FROM", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_ALLOW_PLAINTEXT", "LIVEKIT_URL", "CSAM_PROVIDER", "ENABLE_VIDEO", "VIDEO_SCANNER", "VIDEO_SCANNER_URL", "VIDEO_SCANNER_KEY", "PUSH_PROVIDER", "FCM_PROJECT_ID", "FIREBASE_CREDENTIALS"} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func valid() map[string]string {
	return map[string]string{
		"POSTGRES_URL": "postgres://x", "REDIS_URL": "redis://x", "MONGO_URL": "mongodb://x",
		"JWT_SECRET": strings.Repeat("a", 40),
	}
}

func TestLoadOK(t *testing.T) {
	setEnv(t, valid())
	c, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Port != "8080" || c.JWTAccessExpiry.Minutes() != 15 {
		t.Fatalf("bad defaults: %+v", c)
	}
}

func TestLoadFailsFast(t *testing.T) {
	cases := map[string]func(m map[string]string){
		"missing postgres": func(m map[string]string) { delete(m, "POSTGRES_URL") },
		"short secret":     func(m map[string]string) { m["JWT_SECRET"] = "short" },
		"placeholder":      func(m map[string]string) { m["JWT_SECRET"] = "change-me-" + strings.Repeat("x", 40) },
		"bad expiry":       func(m map[string]string) { m["JWT_ACCESS_EXPIRY"] = "nope" },
		"long access":      func(m map[string]string) { m["JWT_ACCESS_EXPIRY"] = "48h" },
		"prod no stripe":   func(m map[string]string) { m["ENV"] = "production"; m["APP_ALLOWED_ORIGINS"] = "https://a" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := valid()
			mutate(m)
			setEnv(t, m)
			if _, err := Load(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestProductionRequiresRealEmailDelivery(t *testing.T) {
	prod := func() map[string]string {
		m := valid()
		m["ENV"] = "production"
		m["APP_ALLOWED_ORIGINS"] = "https://app.example.com"
		m["STRIPE_SECRET_KEY"] = "sk_live_x"
		m["STRIPE_WEBHOOK_SECRET"] = "whsec_x"
		m["AI_SERVICE_KEY"] = "k"
		m["CSAM_PROVIDER"] = "photodna"
		return m
	}
	setEnv(t, prod())
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SMTP_HOST") {
		t.Fatalf("production without SMTP should fail naming SMTP_HOST, got %v", err)
	}
	m := prod()
	m["SMTP_HOST"], m["SMTP_FROM"] = "smtp.example.com", "ConnectMe <no-reply@example.com>"
	setEnv(t, m)
	if _, err := Load(); err != nil {
		t.Fatalf("fully configured production rejected: %v", err)
	}
	m["SMTP_ALLOW_PLAINTEXT"] = "true"
	setEnv(t, m)
	if _, err := Load(); err == nil {
		t.Fatal("plaintext SMTP allowed in production")
	}
}

func TestProductionRefusesUnscannedVideo(t *testing.T) {
	m := valid()
	m["ENV"] = "production"
	m["APP_ALLOWED_ORIGINS"] = "https://app.example.com"
	m["STRIPE_SECRET_KEY"] = "sk_live_x"
	m["STRIPE_WEBHOOK_SECRET"] = "whsec_x"
	m["AI_SERVICE_KEY"] = "k"
	m["CSAM_PROVIDER"] = "photodna"
	m["SMTP_HOST"], m["SMTP_FROM"] = "smtp.example.com", "ConnectMe <no-reply@example.com>"
	m["ENABLE_VIDEO"] = "true"
	setEnv(t, m)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ENABLE_VIDEO") {
		t.Fatalf("production with videos on should fail naming ENABLE_VIDEO, got %v", err)
	}
	m["ENV"] = "development"
	setEnv(t, m)
	if _, err := Load(); err != nil {
		t.Fatalf("videos in development rejected: %v", err)
	}
}

func TestVideoScannerConfiguration(t *testing.T) {
	prod := func() map[string]string {
		m := valid()
		m["ENV"] = "production"
		m["APP_ALLOWED_ORIGINS"] = "https://app.example.com"
		m["STRIPE_SECRET_KEY"] = "sk_live_x"
		m["STRIPE_WEBHOOK_SECRET"] = "whsec_x"
		m["AI_SERVICE_KEY"] = "k"
		m["CSAM_PROVIDER"] = "photodna"
		m["SMTP_HOST"], m["SMTP_FROM"] = "smtp.example.com", "ConnectMe <no-reply@example.com>"
		m["ENABLE_VIDEO"] = "true"
		return m
	}
	try := func(m map[string]string) error {
		setEnv(t, m)
		_, err := Load()
		return err
	}
	m := prod()
	m["VIDEO_SCANNER"] = "http"
	if err := try(m); err == nil || !strings.Contains(err.Error(), "VIDEO_SCANNER_URL") {
		t.Errorf("http scanner without URL/key: %v", err)
	}
	m["VIDEO_SCANNER_URL"], m["VIDEO_SCANNER_KEY"] = "http://scan.example.com/v1", "k"
	if err := try(m); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("plain http scanner URL in production: %v", err)
	}
	m["VIDEO_SCANNER_URL"] = "https://scan.example.com/v1"
	if err := try(m); err != nil {
		t.Errorf("fully configured production with a scanner rejected: %v", err)
	}
	m["VIDEO_SCANNER"] = "magic"
	if err := try(m); err == nil || !strings.Contains(err.Error(), "VIDEO_SCANNER must be") {
		t.Errorf("unknown scanner: %v", err)
	}
	// Videos off: the scanner settings do not matter.
	off := prod()
	off["ENABLE_VIDEO"] = "false"
	if err := try(off); err != nil {
		t.Errorf("videos off in production: %v", err)
	}
}

func TestPushProviderConfiguration(t *testing.T) {
	try := func(kv map[string]string) error {
		m := valid()
		for k, v := range kv {
			m[k] = v
		}
		setEnv(t, m)
		_, err := Load()
		return err
	}
	if err := try(nil); err != nil {
		t.Errorf("default (stub): %v", err)
	}
	if err := try(map[string]string{"PUSH_PROVIDER": "fcm"}); err == nil || !strings.Contains(err.Error(), "FCM_PROJECT_ID") {
		t.Errorf("fcm without settings: %v", err)
	}
	if err := try(map[string]string{"PUSH_PROVIDER": "fcm", "FCM_PROJECT_ID": "p", "FIREBASE_CREDENTIALS": "/run/secrets/fcm.json"}); err != nil {
		t.Errorf("fcm configured: %v", err)
	}
	if err := try(map[string]string{"PUSH_PROVIDER": "apns"}); err == nil || !strings.Contains(err.Error(), "PUSH_PROVIDER must be") {
		t.Errorf("unknown provider: %v", err)
	}
}
