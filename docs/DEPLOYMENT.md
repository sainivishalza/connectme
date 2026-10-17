# Deployment checklist

Status key: [x] done and verified in this repo, [ ] you must do it, [~] partly done.

## Build artefacts

- [x] `backend/Dockerfile`: static Go binary on distroless, non-root
- [x] `realtime/Dockerfile`: Node 22 alpine, non-root, production deps only (built and tagged locally)
- [x] `ai-service/Dockerfile`: Python 3.12 slim, non-root
- [x] `docker-compose.yml`: PostGIS, Redis, MongoDB, AI service for local use (ports bound to 127.0.0.1)
- [ ] Push images to your registry with immutable tags; scan them (Trivy/Grype)
- [ ] Pin base images by digest

## Configuration (all via environment, see `.env.example`)

- [ ] Strong, unique values: `POSTGRES_PASSWORD`, `REDIS_PASSWORD`, `MONGO_PASSWORD`, `JWT_SECRET` (>= 32 bytes), `AI_SERVICE_KEY`
- [ ] `ENV=production` (turns on: Stripe keys required, allowed origins required, stub CSAM provider refused, mailer never logs tokens)
- [ ] `APP_ALLOWED_ORIGINS` set to your web origins
- [ ] **`TRUSTED_PROXY_CIDRS` set to your load balancer / ingress range.** Without it, every user behind the proxy shares one per-IP rate limit
- [ ] `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET` (live keys only in production secrets)
- [ ] `CSAM_PROVIDER` set to a real provider (the server will not start with `stub`)
- [ ] `CHECKOUT_SUCCESS_URL` / `CHECKOUT_CANCEL_URL` (deep links or HTTPS)
- [ ] Secrets in a secret manager (not in images, not in git); rotate on a schedule

## Kubernetes (manifests are **not** provided or tested; this is what to create)

- [ ] Deployments: `api` (>= 2 replicas), `realtime` (>= 2, Redis adapter already in place), `ai-service` (>= 2, ClusterIP only)
- [ ] `AI_SERVICE_URL` points at the in-cluster service; NetworkPolicy so only `api` may call it
- [ ] Ingress with TLS (>= 1.2), HSTS, request-size limits <= 12 MiB, websocket support for `/socket.io`
- [ ] Liveness `GET /healthz`, readiness `GET /readyz` (checks Postgres, Redis, MongoDB)
- [ ] Resource requests/limits, PodDisruptionBudget, HPA on CPU and request rate
- [ ] SecurityContext: `runAsNonRoot`, read-only root FS, drop all capabilities
- [ ] Managed PostgreSQL **with PostGIS** (extensions `postgis`, `pgcrypto`), PITR backups, tested restore
- [ ] Managed Redis with auth + TLS (rate limits and refresh sessions live here; use persistence/HA)
- [ ] Managed MongoDB with auth + TLS (ciphertext only)
- [ ] Object storage for media implementing `users.MediaStore` (local disk is dev-only); server-side encryption
- [ ] Migrations run once per release before rollout (the server applies them under an advisory lock at start)

## Background work

- [x] Hotel retention + chat sweeper + burn job run inside the API, idempotent, safe on multiple replicas
- [x] Alert if they stop: `connectme_job_last_success_timestamp_seconds` with `RetentionJobStalled` / `ChatRetentionStalled` in `ops/alerts.yml`
- [ ] Optional: move jobs to a dedicated worker deployment if API replicas scale a lot

## Observability

- [ ] Central structured logging (JSON), retention policy, PII review (the code logs no tokens in production)
- [x] Metrics: served only on the private `METRICS_ADDR` listener (default `127.0.0.1:9464`, never on the public port; in k8s expose it only to the Prometheus namespace via NetworkPolicy). Scrape config `ops/prometheus.yml`, alerts `ops/alerts.yml`
- [x] Grafana dashboard: import `ops/grafana/connectme-api.json` (traffic, latency, 5xx, rate limits, job freshness, AI breaker, moderation, Stripe)
- [ ] Error tracking (Sentry) for API, realtime, mobile
- [ ] Uptime checks on `/healthz` and a synthetic login/swipe/chat flow

## Before opening to the public

- [ ] Independent penetration test; fix findings
- [ ] Legal review per market (GDPR, CCPA, PIPL, age assurance, E2E vs. scanning, app-store dating-app rules)
- [ ] Privacy policy and terms: drafts in `docs/legal/` need legal review and filled placeholders; data-processing agreements (Stripe, hosting, email, CSAM vendor) still to sign
- [ ] 24/7 moderation process and a moderator tool (dashboard at `/admin/`; restrict it by network/VPN as defence in depth)
- [ ] Email: set `SMTP_*` for your relay (implemented, required in production) and configure SPF, DKIM and DMARC for the sending domain; bounce/complaint handling is not built
- [ ] Media: set `S3_*` for an object store (private bucket, no public ACLs, versioning/lifecycle per your retention policy, `S3_USE_SSL=true`); otherwise run a single API replica on a persistent disk or a ReadWriteMany volume
- [ ] Real CSAM + ID/liveness providers
- [ ] In-app purchase decision for iOS/Android
- [ ] Load test at target scale; tune pools and indexes with production-like data
- [x] Incident response runbook and key-rotation plan drafted: `docs/RUNBOOK.md` (not yet exercised in a drill). Note: rotating `JWT_SECRET` invalidates access tokens only; refresh tokens live in Redis and must be deleted to force a full sign-out
- [ ] Android/iOS signing, store listings, privacy labels / data-safety forms

## Live streaming (LiveKit)

- [ ] Run a LiveKit server (or LiveKit Cloud) reachable by clients over `wss://`; set `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` (secret >= 32 bytes) on the API. With `LIVEKIT_URL` empty the `/live` routes are not mounted.
- [ ] Open the server's WebRTC ports (UDP/TCP) and configure TURN for clients behind strict NATs; this is outside the API and is **untested here**.
- [ ] Size for audience: chat and gift events are polled every 2 s, so each viewer costs about 0.5 req/s (see `docs/AUDIT.md` §3.7). Move to a push channel before very large rooms.
- [ ] Confirm camera/microphone permissions and playback on physical Android and iOS devices.
