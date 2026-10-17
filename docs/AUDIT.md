# ConnectMe: security & readiness audit

Scope: Phase 1 (Steps 1-9), the Step 10B audit, and the later additions: moderator dashboard, metrics, Kubernetes manifests, CI, live streaming (10A) and draft legal documents. **Live streaming** has API and Flutter screens tested; token signing, room close and participant removal are verified against a real LiveKit server (Docker); the camera/microphone path has never run on a device.
Date of audit run: see git history / CI; every claim below names the test or file that proves it.

## How to read this

- **PASS (automated)**: a test in this repo fails if the property regresses. Run `go test ./...` with the databases up.
- **PASS (reviewed)**: verified by reading the code; no regression test.
- **PARTIAL**: works, with a stated limit.
- **NOT DONE / BLOCKER**: not implemented or not verifiable on a development machine. Listed honestly in §4.

An automated audit cannot prove the absence of vulnerabilities. This report documents what was checked, how, and what was
*not* checked. It is **not** a substitute for an independent penetration test or legal review before launch.

### Test inventory at audit time

| Suite | Count | Notes |
|---|---|---|
| Go (API, real Postgres/PostGIS/Redis/MongoDB) | 165 pass, 0 fail, 9 opt-in skips | integration tests run through the real router and middleware; includes money-race and live-stream tests |
| Python (AI service) | 69 pass | |
| Node (realtime relay) | 7 pass | |
| Flutter (app) | 106 pass, `flutter analyze` clean | includes real Signal-protocol encryption round trips and the live screens |

Opt-in tests: `LOAD=1` load smokes (general and live room), `UPDATE_ROUTES=1` route inventory, `TEST_AI_URL` contract test against the real Python service, and `TEST_LIVEKIT_URL`/`TEST_LIVEKIT_SECRET` against a real LiveKit server (all run and passing; CI runs the LiveKit one).

---

## 1. Step 10B checklist (from the specification)

