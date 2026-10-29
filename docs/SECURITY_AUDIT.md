# Security audit (Phase 1, read-only)

Branch `security-hardening`, cut from `safety-fail-closed` (the latest code; `main` is missing the purge and the safety fixes, so auditing it would audit code that is no longer shipped).
**No application code was changed in this phase.** The only additions are this report, `docs/SECURITY_ROUTES.md` and one generator test (`backend/internal/integration/security_routes_test.go`).

How to read the evidence: `file:line` points at the code I read. "Reviewed" means I read the service code that decides the question. "Spot-checked" means I looked but did not prove every method. Anything not listed was not looked at. Nothing below is a claim from a comment or a doc: AUDIT.md and the README were not trusted.

## Baseline

| Check | Result |
|---|---|
| `go build ./...`, `go vet ./...` | pass |
| `go test -p 1 ./...` (real Postgres, Redis, Mongo) | 20 packages ok, 0 failures |
| `go test -race` | **cannot run here**: this Windows PC has no C compiler (CGO is off). It has to run in Docker or CI. |
| `flutter analyze` | no issues |
| `flutter test` | 315 passed |
| `pytest` (ai-service) | 148 passed |
| Realtime (Node) `npm test` | 9 passed |
| `govulncheck ./...` | 1 reachable vulnerability (stdlib), 3 in required modules not called |
| `pip-audit -r ai-service/requirements.txt` | no known vulnerabilities |
| `npm audit --omit=dev` (realtime) | 0 vulnerabilities |
| gitleaks | **not installed**; I used a regex scan instead (see item 8) |

## Phase 0: route map

`docs/SECURITY_ROUTES.md` is generated from the live router. 265 routes: 219 signed-in, 31 staff, 15 public.

- Every route reports a rate limit except the three public health/webhook routes (`/healthz`, `/readyz`, `/payments/webhook`).
- Every route that takes an identifier has an ownership rule: 95 routes **reviewed** in code, 23 **spot-checked**, 120 act only on the caller's own data (the user id comes from the signed token). Zero are unreviewed.
- Inputs come from: JSON bodies (most handlers use `DisallowUnknownFields`), multipart photo and voice uploads, chat media (ciphertext), query strings (`limit`, `before`, `radius_km`, filters), path ids, the Stripe webhook body and `Stripe-Signature` header, `Authorization`, `X-Forwarded-For` (only from trusted proxies), and the Socket.IO events listed at the bottom of `SECURITY_ROUTES.md`.

## The 20 items

Severity: P0 exploitable now, P1 serious, P2 hardening, P3 note.

