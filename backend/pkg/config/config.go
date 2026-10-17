// Package config loads and validates process configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"connectme/pkg/middleware"

	"github.com/joho/godotenv"
)

const minJWTSecretBytes = 32

type Config struct {
	Env                 string
	Port                string
	AllowedOrigins      []string
	PostgresURL         string
	RedisURL            string
	MongoURL            string
	JWTSecret           string
	JWTAccessExpiry     time.Duration
	JWTRefreshExpiry    time.Duration
	StripeSecretKey     string
	StripeWebhookSecret string
	AIServiceURL        string
	AIServiceKey        string
	CSAMProvider        string
	MediaDir            string
	StripeAPIBase       string
	CheckoutSuccessURL  string
	CheckoutCancelURL   string
	SMTPHost            string // empty = emails are only logged (development)
	SMTPPort            int
	SMTPUsername        string
	SMTPPassword        string
	SMTPFrom            string
	SMTPAllowPlaintext  bool   // local test relays only
	S3Endpoint          string // empty = media stays on local disk (MEDIA_DIR)
	S3Bucket            string
	S3AccessKey         string
	S3SecretKey         string
	S3Region            string
	S3UseSSL            bool
	S3SSE               bool
	PushProvider        string // stub | fcm
	FCMProjectID        string
	FirebaseCredentials string // path to a service-account JSON key
	EnableVideo         bool   // short videos and profile reels
	VideoScanner        string // stub | http; production needs http
	VideoScannerURL     string
	VideoScannerKey     string
	LiveKitURL          string // empty disables live streaming
	LiveKitAPIKey       string
	LiveKitAPISecret    string
	MetricsAddr         string   // private listener for /metrics; empty disables
	TrustedProxies      []string // CIDRs/IPs of our load balancers (X-Forwarded-For is honoured only from these)
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load reads .env files (if present) then the process environment, and fails
// fast with every problem listed so misconfiguration is fixed in one pass.
func Load() (Config, error) {
	// Optional files; real environment variables always win. Loaded one at a
	// time because godotenv.Load stops at the first missing file.
	for _, f := range []string{".env", "../.env"} {
		_ = godotenv.Load(f)
	}

	c := Config{
		Env:                 get("ENV", "development"),
		Port:                get("PORT", "8080"),
		PostgresURL:         os.Getenv("POSTGRES_URL"),
		RedisURL:            os.Getenv("REDIS_URL"),
		MongoURL:            os.Getenv("MONGO_URL"),
		JWTSecret:           os.Getenv("JWT_SECRET"),
		StripeSecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		AIServiceURL:        get("AI_SERVICE_URL", "http://localhost:8000"),
		AIServiceKey:        os.Getenv("AI_SERVICE_KEY"),
		CSAMProvider:        get("CSAM_PROVIDER", "stub"),
		MetricsAddr:         get("METRICS_ADDR", "127.0.0.1:9464"),
		SMTPHost:            os.Getenv("SMTP_HOST"),
		SMTPPort:            getInt("SMTP_PORT", 587),
		SMTPUsername:        os.Getenv("SMTP_USERNAME"),
		SMTPPassword:        os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:            os.Getenv("SMTP_FROM"),
		SMTPAllowPlaintext:  os.Getenv("SMTP_ALLOW_PLAINTEXT") == "true",
		S3Endpoint:          os.Getenv("S3_ENDPOINT"),
		S3Bucket:            os.Getenv("S3_BUCKET"),
		S3AccessKey:         os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:         os.Getenv("S3_SECRET_KEY"),
		S3Region:            os.Getenv("S3_REGION"),
		S3UseSSL:            get("S3_USE_SSL", "true") != "false",
		S3SSE:               os.Getenv("S3_SSE") == "true",
		PushProvider:        get("PUSH_PROVIDER", "stub"),
		FCMProjectID:        os.Getenv("FCM_PROJECT_ID"),
		FirebaseCredentials: os.Getenv("FIREBASE_CREDENTIALS"),
		EnableVideo:         os.Getenv("ENABLE_VIDEO") == "true",
		VideoScanner:        get("VIDEO_SCANNER", "stub"),
		VideoScannerURL:     os.Getenv("VIDEO_SCANNER_URL"),
		VideoScannerKey:     os.Getenv("VIDEO_SCANNER_KEY"),
		LiveKitURL:          os.Getenv("LIVEKIT_URL"),
		LiveKitAPIKey:       os.Getenv("LIVEKIT_API_KEY"),
		LiveKitAPISecret:    os.Getenv("LIVEKIT_API_SECRET"),
		MediaDir:            get("MEDIA_DIR", "./data/media"),
		StripeAPIBase:       get("STRIPE_API_BASE", "https://api.stripe.com"),
		CheckoutSuccessURL:  get("CHECKOUT_SUCCESS_URL", "connectme://payments/success"),
		CheckoutCancelURL:   get("CHECKOUT_CANCEL_URL", "connectme://payments/cancel"),
	}
	for _, p := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			c.TrustedProxies = append(c.TrustedProxies, p)
		}
	}
	for _, o := range strings.Split(os.Getenv("APP_ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, o)
		}
	}

	var errs []error
	var err error
	if c.JWTAccessExpiry, err = duration("JWT_ACCESS_EXPIRY", "15m"); err != nil {
		errs = append(errs, err)
	}
	if c.JWTRefreshExpiry, err = duration("JWT_REFRESH_EXPIRY", "168h"); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, c.validate()...)
	return c, errors.Join(errs...)
}

