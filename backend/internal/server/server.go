// Package server wires configuration, connectors and routes into an http.Server.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"connectme/internal/admin"
	"connectme/internal/aiclient"
	"connectme/internal/audio"
	"connectme/internal/auth"
	"connectme/internal/calls"
	"connectme/internal/chapters"
	"connectme/internal/chat"
	"connectme/internal/communities"
	"connectme/internal/forums"
	"connectme/internal/games"
	"connectme/internal/geo"
	"connectme/internal/groups"
	"connectme/internal/live"
	"connectme/internal/matching"
	"connectme/internal/moderation"
	"connectme/internal/partners"
	"connectme/internal/payments"
	"connectme/internal/portal"
	"connectme/internal/privacy"
	"connectme/internal/push"
	"connectme/internal/random"
	"connectme/internal/social"
	"connectme/internal/users"
	"connectme/internal/videos"
	"connectme/pkg/config"
	"connectme/pkg/database"
	"connectme/pkg/metrics"
	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Deps are the connected backing services shared by all feature modules.
type Deps struct {
	Cfg   config.Config
	PG    *pgxpool.Pool
	Redis *database.Redis
	Mongo *database.Mongo
	Push  push.Provider // optional override (tests); otherwise built from Cfg
}

func New(d Deps) *http.Server {
	r := chi.NewRouter()
	// Body-size caps are applied per route group (default 1 MiB), not globally,
	// because upload routes need a larger limit and nested caps cannot raise it.
	r.Use(metrics.HTTP, chimw.Recoverer, middleware.SecurityHeaders, middleware.CORS(d.Cfg.AllowedOrigins),
		chimw.Timeout(30*time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", d.ready)

	if nets, err := middleware.ParseCIDRs(d.Cfg.TrustedProxies); err == nil {
		middleware.SetTrustedProxies(nets)
	}
	metrics.RegisterPool(d.PG)
	secret := []byte(d.Cfg.JWTSecret)
	tokens := auth.NewTokenStore(d.Redis.Client, d.Cfg.JWTRefreshExpiry)
	var mailer auth.Mailer = auth.LogMailer{Production: d.Cfg.IsProduction()}
	if d.Cfg.SMTPHost != "" {
		sm, err := auth.NewSMTPMailer(auth.SMTPConfig{Host: d.Cfg.SMTPHost, Port: d.Cfg.SMTPPort,
			Username: d.Cfg.SMTPUsername, Password: d.Cfg.SMTPPassword, From: d.Cfg.SMTPFrom,
			AllowPlaintext: d.Cfg.SMTPAllowPlaintext})
		if err != nil {
			panic("smtp: " + err.Error()) // misconfiguration must stop startup, not silently drop emails
		}
		mailer = sm
	}
	svc := auth.NewService(d.PG, d.Redis.Client, tokens, mailer, secret, d.Cfg.JWTAccessExpiry)
	ver := &auth.VerificationHandler{DB: d.PG, CSAM: auth.StubCSAMScanner{}, Liveness: auth.StubLiveness{}}
	r.Mount("/auth", auth.Routes(svc, d.Redis.Client, secret, ver))

	usersSvc := newUsers(d)
	pushSvc := newPush(d)
	middleware.SetSessionCutoff(tokens.SessionCutoff)
	r.Mount("/push", push.Routes(pushSvc, d.Redis.Client, secret))
	r.Mount("/users", users.Routes(usersSvc, d.Redis.Client, secret))
	r.Mount("/media", users.MediaRoutes(usersSvc, d.Redis.Client, secret))

	chatSvc := NewChat(d)
	if chatSvc != nil {
		chatSvc.SetNotifier(pushSvc)
	}
	matchSvc := matching.NewService(d.PG, usersSvc)
	matchSvc.SetNotifier(pushSvc)
	if ai := newAI(d); ai != nil {
		var hist matching.HistoryProvider
		if chatSvc != nil {
			hist = chatSvc
		}
		matchSvc.SetScorer(aiAdapter{ai}, hist)
	}
	r.Mount("/matching", matching.Routes(matchSvc, d.Redis.Client, secret))
	r.Mount("/geo", geo.Routes(NewGeo(d), d.Redis.Client, secret))
	if chatSvc != nil {
		r.Mount("/chat", chat.Routes(chatSvc, d.Redis.Client, secret))
	}
	var stripeClient payments.Stripe
	if d.Cfg.StripeSecretKey != "" {
		stripeClient = payments.NewHTTPStripe(d.Cfg.StripeSecretKey, d.Cfg.StripeAPIBase)
	}
	paySvc := payments.NewService(d.PG, usersSvc, stripeClient, payments.Config{
		WebhookSecret: d.Cfg.StripeWebhookSecret, SuccessURL: d.Cfg.CheckoutSuccessURL, CancelURL: d.Cfg.CheckoutCancelURL})
	r.Mount("/payments", payments.Routes(paySvc, d.Redis.Client, secret))
	r.Mount("/groups", groups.Routes(groups.NewService(d.PG, usersSvc, groups.NewHTTPSuggester(d.Cfg.AIServiceURL, d.Cfg.AIServiceKey)),
		d.Redis.Client, secret))
	var liveSvc *live.Service
	var callSvc *calls.Service
	var audioSvc *audio.Service
	if d.Cfg.LiveKitURL != "" {
		liveSvc = live.NewService(d.PG, live.LiveKit{
			WSURL: d.Cfg.LiveKitURL, APIKey: d.Cfg.LiveKitAPIKey, APISecret: d.Cfg.LiveKitAPISecret})
		liveSvc.SetVetter(usersSvc)
		r.Mount("/live", live.Routes(liveSvc, d.Redis.Client, secret))
		callSvc = calls.NewService(d.PG, usersSvc, live.LiveKit{
			WSURL: d.Cfg.LiveKitURL, APIKey: d.Cfg.LiveKitAPIKey, APISecret: d.Cfg.LiveKitAPISecret})
		callSvc.SetNotifier(pushSvc)
		r.Mount("/calls", calls.Routes(callSvc, d.Redis.Client, secret))
		audioSvc = audio.NewService(d.PG, live.LiveKit{
			WSURL: d.Cfg.LiveKitURL, APIKey: d.Cfg.LiveKitAPIKey, APISecret: d.Cfg.LiveKitAPISecret}, usersSvc)
		r.Mount("/audio", audio.Routes(audioSvc, d.Redis.Client, secret))
	}
	r.Mount("/chapters", chapters.Routes(chapters.NewService(d.PG, usersSvc), d.Redis.Client, secret))
	partnerSvc := partners.NewService(d.PG, usersSvc)
	partnerSvc.SetNotifier(pushSvc)
	r.Mount("/partners", partners.Routes(partnerSvc, d.Redis.Client, secret))
	forumSvc := forums.NewService(d.PG, usersSvc, secret)
	r.Mount("/forums", forums.Routes(forumSvc, d.Redis.Client, secret))
	var videoSvc *videos.Service
	if d.Cfg.EnableVideo {
		videoSvc = videos.NewService(d.PG, newMedia(d, "video"), usersSvc, secret)
		if d.Cfg.VideoScanner == "http" {
			videoSvc.SetScanner(videos.HTTPScanner{URL: d.Cfg.VideoScannerURL, Key: d.Cfg.VideoScannerKey})
		}
		r.Mount("/videos", videos.Routes(videoSvc, d.Redis.Client, secret))
	}
	socialSvc := social.NewService(d.PG, usersSvc)
	socialSvc.SetNotifier(pushSvc)
	r.Mount("/social", social.Routes(socialSvc, d.Redis.Client, secret))
	r.Mount("/communities", communities.Routes(communities.NewService(d.PG, usersSvc), socialSvc, d.Redis.Client, secret))
	r.Mount("/games", games.Routes(games.NewService(d.PG), d.Redis.Client, secret))
	randomSvc := random.NewService(d.PG, usersSvc)
	randomSvc.SetNotifier(pushSvc)
	r.Mount("/random", random.Routes(randomSvc, d.Redis.Client, secret))
	var chatPurger privacy.ChatPurger
	if chatSvc != nil {
		chatPurger = chatSvc
	}
	var billing privacy.SubscriptionCanceller
	if stripeClient != nil {
		billing = stripeClient
	}
	r.Mount("/admin", http.StripPrefix("", admin.Handler()))
	r.Mount("/partner", http.StripPrefix("", portal.Handler()))
	privacySvc := privacy.NewService(d.PG, tokens, usersSvc, chatPurger, billing)
	if videoSvc != nil {
		privacySvc.AddFilePurger(videoSvc)
	}
	r.Mount("/privacy", privacy.Routes(privacySvc, d.Redis.Client, secret))
	modSvc := moderation.NewService(d.PG, usersSvc, tokens)
	if liveSvc != nil {
		modSvc.SetStreams(liveSvc)
	}
	if callSvc != nil {
		modSvc.SetCalls(callSvc)
	}
	if audioSvc != nil {
		modSvc.SetAudio(audioSvc)
	}
	if videoSvc != nil {
		modSvc.SetVideos(videoSvc)
	}
	modSvc.SetForums(forumSvc)
	r.Mount("/moderation", moderation.Routes(modSvc, d.Redis.Client, secret))

	// Later steps mount their routers inside this group (1 MiB body cap).
	r.Group(func(r chi.Router) {
		r.Use(middleware.MaxBody(1 << 20))
	})

	return &http.Server{
		Addr:              ":" + d.Cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func (d Deps) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	checks := map[string]error{
		"postgres": database.PingPostgres(ctx, d.PG),
		"redis":    d.Redis.PingTimeout(ctx),
	}
	if d.Mongo != nil {
		checks["mongo"] = d.Mongo.Ping(ctx)
	}
	status, out := http.StatusOK, map[string]string{}
	for name, err := range checks {
		out[name] = "ok"
		if err != nil {
			out[name], status = "down", http.StatusServiceUnavailable
		}
	}
	middleware.WriteJSON(w, status, out)
}

// NewGeo builds the geo service; later steps pass extra Purgers (e.g. chat).
func NewGeo(d Deps, purgers ...geo.Purger) *geo.Service {
	return geo.NewService(d.PG, newUsers(d), purgers...)
}

// NewChat builds the chat service, or nil when MongoDB is not configured.
func NewChat(d Deps) *chat.Service {
	if d.Mongo == nil {
		return nil
	}
	return chat.NewService(d.PG, d.Mongo.DB, newMedia(d, "chat"))
}

// StartJobs launches background workers; they stop when ctx is cancelled.
func StartJobs(ctx context.Context, d Deps) {
	var purgers []geo.Purger
	if c := NewChat(d); c != nil {
		if err := c.EnsureIndexes(ctx); err != nil {
			slog.Error("chat: ensure indexes", "err", err)
		}
		c.StartJob(ctx, time.Minute, func(err error) { slog.Error("chat: retention job", "err", err) })
		purgers = append(purgers, c) // hotel retention also wipes chat for the stay
	}
	NewGeo(d, purgers...).StartJob(ctx, time.Minute)
	random.NewService(d.PG, newUsers(d)).StartJob(ctx, 15*time.Second)
	social.NewService(d.PG, newUsers(d)).StartJob(ctx, 5*time.Minute)
}

// newAI returns the AI client, or nil when no AI service is configured.
func newAI(d Deps) *aiclient.Client {
	if d.Cfg.AIServiceURL == "" {
		return nil
	}
	return aiclient.New(d.Cfg.AIServiceURL, d.Cfg.AIServiceKey)
}

// newMedia returns the blob store for a namespace: an S3-compatible bucket
// when S3_ENDPOINT is set, otherwise local disk under MEDIA_DIR. A bad S3
// configuration stops startup rather than silently falling back to disk.
func newMedia(d Deps, ns string) users.MediaStore {
	if d.Cfg.S3Endpoint == "" {
		if ns == "photos" {
			return users.LocalStore{Dir: d.Cfg.MediaDir}
		}
		return users.LocalStore{Dir: filepath.Join(d.Cfg.MediaDir, ns)}
	}
	st, err := users.NewS3Store(users.S3Config{Endpoint: d.Cfg.S3Endpoint, Bucket: d.Cfg.S3Bucket,
		AccessKey: d.Cfg.S3AccessKey, SecretKey: d.Cfg.S3SecretKey, Region: d.Cfg.S3Region,
		UseSSL: d.Cfg.S3UseSSL, Prefix: ns, SSE: d.Cfg.S3SSE})
	if err != nil {
		panic("s3: " + err.Error())
	}
	return st
}

// newUsers builds the users service with content moderation attached.
func newUsers(d Deps) *users.Service {
	u := users.NewService(d.PG, newMedia(d, "photos"), auth.StubCSAMScanner{})
	if ai := newAI(d); ai != nil {
		u.SetChecker(aiAdapter{ai})
	}
	return u
}

// ServeMetrics exposes Prometheus metrics on a PRIVATE address (default
// 127.0.0.1:9464). It is a separate listener so /metrics can never be
// reached through the public API; in a cluster, bind the pod IP and restrict
// it with a NetworkPolicy.
func ServeMetrics(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics listener stopped", "addr", addr, "err", err)
		}
	}()
}

// newPush builds the push service from configuration and starts its delivery
// workers. With PUSH_PROVIDER=stub (the default) nothing leaves the process.
func newPush(d Deps) *push.Service {
	var p push.Provider = &push.Stub{}
	if d.Push != nil {
		p = d.Push // tests inject their own
	} else if d.Cfg.PushProvider == "fcm" {
		raw, err := os.ReadFile(d.Cfg.FirebaseCredentials)
		if err == nil {
			var f *push.FCM
			if f, err = push.NewFCM(d.Cfg.FCMProjectID, raw); err == nil {
				p = f
			}
		}
		if err != nil {
			// A bad key must stop the server, not silently drop every notification.
			slog.Error("push: FCM is configured but unusable", "err", err)
			panic("push: " + err.Error())
		}
	}
	svc := push.NewService(d.PG, d.Redis.Client, p)
	svc.Start(context.Background())
	return svc
}