| # | Item | Status | Severity | Evidence |
|---|---|---|---|---|
| 1 | SQL / NoSQL / Redis injection | **PASS** | - | No `Sprintf`-built SQL. The few concatenations add constants or `NotBlockedSQL` fragments. The one dynamic insert (`users/details.go:219-224`) takes column names from a fixed list; profile updates use literal column names (`users/service.go:99-148`). Mongo filters use typed strings after `uuid.Parse` / `ObjectIDFromHex` (`chat/service.go:58,299`). Redis keys use ids or a hash of the email (`auth/service.go:117`). The AI service has no database. |
| 2 | XSS | **PASS** (one related finding) | - | No server-side HTML rendering. Admin dashboard has no `innerHTML` (`admin/web/app.js:1`). Emails are plain text (`auth/smtp.go:159-161`). Push text is generic. **Related: F1 email header injection.** |
| 3 | CSRF | **PASS** | - | No cookies anywhere (`SetCookie` does not appear); auth is `Authorization: Bearer` only (`pkg/middleware/auth.go:66`). |
| 4 | File uploads | **PASS** (photos), **not reviewed** (voice, ID-check, video bytes) | P3 | Photos: 5 MB cap (`users/photos.go:24`), magic-byte check, dimension/pixel limits, full re-encode to JPEG that drops EXIF/GPS (`photos.go:90-118`), uuid filenames, uuid check blocks traversal (`photos.go:46-52`), nosniff header. I did not read the voice, ID-check or video upload paths in detail. |
| 5 | BOLA / IDOR | **PASS** for everything reviewed | - | See `SECURITY_ROUTES.md`. Chat (`chat/service.go:58`), media, calls (`calls/service.go:204-210`), matches (`matching/matches.go:161-200`, `voicestep.go:25`), photos (`users/photos.go:227`), hotel mode (`geo/service.go:219`), safety (`safety/service.go:187,239,355`), scam check, social (`social/service.go:389`), key bundles. Unknown or forbidden objects return 404. 23 routes are spot-checked only (videos, communities, group/party games, masked rooms, voice, public profile, chapters). Not done: a live cross-user test for every route (that is Phase 2). |
| 6 | Rate limiting | **PARTIAL** | P2 | Redis sliding window, atomic (`middleware/ratelimit.go:14-27`). Fails **closed** with 503 except read/list/events limiters (`ratelimit.go:44-57`): correct. Signed-in routes key on user, public ones on IP; login also locks per email (`auth/service.go:117-120`). **Gaps:** (a) the Node relay keeps its limits in memory per node (`realtime/src/server.js:91,103`), no per-IP connect limit, no cap on sockets per user (F3); (b) `TRUSTED_PROXY_CIDRS` is optional in production (F4); (c) `/payments/webhook` has no limiter (signature-gated). |
| 7 | JWT | **PASS** (small gaps) | P3 | HS256 pinned, issuer, `exp` required, `typ=access` (`middleware/auth.go:60-75`). Secret at least 32 bytes and placeholder rejected (`config.go:17,175`). Refresh tokens opaque, hashed, rotated with reuse detection (`auth/tokens.go`). Password change, reset and ban revoke all sessions (`auth/account.go:57,65`, `moderation/staff.go:193`). Gaps: no `aud` claim; the session-cutoff lookup **fails open** if Redis is down (`middleware/auth.go:101`, F2). |
| 8 | Secrets in repo / client | **PASS** | P3 | No keys in tracked files or in the history (regex scan of all commits for Stripe, AWS, Google, GitHub, Slack keys and private-key blocks: 0 hits; **gitleaks was not available**). The app reads only `API_BASE` and `REALTIME_BASE` from `--dart-define` (`mobile/lib/core/config.dart:13-14`). The one regex hit in `api_client.dart:182` is a label, not a secret. Note: during this audit a search printed a local development test-account password from `ops/dev/test_account_2.txt` (gitignored, local database only). Treat it as seen and change it if you reuse it anywhere. |
| 9 | Password hashing / reset | **PASS** | - | bcrypt cost 12 (`auth/service.go:24`), timing equalizer for unknown emails (`:32-34,132`), same error for every login failure, registration and forgot-password do not reveal accounts. Reset and verify tokens are random, SHA-256 hashed in the database, single use, expiring (`auth/service.go:268-290`). |
| 10 | MFA | **NONE** | **P1 for staff** | No TOTP/MFA anywhere in the code. Staff and admin accounts can view ID documents, ban people and manage investor pledges and sponsor placements on password alone. Report only; design comes in Phase 2. |
| 11 | CORS | **PASS** (small gap) | P3 | Exact-match allowlist, no `*`, credentials only for listed origins (`middleware/cors.go`); production requires `APP_ALLOWED_ORIGINS`. Config does not reject a literal `*` entry. |
| 12 | Token storage | **PASS** (web note) | P3 | Tokens and chat keys use `flutter_secure_storage` (`core/network/token_store.dart:17-20`, `chat/crypto/secret_store.dart`). `SharedPreferences` only holds display settings. On the **web** build `flutter_secure_storage` falls back to browser storage, which a script injected into the page could read. |
| 13 | Server-side permissions / mass assignment | **PASS** | - | Roles come from the signed token and `RequirePermission` (`pkg/middleware`, `auth/rbac.go`); an existing audit test checks that ordinary users get 403 on the routes in its staff list. That list is matched by exact path, so it silently skips `GET /sponsors/` and `POST /sponsors/` (trailing slash); I read the permission check on those two in `sponsors/handler.go:27-36` and it is present (F16). Request structs are explicit; most decoders use `DisallowUnknownFields` (`auth/handler.go:53` and others). No request struct binds `role`, `is_verified`, `tier` or `coins`. |
| 14 | Row-level security | **FAIL: none** | P2 | No `ROW LEVEL SECURITY` or policy in any migration. The app connects as `connectme` (`.env.example:16`), which the Postgres image creates as a **superuser** and owner of every table. Any SQL bug therefore reads every member's rows. Report only; design comes in Phase 2. |
| 15 | Webhooks | **PASS** | - | HMAC-SHA256 over the raw body, constant-time compare, 5-minute tolerance, multiple `v1` values (`payments/stripe.go:19-72`); event id inserted with `ON CONFLICT DO NOTHING` before processing (`payments/webhook.go:45`); coins and refunds are idempotent by ref. |
| 16 | SSRF | **PASS / N/A** | - | Every outbound HTTP call goes to a configured base URL, never a user-supplied one (AI service, Tenor, Stripe, FCM, Twilio, LiveKit, video scanner). No link previews or image fetch. The Python service only calls its configured LLM endpoint (`ai-service/wingman/llm.py`). A shared safe-fetch client is still worth adding as a guardrail (Phase 2). |
| 17 | Debug exposure | **PASS** | - | No pprof or expvar. Metrics on a private listener (`127.0.0.1:9464`, `server.go` ServeMetrics). FastAPI docs, redoc and openapi are off (`ai-service/main.py:27`). The panic recoverer reports to Sentry with scrubbing and does not return stack traces. |
| 18 | Default credentials | **MOSTLY PASS** | P3 | Compose requires `POSTGRES_PASSWORD`, `REDIS_PASSWORD`, `MONGO_PASSWORD`, `AI_SERVICE_KEY` (`${VAR:?}`) and binds every port to `127.0.0.1`. k8s reads a secret. Seeds use an unusable password hash. **Exception:** the dev monitoring stack sets Grafana to `admin` / `admin` (`ops/dev/monitoring/docker-compose.yml:17`), bound to localhost only. |
| 19 | Sensitive data in logs | **PASS in production** | P3 | There is no request-logging middleware, so no bodies or headers are logged. The AI service logs categories, never text. The only calls that log emails, tokens, message bodies or phone numbers (`auth/mailer.go:24,35`, `safety/sms.go:35`) run only when no mail or SMS provider is configured; production refuses to start without SMTP and uses no SMS sender when Twilio is unset (`server.go:266-274`). |
| 20 | Dependencies | **FAIL (minor)** | P2 | `govulncheck`: **GO-2026-6809** `crypto/x509` in Go 1.27.2, fixed in 1.27.3, reachable through `payments/stripe.go:191`. Required modules, not called: `golang.org/x/net` 0.60.0 (GO-2026-6866, GO-2026-6860, fixed in 0.62.0) and `golang.org/x/crypto` 0.58.0 (GO-2026-5932, no fix released). Python and Node: clean. Flutter: minor updates available (`geolocator`, `go_router`, `livekit_client`, `crypto`, `meta` and others; `flutter pub outdated` has no vulnerability database, so this is version drift, not CVEs). **`libsignal_protocol_dart` 0.8.2** is a community Dart port of the Signal protocol, not maintained by Signal; it is actively released (0.8.2 on 2026-06-20) but I found no public independent security audit. |