| # | Requirement | Status | Evidence |
|---|---|---|---|
| 1 | bcrypt password hashing (cost 12) | PASS (automated) | `TestAuditPasswordHashingCost`; `internal/auth/service.go` (`bcryptCost = 12`); passwords > 72 bytes rejected (`pkg/utils`) |
| 2 | JWT 15 min expiry + refresh rotation | PASS (automated) | `TestAuthFlow` (TTL ≈ 15 m, rotation, **reuse detection revokes the whole family**); `TestAuditBannedUserLosesAccessAtRefresh` |
| 3 | Rate limiting on every endpoint | PASS (automated) | `TestAuditEveryRouteRegistrationIsRateLimited` (every single-line route registration carries a limiter) and `TestAuditEveryLiveRouteIsRateLimitedAtRuntime` (walks the running router and requires every route, 97 at last count, to pass a limiter, announced by the `X-RateLimit-Limit` header; both fail when a limiter is removed); behaviour: `TestCoinsSuperLikeBoostAndSwipeLimit` (51st swipe -> 429), `TestAuditLoginRateLimited`, `TestSendRateLimit` (31st message/min). Limiter **fails closed** (503) if Redis is down |
| 4 | Location privacy: relative distance only | PASS (automated) | `TestAuditNoSensitiveDataInOtherUsersResponses` (no lat/lng/location key in any other-user response); `TestPrivacyOfOwnProfileAndLocation`; distances are bucketed from a ~1.1 km grid and radius filters use the snapped distance (`internal/users/search.go`) |
| 5 | E2E encryption for all chats | PASS (automated) / see limit §3.1 | server stores ciphertext only: `TestSendListAndAuthorization` (no readable field in MongoDB); client Signal round trips: Flutter `crypto_test.dart`, `chat_controller_test.dart` |
| 6 | Auto-delete 24 h after hotel checkout | PASS (automated, simulated clock) | `TestHotelLifecycleAndAutoDelete`, `TestHotelRetentionPurgesChatAndMedia`, `TestHotelGiftsAreDeletedWithTheStay`. Wall-clock 24 h wait was **not** performed (time is shifted in the database) |
| 7 | CSAM hash matching before media publish | **PARTIAL / BLOCKER** | the scan hook runs before any profile photo is stored and blocks on a match (`TestCSAMMatchBlocksStorage`); **only a stub provider exists**, production start is refused with `CSAM_PROVIDER=stub` (`pkg/config`). Chat attachments are E2E ciphertext and cannot be scanned server-side (§3.1) |
| 8 | GDPR erasure endpoint works | PASS (automated) | `TestErasureRemovesPersonalDataEverywhere`, `TestEraseGuards`, `TestStripeFailureAbortsErasureBeforeAnythingIsDeleted`, `TestBannedAccountCannotReRegisterAfterErasure`; export: `TestExportContainsOnlyYourData` |
| 9 | OWASP API Top 10 compliance verified | PASS (automated + reviewed) | §2 |
| 10 | Input validation on every endpoint | PASS (automated) | `TestAuditHostileInputNeverCausesServerErrors` (2,295 hostile requests across every route, no unhandled 5xx); every JSON body decodes with `DisallowUnknownFields` (`TestAuditMassAssignmentIsImpossible`) |
| 11 | RBAC enforced on all admin routes | PASS (automated) | `TestAuditStaffRoutesRejectOrdinaryUsers`, `TestRBAC`, `TestReportAndModeratorActions` (moderators cannot act on staff) |
| 12 | SQL injection prevention | PASS (automated) | `TestAuditNoStringBuiltSQL`: no `Sprintf` SQL; the only string splices are fixed internal helpers that are called with string literals; hostile-input test sends `' OR 1=1` payloads to every route |
| 13 | XSS prevention (sanitisation + output encoding) | PASS (automated) | all responses are `application/json` with `X-Content-Type-Options: nosniff` and `CSP: default-src 'none'` (`TestAuditSecurityHeadersOnEveryResponse`); HTML stripped from names/bios/posts (`TestProfileValidationAndModeRules`, hotel board test). The API serves no HTML |
| 14 | CORS for app domains only | PASS (automated) | `TestAuditCORS` (exact-match origins, no wildcard, no `null`, preflight from other origins -> 403) |
| 15 | Secrets from env, never hard-coded | PASS (automated) | `TestAuditNoSecretsOrWeakCrypto` (no live keys / private keys / hard-coded passwords; `.env` gitignored); config refuses weak or placeholder `JWT_SECRET`, and `STRIPE_*`, `CSAM_PROVIDER`, `AI_SERVICE_KEY` are required in production (`pkg/config`) |
| 15a | Dependencies free of known vulnerabilities | PASS (scan, not a test) | `govulncheck`, `pip-audit`, `npm audit` run clean for code paths we reach; CI runs them on every push. One advisory (`golang.org/x/crypto`, no upstream fix) is in a module we require but never call |

## 2. OWASP API Security Top 10 (2023)

