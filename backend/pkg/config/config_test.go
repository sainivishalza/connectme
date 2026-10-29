package config

import (
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	old := ImplementedCSAMProviders
	ImplementedCSAMProviders = []string{"photodna"}
	t.Cleanup(func() { ImplementedCSAMProviders = old })
	for _, k := range []string{"ENV", "POSTGRES_URL", "REDIS_URL", "MONGO_URL", "JWT_SECRET",
		"JWT_ACCESS_EXPIRY", "JWT_REFRESH_EXPIRY", "STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET", "APP_ALLOWED_ORIGINS",
		"SMTP_HOST", "SMTP_PORT", "SMTP_FROM", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_ALLOW_PLAINTEXT", "LIVEKIT_URL", "CSAM_PROVIDER", "AI_SERVICE_URL", "AI_SERVICE_KEY", "MODERATION_DEV_BYPASS", "AI_TEXT_TIMEOUT_MS", "AI_IMAGE_TIMEOUT_MS", "ENABLE_VIDEO", "VIDEO_SCANNER", "VIDEO_SCANNER_URL", "VIDEO_SCANNER_KEY", "PUSH_PROVIDER", "FCM_PROJECT_ID", "FIREBASE_CREDENTIALS"} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func valid() map[string]string {
	return map[string]string{
		"POSTGRES_URL": "postgres://x", "REDIS_URL": "redis://x", "MONGO_URL": "mongodb://x",
		"JWT_SECRET": strings.Repeat("a", 40), "ENV": "development",
	}
}

// prodOK is a production setup that passes every check. A CSAM adapter is
// pretended to exist so the other rules can be tested on their own.
func prodOK(t *testing.T) map[string]string {
	t.Helper()
	old := ImplementedCSAMProviders
	ImplementedCSAMProviders = []string{"photodna"}
	t.Cleanup(func() { ImplementedCSAMProviders = old })
	m := valid()
	m["ENV"] = "production"
	m["APP_ALLOWED_ORIGINS"] = "https://app.example.com"
	m["STRIPE_SECRET_KEY"], m["STRIPE_WEBHOOK_SECRET"] = "sk_live_x", "whsec_x"
	m["SMTP_HOST"], m["SMTP_FROM"] = "smtp.example.com", "ConnectMe <no-reply@example.com>"
	m["AI_SERVICE_URL"], m["AI_SERVICE_KEY"] = "http://ai-service:8000", "k"
	m["CSAM_PROVIDER"] = "photodna"
	return m
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
		m["AI_SERVICE_URL"], m["AI_SERVICE_KEY"] = "http://ai-service:8000", "k"
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
	m["AI_SERVICE_URL"], m["AI_SERVICE_KEY"] = "http://ai-service:8000", "k"
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
		m["AI_SERVICE_URL"], m["AI_SERVICE_KEY"] = "http://ai-service:8000", "k"
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

func TestUnsetENVIsProduction(t *testing.T) {
	for env, want := range map[string]bool{"": true, "production": true, "staging": true, "prod": true, "Production": true,
		"development": false, "Development": false, "test": false, " test ": false} {
		if got := (Config{Env: env}).IsProduction(); got != want {
			t.Errorf("ENV=%q: IsProduction=%v, want %v", env, got, want)
		}
	}
	m := valid()
	delete(m, "ENV")
	setEnv(t, m)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "CSAM_PROVIDER") {
		t.Fatalf("an unset ENV must run the production checks, got %v", err)
	}
}

func TestProductionSafetyRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(m map[string]string)
		want   string // substring of the error; "" = must load
	}{
		{"everything configured", func(m map[string]string) {}, ""},
		{"csam provider empty", func(m map[string]string) { m["CSAM_PROVIDER"] = "" }, "CSAM_PROVIDER must be a real provider"},
		{"csam provider stub", func(m map[string]string) { m["CSAM_PROVIDER"] = "stub" }, "CSAM_PROVIDER must be a real provider"},
		{"csam provider STUB", func(m map[string]string) { m["CSAM_PROVIDER"] = "STUB" }, "CSAM_PROVIDER must be a real provider"},
		{"csam provider without an adapter", func(m map[string]string) { m["CSAM_PROVIDER"] = "somevendor" }, "no working adapter"},
		{"ai url missing", func(m map[string]string) { delete(m, "AI_SERVICE_URL") }, "AI_SERVICE_URL is required"},
		{"ai key missing", func(m map[string]string) { delete(m, "AI_SERVICE_KEY") }, "AI_SERVICE_KEY is required"},
		{"dev bypass on", func(m map[string]string) { m["MODERATION_DEV_BYPASS"] = "true" }, "MODERATION_DEV_BYPASS must not be set"},
		{"jwt secret placeholder", func(m map[string]string) { m["JWT_SECRET"] = "change-me" + strings.Repeat("x", 40) }, "JWT_SECRET"},
		{"jwt secret short", func(m map[string]string) { m["JWT_SECRET"] = "short" }, "JWT_SECRET"},
		{"ENV unset", func(m map[string]string) { delete(m, "ENV"); delete(m, "CSAM_PROVIDER") }, "CSAM_PROVIDER"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := prodOK(t)
			c.mutate(m)
			setEnv(t, m)
			_, err := Load()
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("rejected: %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("want an error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestAllProblemsAreReportedTogether(t *testing.T) {
	setEnv(t, map[string]string{"ENV": "production", "POSTGRES_URL": "x", "REDIS_URL": "x", "MONGO_URL": "x", "JWT_SECRET": "short"})
	_, err := Load()
	if err == nil {
		t.Fatal("an empty production setup must not load")
	}
	for _, need := range []string{"JWT_SECRET", "AI_SERVICE_URL", "AI_SERVICE_KEY", "CSAM_PROVIDER", "STRIPE_SECRET_KEY", "SMTP_HOST", "APP_ALLOWED_ORIGINS"} {
		if !strings.Contains(err.Error(), need) {
			t.Errorf("the list of problems is missing %s: %v", need, err)
		}
	}
}

func TestDevelopmentKeepsItsConveniences(t *testing.T) {
	m := valid()
	m["MODERATION_DEV_BYPASS"] = "true"
	setEnv(t, m)
	c, err := Load()
	if err != nil {
		t.Fatalf("development rejected: %v", err)
	}
	if c.CSAMProvider != "stub" || c.AIServiceURL == "" || !c.ModerationDevBypass {
		t.Errorf("development defaults: csam=%q ai=%q bypass=%v", c.CSAMProvider, c.AIServiceURL, c.ModerationDevBypass)
	}
	if c.AITextTimeoutMS != 2000 || c.AIImageTimeoutMS != 6000 {
		t.Errorf("default timeouts: %d / %d", c.AITextTimeoutMS, c.AIImageTimeoutMS)
	}
	m["AI_TEXT_TIMEOUT_MS"], m["AI_IMAGE_TIMEOUT_MS"] = "500", "9000"
	setEnv(t, m)
	c, _ = Load()
	if c.AITextTimeoutMS != 500 || c.AIImageTimeoutMS != 9000 {
		t.Errorf("configured timeouts: %d / %d", c.AITextTimeoutMS, c.AIImageTimeoutMS)
	}
}