## Findings, in priority order

| ID | Severity | Finding | Evidence | Fix |
|---|---|---|---|---|
| F1 | **P1** | **Email header injection through the display name.** A member can set a display name containing line breaks (proven: `StripTags` keeps `\r\n`, only the 50-character limit applies). The name is put straight into the `Subject:` of emails sent to trusted contacts, and the SMTP writer does not strip line breaks. An attacker who persuades someone to confirm them as a trusted contact can then send that person forged headers (Reply-To, extra body text) in a genuine email from our SMTP account. The envelope recipient cannot be changed. | `users/service.go:92-99`, `pkg/utils/validation.go:109-115`, `safety/service.go:292,379,381,461`, `auth/smtp.go:159` | Reject control characters in names at input and strip CR/LF in the mail writer (defence in depth). |
| F2 | P2 | Session revocation fails open: if Redis is down, logged-out, banned or password-reset tokens keep working for up to 15 minutes. | `middleware/auth.go:101` | Fail closed for staff routes; keep failing open for plain reads only if you accept it. Your decision. |
| F3 | P2 | Realtime relay: rate limits are in memory per node (budget multiplies with node count); no per-IP connect limit; no cap on sockets per user; an open socket is never closed when its token expires, the user logs out or is banned (it just stops being able to send). | `realtime/src/server.js:91,103,156-175` | Redis-backed limits, connection caps, close on expiry. |
| F4 | P2 | `TRUSTED_PROXY_CIDRS` is optional in production. Behind a load balancer with it unset, every member shares the balancer's address, so the login limit (20) and register limit (10) become global and one person can lock everyone out. | `config.go:170-172,282`; `middleware/clientip.go:61` | Require it in production, or fail startup. |
| F5 | P2 | The app's database account is a superuser that owns the tables, and there is no row-level security. | `.env.example:16`, no policies in `migrations/` | Separate least-privilege role first; RLS design (Phase 2, needs your approval). |
| F6 | P1 (staff) | No MFA for staff/admin. | no TOTP code anywhere | TOTP design (Phase 2, needs your approval). |
| F7 | P2 | Go 1.27.2 standard library vulnerability reachable (GO-2026-6809); `golang.org/x/net` below the fixed version. | `govulncheck` output | Upgrade the Go toolchain to 1.27.3 and `x/net` to 0.62.0. |
| F8 | P2 | **My own earlier change:** `csam.RecordHit` bans the account but does not revoke its sessions, so the current access token works for up to 15 minutes (refresh is already blocked for banned users). | `csam/hit.go:17-47` | Revoke all sessions in the same step as the ban. |
| F9 | P3 | Login lockout is per email: 10 bad guesses lock that address for 15 minutes, so anyone can lock out a known email. | `auth/service.go:25-26,120` | Lock on email + IP together. |
| F10 | P3 | `GET /chat/keys/{matchID}` uses up one of the other person's one-time prekeys per call, so a match partner can drain the pool (60 calls per window). The Signal protocol still works without them, with weaker forward secrecy for that first message. | `chat/keys.go:76-100` | Lower limit for this route. |
| F11 | P3 | `/payments/webhook` has no rate limit (it is signature-gated). | `SECURITY_ROUTES.md` | IP limiter generous enough for Stripe. |
| F12 | P3 | Production config does not reject a `*` CORS origin. | `config.go:148` | Validate. |
| F13 | P3 | Grafana `admin`/`admin` in the dev monitoring compose (localhost only). | `ops/dev/monitoring/docker-compose.yml:17` | Require a variable. |
| F14 | P3 | No `aud` claim in access tokens. | `middleware/auth.go` | Add and check. |
| F16 | P3 | The staff-route test matches exact paths, so the two trailing-slash sponsor routes are not covered by the "ordinary users get 403" check. | `integration/audit_routes_test.go:152-160` | Normalize paths in the test (Phase 2 route-registry test). |
| F15 | P3 | Dev-only mailer and SMS log tokens, bodies and phone numbers. | `auth/mailer.go:24,35`, `safety/sms.go:35` | Redact even in development. |