| Risk | Status | How it is addressed / checked |
|---|---|---|
| **API1 BOLA** | PASS (automated) | Every object is fetched through the caller's identity; strangers get 404. `TestAuditObjectLevelAuthorizationMatrix` probes 18 object endpoints (photos, chats, media, key bundles, matches, groups, invites, dates) as an outsider. Profiles are addressed by an opaque `public_ref` that **rotates on mode change**, never by user id |
| **API2 Broken authentication** | PASS (automated) | bcrypt 12; account enumeration closed (register/forgot/login give identical responses, equal bcrypt work for unknown emails); per-account lockout after 10 failures; per-IP limits; single-use hashed email tokens; refresh-token reuse detection; banned users cannot refresh; `alg:none`/forged/expired tokens rejected (`TestAuth`) |
| **API3 Broken object property level authz** | PASS (automated) | one `PublicProfile` projection for all other-user output; `TestAuditMassAssignmentIsImpossible` (role, is_verified, mode, created_by, ... all rejected); `TestAuditNoSensitiveDataInOtherUsersResponses` (no ids/emails/DOB/hash/phone/coords) |
| **API4 Unrestricted resource consumption** | PASS (automated) | per-route body caps (8-32 KiB JSON, 5 MiB photos, 8 MiB attachments); page/radius/age bounds (`TestAuditResourceLimits`); image decode caps (dimensions, pixels); server timeouts; AI service input size limits |
| **API5 Broken function level authz** | PASS (automated) | `TestAuditEveryNonPublicRouteRequiresAuthentication` (73 protected routes -> 401 without a token), `TestAuditPublicRouteAllowlistIsExact` (new unauthenticated routes fail the build until declared), staff routes -> 403 for users |
| **API6 Unrestricted access to sensitive business flows** | PARTIAL | swipe limits 50/15 min and 100/h, daily free-like cap, super-like/boost entitlements enforced in a locked transaction, idempotent coin ledger and gifts (`request_id`), webhook replay protection, login lockout, report/block/gift rate limits. **Not done:** device/bot attestation, CAPTCHA, signup velocity per device, disposable-email blocking |
| **API7 SSRF** | PASS (reviewed) | the server never fetches user-supplied URLs. Outbound calls go only to configured Stripe and AI endpoints; checkout return URLs are fixed in config, never client-supplied (`TestCheckoutUsesServerSidePrices`) |
| **API8 Security misconfiguration** | PASS (automated) | security headers on every response including errors; strict CORS; no debug/docs endpoints (AI service docs disabled); config fails fast; containers run non-root; production forbids stub CSAM provider and requires Stripe/AI keys; per-IP limits are correct behind a proxy only when `TRUSTED_PROXY_CIDRS` is set (`clientip_test.go`) |
| **API9 Improper inventory management** | PASS (automated) | `docs/ROUTES.md` is generated from the live router; undeclared public routes fail tests; old endpoints are not retained |
| **API10 Unsafe consumption of APIs** | PASS (automated) | AI and Stripe responses are size-limited, schema-parsed, time-limited; circuit breaker; Stripe webhooks are HMAC-verified with a replay window and processed exactly once (`TestWebhookSecurity`, `TestCoinsGrantedExactlyOnce`); AI outage never blocks users (`TestAIOutageFailsOpenAndBreakerStopsCalls`) |

## 3. Residual risks and known limitations (read these)

### 3.1 Tension: end-to-end encryption vs. media scanning
Chats and chat attachments are end-to-end encrypted. The server therefore **cannot** CSAM-scan or nudity-scan them, and cannot moderate chat text.
Mitigations: client-side checks before encryption (not yet implemented in the app), report-with-evidence by recipients, block, rate limits.
Public content (profile photos, bios, posts, group text) *is* screened server-side. This trade-off needs a deliberate legal/product decision.

