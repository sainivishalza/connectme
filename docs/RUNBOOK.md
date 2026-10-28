# ConnectMe operations runbook

Written from how the system actually behaves (see `DECISIONS.md`, `ops/alerts.yml`). It has **not been exercised in a real incident or drill**; run a game day before launch and correct what you find. Replace `[bracketed]` items with your real contacts and tooling.

## 0. Roles and first five minutes
- **Incident lead:** [name/rota]. **Comms:** [name]. **Legal/DPO:** [name].
- Open an incident channel, write the timeline as you go (UTC), and stop making unrelated changes.
- Triage: *is user safety at risk?* (a stream or account causing harm) → section 3. *Is data exposed?* → section 5. *Is it availability?* → section 1.

## 1. Alerts and first responses
| Alert | Meaning | First actions |
|---|---|---|
| `RetentionJobStalled` | Hotel 24 h purge has not succeeded for 10 min. **Privacy promise at risk.** | Check API logs for `geo_retention` errors; confirm Postgres reachable; restart one API pod (job is idempotent and runs in every replica). If it stays down, treat as a data-retention incident (section 5). |
| `ChatRetentionStalled` | Chat sweeper/burn job stalled | Same as above for `chat_retention`; check Mongo connectivity. |
| `JobErrors` | A background job is failing repeatedly | Read the error in logs; most often a DB/Mongo outage. |
| `HighServerErrorRate` | 5xx ratio elevated | Check `/readyz`, DB pool (`DBPoolSaturated`), recent deploy; roll back the last release first if one just shipped. |
| `SlowRequests` | p95 latency high | Look at the Grafana route panel for the slow route; check DB pool and slow queries. |
| `DBPoolSaturated` | Pool exhausted | Find long transactions in `pg_stat_activity`; scale API or raise pool only after finding the cause. |
| `AIServiceBreakerOpen` | AI screening down; **moderation is failing open** | Restore the AI service. Meanwhile expect more content to reach humans only via reports; raise moderator staffing and consider pausing new-account photo uploads if abuse spikes. |
| `StripeWebhookFailures` | Webhooks not verified/processed | Check `STRIPE_WEBHOOK_SECRET`; Stripe retries for days, so fix then replay from the Stripe dashboard (handlers are idempotent). |
| `LoginFailureSpike` | Credential stuffing or outage | Check per-IP limits are firing (`RateLimitingSpike`); confirm `TRUSTED_PROXY_CIDRS` so client IPs are real; consider tightening at the edge/WAF. |
| `RateLimitingSpike` | Abuse or a client bug | Identify the limiter label in metrics; compare with a recent app release. |

Useful facts: `/healthz` is liveness, `/readyz` checks dependencies; metrics are only on `METRICS_ADDR` (private); rate limits, sessions and refresh tokens live in Redis and the limiter **fails closed** for logins, writes, payments and gifts: if Redis is unreachable those routes answer 503. Cheap authenticated reads (limiters named `*:read`, `*:list`, `*:events`) fail open and are counted in the `ratelimit_fail_open` event metric, so browsing, polling and stream viewing continue. Refresh and sign-in still need Redis. Treat Redis as a tier-1 dependency (managed, replicated).

## 2. Rollback and deploys
- Images are immutable tags; roll back by redeploying the previous tag. Migrations are forward-only: **never ship a migration that the previous release cannot run against** (additive changes first, destructive later).
- After any rollback verify `/readyz`, the login flow, and one chat send.