Also noted, not exploitable: end-to-end chat means the server could in principle hand out its own keys, so the app's "verify security key" screen is the real protection and should be promoted in onboarding.

## Proposed fix order (Phase 2, after you say "go")

1. F1 email header injection (small, certain).
2. F8 revoke sessions on a CSAM hit (small, mine).
3. F7 toolchain and `x/net` upgrades.
4. F4 require `TRUSTED_PROXY_CIDRS` in production.
5. F3 realtime limits and socket caps (Redis-backed), or fold into the planned Go WebSocket rewrite.
6. F2 session-cutoff behaviour (needs your decision on fail-open vs fail-closed for reads).
7. Shared helpers and tests the prompt asks for: cross-user test suite for every ID route, shared safe-fetch client and upload validator, redaction helper with a log test, stricter config validation, route-registry test.
8. F9-F15 hardening.
9. **MFA design** (F6) and **RLS design** (F5): documents only, then stop for your approval.
10. Phase 3 guardrails (CLAUDE.md rules, CI jobs, pre-commit hook, PR template).

## Needs your decision before Phase 2

- F2: when Redis is down, should a revoked or banned person's token keep working for reads (current), or should the app refuse everyone?
- MFA scope: TOTP for staff/admin only (as proposed), yes or no, and should it be mandatory before staff can open the dashboard?
- RLS: is the added operational complexity acceptable for launch, or should step one be only a non-superuser database role?
- Is the realtime Go rewrite still post-launch? F3 fixes depend on it.

## Not covered

Live cross-user attack tests (Phase 2), voice, ID-check and video upload internals, the 23 spot-checked route groups in full, Kubernetes network policies, load behaviour, `go test -race` (no C compiler here), and a gitleaks run.