func (c Config) validate() []error {
	var errs []error
	for name, v := range map[string]string{
		"POSTGRES_URL": c.PostgresURL,
		"REDIS_URL":    c.RedisURL,
		"MONGO_URL":    c.MongoURL,
	} {
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
	}
	switch {
	case len(c.JWTSecret) < minJWTSecretBytes:
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d bytes", minJWTSecretBytes))
	case strings.HasPrefix(strings.ToLower(c.JWTSecret), "change-me"):
		errs = append(errs, errors.New("JWT_SECRET is still the placeholder value"))
	}
	if c.IsProduction() {
		if c.StripeSecretKey == "" || c.StripeWebhookSecret == "" {
			errs = append(errs, errors.New("STRIPE_SECRET_KEY and STRIPE_WEBHOOK_SECRET are required in production"))
		}
		if len(c.AllowedOrigins) == 0 {
			errs = append(errs, errors.New("APP_ALLOWED_ORIGINS is required in production"))
		}
		if c.AIServiceURL != "" && c.AIServiceKey == "" {
			errs = append(errs, errors.New("AI_SERVICE_KEY is required in production when AI_SERVICE_URL is set"))
		}
		if c.SMTPHost == "" || c.SMTPFrom == "" {
			errs = append(errs, errors.New("SMTP_HOST and SMTP_FROM are required in production (email verification and password reset need real delivery)"))
		}
		if c.SMTPAllowPlaintext {
			errs = append(errs, errors.New("SMTP_ALLOW_PLAINTEXT must not be set in production"))
		}
		if c.CSAMProvider == "" || c.CSAMProvider == "stub" {
			errs = append(errs, errors.New("CSAM_PROVIDER must be a real provider in production (stub accepts everything)"))
		}
	}
	switch c.PushProvider {
	case "stub":
	case "fcm":
		if c.FCMProjectID == "" || c.FirebaseCredentials == "" {
			errs = append(errs, errors.New("PUSH_PROVIDER=fcm needs FCM_PROJECT_ID and FIREBASE_CREDENTIALS (path to the service-account key)"))
		}
	default:
		errs = append(errs, errors.New("PUSH_PROVIDER must be stub or fcm"))
	}
	if c.EnableVideo {
		switch c.VideoScanner {
		case "stub":
			if c.IsProduction() {
				errs = append(errs, errors.New("ENABLE_VIDEO in production needs VIDEO_SCANNER=http (the stub allows everything)"))
			}
		case "http":
			if c.VideoScannerURL == "" || c.VideoScannerKey == "" {
				errs = append(errs, errors.New("VIDEO_SCANNER=http needs VIDEO_SCANNER_URL and VIDEO_SCANNER_KEY"))
			} else if c.IsProduction() && !strings.HasPrefix(c.VideoScannerURL, "https://") {
				errs = append(errs, errors.New("VIDEO_SCANNER_URL must be https in production"))
			}
		default:
			errs = append(errs, errors.New("VIDEO_SCANNER must be stub or http"))
		}
	}
	if c.S3Endpoint != "" && (c.S3Bucket == "" || c.S3AccessKey == "" || c.S3SecretKey == "") {
		errs = append(errs, errors.New("S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY are required when S3_ENDPOINT is set"))
	}
	if c.IsProduction() && c.S3Endpoint != "" && !c.S3UseSSL {
		errs = append(errs, errors.New("S3_USE_SSL must not be false in production"))
	}
	if c.LiveKitURL != "" && (c.LiveKitAPIKey == "" || len(c.LiveKitAPISecret) < minJWTSecretBytes) {
		errs = append(errs, errors.New("LIVEKIT_API_KEY and a LIVEKIT_API_SECRET of at least 32 bytes are required when LIVEKIT_URL is set"))
	}
	for _, p := range c.TrustedProxies {
		if _, err := middleware.ParseCIDRs([]string{p}); err != nil {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXY_CIDRS: invalid entry %q", p))
		}
	}
	if c.JWTAccessExpiry > time.Hour {
		errs = append(errs, errors.New("JWT_ACCESS_EXPIRY must be <= 1h"))
	}
	return errs
}

func get(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func duration(key, def string) (time.Duration, error) {
	d, err := time.ParseDuration(get(key, def))
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return d, nil
}