### 3.2 Moderation is a first line, not a safety system
- Text rules block clear minors/threats/scams/commercial sex and queue ambiguous cases. It is **not** an ML classifier; slurs require operator-supplied lists.
- Image moderation **does not detect nudity or violence** without a model (it says `screened_by_model: false`).
- Moderation **fails open** if the AI service is down (availability over strictness, documented in `docs/DECISIONS.md` #37).
- The moderator dashboard (`/admin/`, embedded in the API binary) covers the report queue, user lookup, ban/unban with a mandatory reason, and an admin-only append-only audit log. It is a first tool, not a case-management system: no appeals workflow, no assignment/SLA tracking, no bulk actions.

### 3.3 Identity & age assurance
- Photo-ID/liveness is a **stub** behind an interface (`auth.LivenessVerifier`); no real provider is integrated. The 18+ gate is a self-declared date of birth checked against a UTC-12 calendar. It is not age verification.

### 3.4 Chat history is device-local
A Signal message key can be used once, so history exists only on the device that decrypted it. Reinstalling or losing the phone loses history (by design, no server backup).
Multi-device is not supported. Group chats use sender keys distributed through the server's opaque mailbox; new members cannot join an active group match.
A "safety number" / key-change warning UI is not built (a changed identity key simply fails to decrypt).

### 3.5 Payments
- Only tested against a Stripe **stub**; no real Stripe account was used. Run the Stripe CLI against test keys before launch.
- Apple/Google rules generally require in-app purchase for digital goods in iOS/Android apps. **The current Stripe Checkout flow likely violates store policy**: needs a decision before store submission.
- Subscription plan switching, proration, partial refunds and disputes are not automated.

### 3.6 Operations
- Retention/purge job and chat sweeper run in-process once a minute; they are idempotent and safe on several replicas. Staleness alerts exist in `ops/alerts.yml` (`RetentionJobStalled`, `ChatRetentionStalled`) but have never fired against a real Prometheus.
- Metrics, alert rules and a Grafana dashboard exist (`ops/`) but have not been run against a live Prometheus/Grafana; there is no tracing or error tracking (Sentry).
- Database backups and disaster recovery are not set up (the plan and key-rotation steps are in `docs/RUNBOOK.md`, a draft never exercised in a drill). See `docs/DEPLOYMENT.md`.
- Rate limiting fails closed for sign-in, writes and payments, so a Redis outage stops those routes (reads fail open); see `docs/DECISIONS.md` #60.
- Media (photos, chat attachments) can live on local disk or in any S3-compatible bucket (`S3_*` settings). The whole integration suite, including erasure and hotel retention, passes against a real S3 API (SeaweedFS in Docker); it has not been run against AWS S3, bucket policies/lifecycle rules are yours to set, and objects are served only through the authenticated API (never public URLs).
- Access tokens are stateless: after a password change, "sign out of all devices", or a ban, a stolen access token keeps working until it expires (15 minutes); refresh tokens are revoked immediately.
- Email: an SMTP mailer is implemented and verified against a real SMTP server (MailHog) for delivery, header-injection refusal and no-credentials-without-TLS; production startup requires `SMTP_HOST`/`SMTP_FROM`. No real provider account has been used, so deliverability (SPF/DKIM/DMARC, bounce handling, reputation) is untested.

### 3.7 Scale
The load check (`LOAD=1`, in-process, 120 concurrent users, 4,800 requests mixed reads) measured about 1,300 req/s, p50 65 ms, p95 190 ms, p99 385 ms, **0 errors** on a developer laptop sharing CPU with the databases.
A live-room scenario (`LOAD=1 go test ./internal/integration -run TestLoadLiveRoom`: 200 viewers joining, polling events every round, chatting and gifting one room, 5,600 requests in-process) measured about 870-1,300 req/s, p50 120-200 ms, p95 320-400 ms, p99 450-540 ms, **0 errors**. Same caveats: one laptop, no network or TLS, database and Redis sharing the CPU. It is indicative, not a capacity statement.
This says nothing about 10,000 concurrent users; **no real capacity test has been done**. Known scaling work: replace per-request profile/photo joins with caching, add read replicas, index review under real data, Elasticsearch/geo sharding if nearby search outgrows PostGIS.

### 3.9 Newer features (random chat, voice, calls, voice intro, games)
- **Anonymous random chat** puts strangers in contact. It is verified-members-only, rate limited, and every chat is reportable, but messages are end-to-end encrypted and therefore **not scanned**: moderation depends on reports and on staff capacity that does not exist yet. Decide the policy and staffing before enabling it publicly.
- **Voice notes and voice intros** cannot be screened (there is no speech-to-text or audio model); a voice intro is served only to people who may see the full profile. Recording and playback were never run on a physical device, and Safari's recording format differs from Chrome's (the app tries AAC first, then Opus).
- **Calls** need a LiveKit server and **push notifications** to ring a closed or locked phone; today incoming calls are found by polling while the app is open. The call state machine and privacy gates are tested; the media path is verified only against LiveKit's token/room API, not with real audio or video.
- **Games**: the prompt bank is curated and tested for banned content, but it is English-only and fixed; quiz answers are stored on the server (low sensitivity, deleted with the match).

### 3.10 Phase 2 social features (posts, groups, audio rooms, videos, Masked Truth)
- **Posts and groups** put user text in front of strangers. Text is screened by the AI service when it is available (fail-open, as elsewhere), every post can be reported and staff can remove it (audited). Group posts are members-only; missed connections never leave the venue. There is no image content in posts.
- **Audio rooms** are unmoderated live speech. The host can kick and demote, staff can ban (which ends the host's rooms), but nothing listens to the audio. Treat them like streams: decide policy and staffing first. The media path (LiveKit permission update, rejoining with a new token) is tested against our own token/permission code, not with real audio or a real LiveKit server.
- **Videos and reels: the scanner interface exists, a provider does not.** Uploads are scanned before they are stored or served; flagged clips are held for a moderator, blocked ones refused and queued, and a scanner outage holds clips (tested with a stand-in HTTP scanner). Production refuses `ENABLE_VIDEO` without `VIDEO_SCANNER=http` over https. What is still unverified is a *real* provider: hash matching against abuse databases and classification quality need a vendor, a legal agreement (and NCMEC reporting where applicable) and sample testing. Until then keep the feature off, and staff capacity to clear the queue is part of readiness. Validation (MP4/MOV header, `mvhd` length, 12 MB, 30 s, 5 per day), signed 10-minute URLs and the photo-equivalent visibility rule for reels are tested; transcoding, thumbnails and adaptive streaming are not built, so large or exotic files rely on what phones record.
- **Masked Truth** pairs anonymous strangers: same abuse exposure as random chat (3.9).

### 3.11 Phase 3 (forums, moods, chapters, partners, events, bookings)
- **Forums** are public speech between strangers. They share the text screening and report path of other public text and staff can remove posts, but nothing watches them live; moderation capacity matters as much as for posts and groups.
- **Partners are trusted input from admins and their managers.** An admin creates a partner and links managers; managers (ordinary members, scoped to one partner, access ended by removal or a ban) then write events, prices and booking decisions through the portal, so partner text and prices are only as good as those people. Managers see guests as a name only for Open-mode members and a short code otherwise, and cannot reach another partner's data (tested). A venue still learns who turns up in person, which no software can prevent. Events charge coins, which is tested for double charges, capacity races at the lock, refunds and cancellation; the coin purchase path itself (Stripe) is still unverified against real accounts.
- **Bookings can be paid through Stripe Checkout** (amount fixed by staff, checked against the database in the webhook, refunds confirmed by webhook), tested with a stand-in Stripe and request shapes validated by `stripe-mock`. It has **not** run against real Stripe: test-mode runs with the Stripe CLI (checkout, webhook delivery, refund) are required before launch. **Partners are not paid out by the app**; the report states what is owed and settlement happens outside it. Money held between collection and payout is the platform's to account for, with the legal and tax treatment that implies in each market; that needs advice, not code.
- **Hotel mixers** are limited to checked-in guests and vanish with the stay, but a venue can still see who attends in person: attendance is private inside the app, not at the door.
- **Chapters and moods** reuse the photo-visibility rule and are tested not to appear on anonymous profiles.

### 3.12 Performance of the newer feeds
`TestFeedsUnderLoad` (opt in, `LOAD=1`) drives 80 concurrent members through eight read endpoints (moments, universe, videos, forum threads and replies, partner events, nearby partners, nearby groups) over a seeded database, in one process against one local Postgres. After the fixes in DECISIONS 103-105: p50 about 160-900 ms and p95 about 0.3-1.5 s per endpoint, no errors; moments is the slowest because it also builds author cards. Before them the moments feed's p50 was 7.7 s. These numbers show relative cost and catch regressions; they are **not** a capacity claim: a real deployment needs a load test against production-shaped infrastructure with connection pooling, read replicas and caching decisions, and the 10K-concurrent-user target remains untested.

### 3.13 Push notifications
- **Built and tested server-side** against a fake FCM endpoint (RS256 assertion verifies against the key, token cached, payload and priority hints, only `UNREGISTERED` deletes tokens) and through the real router with a recording provider (generic payload, collapse, preferences, banned/unverified skipped, sign-out-everywhere forgets devices).
- **Never exercised against Google or Apple**: no Firebase project, key or phone was available. Delivery latency, iOS behaviour (APNs key, background modes, critical/time-sensitive calls), battery policies on Android and token rotation in the real SDK are unverified. Treat push as "implemented, not proven" until the runbook checks in 7a pass on devices.
- **A notification is metadata.** Even generic, it tells the provider and the lock screen that this person had a message, match or call at a time. That is the unavoidable cost of push; members can switch each kind off, and the app never prompts without a tap.
- **Messages from anonymous matches notify too**, with the same generic text and the match id; the id is not linkable to a person on its own.
- The in-memory queue loses queued notifications if the process dies; for this feature that is acceptable (messages remain in the app).

### 3.14 Second security pass
Method: read the authentication, token, rate-limit, CORS, realtime-relay and key-directory code and the client's Signal store looking for defects, then wrote failing tests for each finding before fixing it.

| Finding | Severity | Status |
|---|---|---|
| Access tokens stayed valid up to 15 min after a ban, sign-out everywhere, password change/reset | High | Fixed (decision 119), tested |
| Demoted staff kept their role until the token expired; no way to force it | Medium | Fixed: admin revoke-sessions (decision 120), tested |
| App gave no warning or recovery when a contact's identity key changed; no way to verify a key, so a first-contact key swap by the server was undetectable | Medium | Fixed in the app: banner, send blocked until reviewed, safety number (decision 121). A server that swaps keys before first contact is only caught if people actually compare numbers |
| A matched person could exhaust a victim's one-time prekeys | Medium | Fixed (decision 122), tested |
| Realtime relay: unlimited typing/receipt/join events per socket | Low | Fixed (decision 123), tested |
| CORS preflight omitted `PUT`, breaking cross-origin web clients | Low (functional) | Fixed |
| Password reset left push devices registered | Low | Fixed |
| Per-email login lockout (10 failures/15 min) lets someone lock a victim out of password login | Low | Accepted: an attacker needs only an email; the per-IP limit and the lock expiring after 15 minutes bound it. Revisit with CAPTCHA or device-based throttling |
| Forgot-password does a database insert only for real accounts (a millisecond-scale timing difference) | Info | Accepted: mail is queued, response shape is identical |
| The dev mailer logs verification tokens | Info | Not reachable in production: production refuses to start without SMTP |
| Web build keeps tokens and Signal keys in browser storage (no OS keystore) | Medium for web | Known limit of the web preview; the native apps use the keychain/keystore |

Checked and found sound: access-token validation (algorithm pinned, issuer, expiry, type), refresh rotation with reuse detection and family revocation, email-token storage (hashes only, single use), constant-time-equivalent login for unknown emails, 72-byte bcrypt limit, X-Forwarded-For only from trusted proxies, security headers and CSP, upload re-encoding and pixel limits, no secrets or message bodies in logs, non-root containers with read-only filesystems, SQL built only from fixed helpers (audit test), and the domain separation of the HMAC uses of the JWT secret (video tickets and forum aliases cannot collide with a JWT signing input).
Not covered: an independent penetration test, a review of the native Signal implementation beyond its integration, and fuzzing of the media parsers.

### 3.8 Platform builds
Android and iOS release builds were **not produced** on the development machine (Android SDK command-line tools and macOS unavailable). The app was verified by analysis, 71 tests and a full `flutter build web --release` compile (re-run after adding the LiveKit client package). No physical-device run, no TestFlight/Play internal track.

## 4. Spec "launch checklist" (Part 8) status

| Item | Status |
|---|---|
| All build steps built and reviewed | Phase 1 steps 1-9 + this audit. **Step 10A live streaming built** (API + Flutter list/watch/broadcast/gift screens; our tokens and DeleteRoom verified on a real LiveKit 1.13 server; device media path unverified) |
| Security audit passed | PASS for the checks above; **independent pen test not done** |
| iOS / Android build runs on a physical device | **NOT DONE** |
| All API endpoints tested with curl/Postman | covered by automated integration tests; route inventory generated |
| Stripe webhook tested with Stripe CLI | **NOT DONE**. Partial: our outgoing requests (checkout in both modes, cancel at period end, cancel now) are validated against Stripe's official OpenAPI spec via `stripe-mock` (`stripe_mock_test.go`, opt-in, passing); webhook signature handling is unit-tested. Real test-mode runs are still required |
| Hotel auto-delete job verified | PASS with simulated time |
| E2E encryption verified (cannot read messages in DB) | PASS |
| Rate limiting verified (51 swipes -> 429) | PASS |
| CSAM hash matching tested with a sample image | **BLOCKER**: stub provider only |
| GDPR erasure endpoint tested | PASS |
| Admin moderation dashboard functional | PASS (browser-verified; strict CSP; every view/ban audited; Go tests in `staff_dashboard_test.go`) |
| Load test 10K concurrent users | **NOT DONE** (120-user smoke only) |
| Docker + K8s deployment working | images built and smoke-tested; K8s manifests + NetworkPolicies written in `ops/k8s` but **not applied to a real cluster** |
| Monitoring (Prometheus + Grafana) | **PARTIAL**: metrics on a private listener, alert rules and a Grafana dashboard are written (`ops/`); never run against a live Prometheus/Grafana |
| Error tracking (Sentry) | **NOT DONE** |
| Privacy policy + terms drafted | **DRAFTED** (`docs/legal/`, placeholders + legal review required) |
| 18+ age gate enforced | PASS (self-declared; see §3.3) |
| Legal review for target markets | **NOT DONE** |

## 5. Defects found *by* this audit and fixed

| Finding | Severity | Fix |
|---|---|---|
| GDPR/CCPA erasure and data export endpoints were **missing** (spec F1.25) | High | `internal/privacy`, tests, in-app UI; billing cancelled first, external stores purged before the row is deleted |
| Banned users could erase and re-register to evade a ban | Medium | one-way email-hash tombstone (`banned_identities`) |
| Per-IP limits would treat all users behind a load balancer as one client | High (production) | `TRUSTED_PROXY_CIDRS`; right-most-untrusted `X-Forwarded-For` hop, header ignored from untrusted peers |
| `POST /groups/{id}/match` (and other group routes) returned 500 on a malformed id | Low | UUID validation at the shared membership check |

Defects found and fixed earlier in the build (each with a regression test) are recorded in the step reports and `docs/DECISIONS.md`:
age gate depending on server time zone; refresh interceptor deadlock on a second 401; chat controller using `ref` in a dispose callback;
one-time prekeys re-uploaded incorrectly; boosted users sorted last; test/data isolation issues; moderation evasion by letter repetition.

## 6. Recommended next steps before launch

1. Independent penetration test and a legal review (privacy, age assurance, E2E vs. scanning, store policies).
2. Real providers: CSAM hash matching, ID/liveness, email, object storage, push notifications.
3. Decide the in-app purchase model; test Stripe with the Stripe CLI.
4. Capacity test at target scale; add tracing and error tracking (metrics, alerts and a dashboard exist in `ops/`).
5. An appeals process and moderator case management; a nudity/violence model behind the existing image interface.
6. Build and test Android/iOS releases on physical devices; add safety-number UI and client-side attachment scanning.
7. Live streaming: run on physical devices (camera/mic permissions are declared but untested); in-stream chat is polled every 2 s (hosts can mute or remove viewers); voice/video calls and voice notes are Phase 2.