## 3. Safety incidents (harm to a person)
1. **Stop the harm:** ban the account (dashboard → Look up a user → Ban, reason required) or end the stream (Live streams → End stream). A ban revokes sessions and ends live rooms immediately.
2. **Preserve evidence:** the reports (with the reporter's decrypted evidence), the staff audit log and account metadata are retained; do not delete the account. Chats are end-to-end encrypted and unreadable to us: only the reporter's evidence exists.
3. **Escalate:** threats to life or child-safety material → contact the relevant authorities immediately [law-enforcement contact / NCMEC process]. CSAM must go through the licensed provider workflow (**not integrated yet: launch blocker**); until then, never view, copy or forward such material beyond what the law requires, and call counsel.
4. **Follow up:** record the decision in the report, notify the reporter if policy allows, and consider whether a rule or screening change is needed.

## 4. Key and credential compromise
| Secret | Where it is used | Rotation steps | Effect |
|---|---|---|---|
| `JWT_SECRET` | Go API and Node realtime (both must share it) | Generate ≥32 random bytes; update **both** services' environment and restart them together | All access tokens (15 min) become invalid. Refresh tokens are opaque and stored in Redis, so users silently get new access tokens. **To force everyone to sign in again** also delete refresh state: scan Redis for `rt:*`, `ufam:*`, `rtrevoked:*` and delete them. |
| `AI_SERVICE_KEY` | API ↔ AI service | Set the same new value on both, restart AI first then API | Brief fail-open window for moderation |
| `STRIPE_SECRET_KEY` / `STRIPE_WEBHOOK_SECRET` | Payments | Roll in the Stripe dashboard, update env, restart API | Webhooks fail until both sides match; replay afterwards |
| `LIVEKIT_API_KEY/SECRET` | Token signing and room close | Add the new key pair on the LiveKit server, update env, restart API, then remove the old key | Existing tokens (≤2 h) stop working when the old key is removed |
| DB / Redis / Mongo passwords | API | Change in the datastore, update the secret, rolling restart | Short connection errors during rollover |

After any suspected compromise: rotate, review the staff audit log (`/moderation/audit`, admin only) for unexpected staff actions, check recent role changes in `users.role`, and invalidate sessions as above.

## 5. Personal-data breach
1. Contain (rotate credentials, block the access path) and **start the clock**: regulators such as GDPR supervisory authorities expect notice within 72 hours of awareness where there is risk to people.
2. Establish scope with logs and the audit trail: which tables or object stores, which users, which fields. Passwords are bcrypt hashes; chats are ciphertext; exact location is stored server-side (the most sensitive field); photos are on the media volume.
3. Involve legal/DPO [contact]; decide on regulator and user notification; prepare a plain-language notice.
4. Fix the cause, add a regression test, and write a post-incident review.

## 6. Backups and restore
- **Postgres** is the system of record: take continuous/point-in-time backups [provider/tooling]. **Redis** holds sessions and rate limits only (loss = everyone signs in again; unavailability = sign-in and all writes fail, see section 1). **Mongo** holds ciphertext only. **Media volume** holds photos and attachments.
- **Erasure and backups:** deleted accounts are erased immediately from live data; backups age out on the schedule stated in the privacy policy. If you restore a backup, **re-apply erasures made since**: keep the erasure/ban record (`banned_identities` and a log of erased user ids) and re-run it after a restore, or deleted people reappear.
- Test a restore into a scratch environment quarterly and record the time it took.

## 7. Moderation operations
- Queue health: dashboard stats (open reports, oldest open). Target: oldest open report [N] hours.
- Every staff view of a user and every action is in the append-only audit log; review it [weekly] for unusual access. Moderators see no email/phone/DOB/location.
- Appeals: [process and owner: not built]. Decisions can be reversed with Unban plus a reason.

## 7a. Push notifications
Server: set `PUSH_PROVIDER=fcm`, `FCM_PROJECT_ID` and `FIREBASE_CREDENTIALS` (path to a service-account key with the *Firebase Cloud Messaging API* enabled; mount it as a secret, never bake it into an image). A key that cannot be read or parsed stops startup. With `stub` (default) nothing leaves the process.
App: add `firebase_core` and `firebase_messaging`, the Firebase config files (`google-services.json`, `GoogleService-Info.plist`) and, for iOS, upload an APNs key to the Firebase project. Write a class implementing `PushTokenSource` (`lib/features/push/push_source.dart`) over `FirebaseMessaging` and override `pushTokenSourceProvider` with it in `main.dart`. Verify on a physical phone: permission prompt from Settings > Notifications, a message while the app is closed, a tap opening the chat, a call waking the phone, and sign-out stopping notifications.
Watch `push_sent`, `push_failed`, `push_token_dropped`, `push_collapsed` and `push_dropped` in `/metrics`; a rise in `push_failed` means the provider or the key is unhealthy, a rise in `push_dropped` means the queue is saturated.

## 7b. Onboarding a partner
1. An admin creates the partner (`POST /partners`, with location, kind, commission and, for a hotel, `hotel_id`).
2. The venue's person registers a normal ConnectMe account and verifies their email.
3. The admin links it: `POST /partners/{id}/managers {"email": "..."}`. Removing: `DELETE /partners/{id}/managers/{ref}` (the ref is in `GET /partners/{id}/managers`).
4. The manager signs in at `/partner/` to confirm bookings with a price, schedule events and read earnings.
5. Settlement: `GET /partners/{id}/report` shows collected, commission and owed amounts and refunds due; payouts happen outside the app. Refunds: `POST /payments/bookings/{id}/refund` (admin), recorded when Stripe confirms.

## 7c. AI wingman
Set on the ai-service: `WINGMAN_PROVIDER=llm`, `ANTHROPIC_API_KEY` (a secret), optionally `WINGMAN_MODEL`, `WINGMAN_LLM_RPM` and `WINGMAN_TIMEOUT_S`. Without a key it silently stays on templates. Check it works: `POST /wingman/suggest` with the internal key returns `"source":"llm"` and a `plan`; `"source":"ai"` means it fell back (look for `wingman: ...` log lines, which never contain prompts). Allow egress from the ai-service to the provider on TCP 443. Update the privacy notice before enabling (see AUDIT 3.15).

## 8. Maintenance hygiene
- Rotate staff accounts when people leave: set `users.role='user'` or ban, **and** call `POST /moderation/users/{ref}/revoke-sessions` (admin): the role inside an access token is trusted until the sessions are revoked.
- Review dependency advisories monthly (`go list -m -u all`, `pip-audit`, `npm audit`, `flutter pub outdated`); the CI runs the test suites on every push.
- Re-run the audit suite (`go test ./internal/integration`) before each release; new routes must be added to its allowlists deliberately.
