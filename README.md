# ConnectMe

A privacy-first dating app with two identities, **Open** and **Incognito**, plus anonymous same-hotel discovery that
auto-deletes, group dates, end-to-end encrypted chat, live streaming, and coins/subscriptions.

> Status: Phase 1 plus the moderator dashboard, observability, live streaming and ops scaffolding are built and tested locally. It is **not launch-ready**: see `docs/AUDIT.md` (what is verified) and
> `docs/DEPLOYMENT.md` (what you still have to do).

## Architecture

| Part | Tech | Folder |
|---|---|---|
| Mobile app | Flutter, Riverpod, GoRouter, libsignal | `mobile/` |
| API | Go, chi, pgx | `backend/` |
| Real-time relay | Node.js, Socket.io (+ Redis adapter) | `realtime/` |
| AI service | Python, FastAPI (scoring, moderation, wingman) | `ai-service/` |
| Data | PostgreSQL + PostGIS, Redis, MongoDB (ciphertext only) | `docker-compose.yml` |

Key ideas (details and trade-offs in `docs/DECISIONS.md`):

- **Dual-mode identity.** Every outward profile goes through one projection; anonymous cards contain no name, photo, age or coordinates, and the opaque profile ref rotates when the mode changes.
- **Hotel mode.** Check-in is verified against a geofence server-side; everything from a stay (matches, chats, gifts, posts) is bound to the check-in and deleted 24 h after checkout.
- **E2E chat.** The server stores and relays Signal ciphertext only; history lives on the device.
- **AI as an enhancement.** Timeouts, a circuit breaker, minimal inputs; the product works with it down.

## Run locally

```bash
cp .env.example .env            # then fill in the passwords and JWT_SECRET (>= 32 bytes) and AI_SERVICE_KEY
docker compose up -d            # PostGIS, Redis, MongoDB, AI service
# optional live streaming: docker run -d -p 7880:7880 -e LIVEKIT_KEYS="devkey: <32+ byte secret>" livekit/livekit-server --bind 0.0.0.0
#   then set LIVEKIT_URL=ws://127.0.0.1:7880, LIVEKIT_API_KEY=devkey, LIVEKIT_API_SECRET=<secret> in .env

cd backend  && go run ./cmd/server            # API on :8080 (applies migrations)
cd realtime && npm ci && npm start            # relay on :3001
cd mobile   && flutter run --dart-define=API_BASE=http://10.0.2.2:8080 --dart-define=REALTIME_BASE=http://10.0.2.2:3001
```

## Test

```bash
# Go: needs the databases from docker compose
export TEST_POSTGRES_URL=... TEST_REDIS_URL=... TEST_MONGO_URL=...
cd backend && go test ./...

cd ai-service && python -m venv .venv && .venv/bin/pip install -r requirements.txt && .venv/bin/pytest
cd realtime  && npm test
cd mobile    && flutter analyze && flutter test
```

## Documentation

| File | What |
|---|---|
| `docs/AUDIT.md` | Security & readiness audit: checklist, OWASP API Top 10, defects found, limitations, blockers |
| `docs/DEPLOYMENT.md` | Docker/Kubernetes/ops checklist |
| `docs/DECISIONS.md` | Every deviation from the original spec and why |
| `docs/FEATURES.md` | Every PRD feature: built, partial or not started |
| `docs/ROUTES.md` | API surface, generated from the live router |
| `docs/RUNBOOK.md` | Incident response, key rotation, backups |
| `docs/legal/` | Draft privacy policy and terms (need legal review) |
| `ops/` | Prometheus config, alert rules, Grafana dashboard, Kubernetes manifests |
| `prompts/` | The original specification files |
| `mobile/README.md` | Mobile specifics |

Feature-by-feature status against the PRD is in `docs/FEATURES.md`.

## Not in this build

The AI Wingman as a full agent (needs an LLM provider), payouts to partners (booking payments are collected, but settlement is manual), a video-scanner provider (the interface and review queue are built; production refuses videos without a real scanner), the Firebase plugin in the app (push is built on the server and behind an app interface, but never run against Firebase), real CSAM/ID-verification providers, Android/iOS release builds, tracing and error tracking.
Live streaming (with chat, gifts and host/staff moderation), the moderator dashboard (`/admin/`), metrics/alerts/Grafana (`ops/`) and
Kubernetes manifests (`ops/k8s`) are built and tested locally, but the camera path has not run on a device and the manifests have not
been applied to a cluster. Privacy policy and terms are drafts (`docs/legal/`). See `docs/AUDIT.md` §3-4.
