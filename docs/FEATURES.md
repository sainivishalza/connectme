# Feature coverage against the PRD

Status of every feature in `prompts/0_prd_and_prompts.txt` §4. **Built** means implemented and covered by automated tests on a development machine. Nothing here has run on a physical phone, against real third-party accounts (Stripe, ID/liveness, CSAM, email provider) or on a real cluster: see `AUDIT.md` for what is and is not verified.

## Phase 1

| # | Feature | Status | Notes |
|---|---|---|---|
| F1.1 | Nearby people | Built | PostGIS; distance shown only as a bucket |
| F1.2 | Swipe matching | Built | like, pass, super like (coins) |
| F1.3 | AI compatibility scoring | Built | rule-based scorer in the AI service, fail-open |
| F1.4 | Open / Incognito toggle | Built | one projection decides what anyone can see |
| F1.5 | Hotel / same-place discovery | Built | geofence check-in, feed, drinks, 24 h auto-delete |
| F1.6 | Anonymous random matching | Built | queue pairs two waiting people into a 10-minute anonymous encrypted chat; both must Keep chatting or it vanishes. Needs moderator capacity before public launch |
| F1.7 | Real-time chat: text, voice notes, images | Built | all end-to-end encrypted; voice notes up to 2 minutes |
| F1.8 | Voice and video calls | Built, needs LiveKit | one-to-one over LiveKit; video in anonymous matches needs mutual reveal; incoming calls also send a generic push when a device is registered (real push needs Firebase credentials) |
| F1.9 | Burn-after-reading | Built | |
| F1.10 | Location message board | Built | hotel board |
| F1.11 | End-to-end encryption | Built | Signal protocol, 1:1 and groups |
| F1.12 | Double dates | Built | |
| F1.13 | Group dates | Built | |
| F1.14 | Group chat with AI icebreakers | Built (opt-in) | icebreaker ideas, plus "Ask Wingman for a reply": a member ticks up to 8 recent messages and gets 3 reply ideas. The AI is not a silent chat member (impossible under end-to-end encryption); it sees only what a member chooses to share |
| F1.15 | Coins | Built | |
| F1.16 | Gifts | Built | |
| F1.17 | Silver / Gold / Platinum | Built | Stripe requests validated against Stripe's API spec; real test-mode run still needed |
| F1.18 | Boosts, super likes, invisibility | Built | |
| F1.19 | Photo ID and liveness | Built (human-reviewed) | member photographs an ID and a selfie holding a random code; a moderator approves or rejects; photos are deleted on decision. Not automated and not forensic: a paid provider could replace it later |
| F1.20 | CSAM hash matching | Partial | interface and stub only: needs a licensed provider (launch blocker) |
| F1.21 | Content moderation | Partial | text rules + staff dashboard; images get structural checks, a perceptual-hash blocklist and, with `NSFW_MODEL_PATH`, a local nudity model (plus an optional violence model that only sends images to human review; accuracy unmeasured) |
| F1.22 | Rate limiting | Built | audited on every route at runtime |
| F1.23 | Block, report, unmatch | Built | |
| F1.24 | Auto-delete after hotel checkout | Built | |
| F1.25 | Erase and export | Built | erasure is immediate |

## Phase 2

| # | Feature | Status | Notes |
|---|---|---|---|
| F2.1 | Live streaming | Removed from the MVP | with chat, host mute/kick, staff end |
| F2.2 | Gifts in live streams | Removed from the MVP | 70% host / 30% platform |
| F2.3 | Short video feed | Built, off by default | MP4/MOV up to 30 s, Open-mode posters only, likes, reports, staff review queue. Needs `ENABLE_VIDEO=true`; production also needs a real scanner behind `VIDEO_SCANNER=http` (interface built; ai-service ships a frame-sampling scanner at `/moderate/video` that needs the nudity model; not a certified provider) |
| F2.4 | Audio rooms | Removed from the MVP | host promotes up to 7 speakers, raise hand, anonymous lounges with generated aliases. Media path not run with real audio |
| F2.5 | Social games | Built (core) | truth, dare, icebreaker prompts from a curated bank; two-person couple quiz with a hidden-until-both-finish comparison. Truth or dare is turn-based with skips and take-over. Group games: Would you rather and Most likely to. Live rooms have host-run polls. Group truth or dare and masked rooms (3 to 5 strangers) built. Not built: party games with scoring |
| F2.6 | Nearby groups (interest-based, hotel guests) | Built | members-only posts; interest groups within ~25 km; a group per hotel for people checked in; owner moderation |
| F2.7 | Little Universe | Built | anonymous nicknames, 48 h posts, nearby |
| F2.8 | Masked Truth | Built (core) | anonymous truth-or-dare pairing on the random-chat queue, opening into the turn-based game. Not built: group rounds |
| F2.9 | Missed connections at events | Built | venue-only, no author shown, "that's me" consent handshake into a hotel match |
| F2.10 | Nearby feed / Moments | Built | 24 h posts shown the way each author's mode allows |
| F2.11 | Video profile reels | Built, off by default | one per profile, visible only where photos are; same gate, scanner and review queue as F2.3 |
| F2.12 | Voice intro clips | Built | up to 30 s; playable only by people who can see the full profile, never while anonymous |

## Phase 3

| # | Feature | Status | Notes |
|---|---|---|---|
| F3.1 | Forums (dating advice, travel, city boards) | Built | seeded boards, admin-made city boards, alias unless Open mode, reports, staff removal |
| F3.2 | Mood-based hotel discovery | Built | business / company / local guide per stay; filter and chips |
| F3.3 | Chapter-based profiles | Built | up to five chapters, visible only where photos are |
| F3.4 | AI Wingman for group dates (full agent) | Built, needs an API key | a language model plans the outing (venue kinds, icebreakers, a step-by-step plan) from shared interests; strict input cleaning and output validation, templates as the fallback. Never run against the real model |
| F3.5 | Sponsored live concerts and hotel events | Removed from the MVP | partner-scheduled events with coin tickets, capacity and refunds. A partner cannot broadcast: streaming stays member-hosted (F2.1) |
| F3.6 | Event-based group hangouts (hotel mixers) | Built | hotel-guests-only mixers, RSVPs deleted with the stay, attendance as a count |
| F3.7 | Partner booking with commission | Removed from the MVP | booking requests, staff confirm with a price, the member pays through Stripe Checkout, admin refunds, commission and amount owed reported. No automated payout to partners; never run against real Stripe |

## Beyond the PRD

Partner portal (`/partner/`): removed from the MVP together with partners.

Moderator dashboard, Prometheus metrics, alert rules and a Grafana dashboard, CI, Kubernetes manifests, SMTP email, S3 media storage, password change and sign out everywhere, runbook, draft legal documents, and the redesign. See `DECISIONS.md` for why.

## What needs something from you

Real ID/liveness and CSAM providers, a content-moderation model, Stripe test keys, a push-notification provider (calls and messages while the app is closed), a LiveKit server for streaming and calls, physical devices and a Mac for iOS builds, a cluster, and the legal and penetration reviews.

## Added after the first feature pass
| Item | Status | Notes |
|---|---|---|
| Group-date logistics fee | Built | 100 coins per person for a shared plan (venue kinds, icebreakers, outing steps); refunded if the date is cancelled |
| Sponsored placements | Built (API-managed) | one labelled card in Moments/Little Universe; Silver+ see none; 3-minute cap; location targeting; totals only |
| Partner payout ledger | Removed from the MVP | staff record each settlement (paid minus commission); no double payout; partners see history in the portal. Money transfer is still manual |
| Error tracking | Built | Sentry-compatible, privacy-scrubbed, off without `SENTRY_DSN` |
| Group game: Would you rather | Built | matched group chats; anonymous totals; one round at a time |
| Rich profiles | Built | goal, kids, lifestyle, languages, height, job, education, hometown, star sign, 3 prompts; anonymous modes show only lifestyle answers |
| Discovery filters on what people want | Built | goal, kids, smoking, drinking, exercise, language, ID verified only |
| Profile completeness meter | Built | percentage plus the next step, on the Profile tab |
| ID verified badge on cards | Built | shown on every card, anonymous included |
| Swipe deck with drag gestures | Built | photo-first cards, photo carousel, LIKE/NOPE/SUPER stamps, arrow keys, profile sheet |
| Match celebration screen | Built | full-screen, photo, Say hi |
| Rewind | Built (Gold) | undo the last pass within 10 minutes |
| Like with a note | Built | 140 characters, shown in Likes you and the chat opening card; never from an anonymous sender |
| Top picks | Built | best 3 (Gold 10) with the reason in one sentence |
| Chat replies, reactions, unsend | Built | all inside the end-to-end encrypted payload; unsend deletes on the server |
| Message search | Built | on-device only (chats are encrypted); jumps to the message |
| Group Truth or Dare / Masked rooms | Built | turn engine for group chats and anonymous rooms |
| Sounds on short videos | Built | 8 synthesised loops; no licensing |
| Live polls (host-run party game) | Removed from the MVP | 2 to 4 answers, totals live |
| Event groups (board for people going to an event) | Built | RSVP is the ticket; deleted a day after |
| Live room themes | Removed from the MVP | 6 looks; decoration only |
| Timed reveal in anonymous matches | Built | both agree, either can cancel |
| Weekly summary email | Built | opt-in, numbers only, one-tap unsubscribe |
| Languages (en, hi, es) | Partly | ~190 labels across most screens; messages with changing parts are English; drafts need native review |
| Safety center (trusted contacts, date plans, check-in alerts) | Built | emails need SMTP + APP_URL in production |
| Scam warnings in chat | Built | on-device, English keywords |
| Appearance settings | Built | theme, text size, reduce motion; device-only |
| Screen-reader swipe deck | Built | card described; Like/Pass/Super Like as actions |
| Investor page (pledges, public ownership board) | Built | off unless ENABLE_INVESTORS; no money handled in-app; needs legal review |
| Live co-hosts | Removed from the MVP | two per room; publish, mute, pin |
| Gift animations in live rooms | Removed from the MVP | flying icon by gift type |
| Video comments and Following filter | Built | shorts only |
| Reply notifications | Built | generic, own switch |
| Quiet hours | Built | per-member window; calls always ring |
| Guided welcome (onboarding) | Built | 5 skippable steps, shown once |
| Compatibility quiz | Built | 30 questions; rounded match % on every card incl. anonymous |
| Voice-first reveal step | Built | both agree to swap voice intros before photos |
| Voice room topics | Built | 7 topics, filter |
| Levels (Charm, Supporter) | Built | derived from gifts; badge on visible profiles; supporter private by default |
| Leaderboards | Built | top hosts and supporters, week/month/all time; opt-in, visible members only |
| Follow members | Built | only profiles shown in full; private follower list; block removes |
| "Followed host is live" push | Removed from the MVP | generic text, own switch |
| Live topics and pinned message | Removed from the MVP | 7 topics, filter, host pins one line |
| Comments on posts | Built | follows post visibility; nicknames under anonymous posts |
| GIFs and animated stickers | Built | through our server; built-in stickers or Tenor; sent as encrypted attachments |

## Improvements pass (after Phase F)
| Item | Status | Notes |
|---|---|---|
| Push signals instead of polling | Built | games and masked rooms refetch on a signal; slow polling stays as backup. Needs the realtime relay running |
| Cached leaderboards | Built | 30 s, blocks applied fresh |
| Reveal timer job | Built | checks every 5 s (was 30 s) |
| Scam warnings in Hindi and Spanish | Built | keyword lists, wording not reviewed by native speakers |
| Quiz answer-change cap | Built | 6 per day |
| Masked room cooldown and evidence | Built | 2 minutes after leaving; blocked for a day after two reporters; reports keep the room's last lines |
| Check-in alerts to all confirmed contacts | Built | plus all-clear to all of them; no SMS |
| Bottom bar labels | Built | |
| Photo order, main photo and captions | Built | captions shown only where the photo is shown |
| Fixed-price investor model | Built | `INVESTOR_MODEL=fixed`; default stays pro-rata |
| Host payouts | Removed from the MVP | request, stamp gifts, admin records transfer; no money moves in the app |
| Staff trends, report reasons, member search | Built | aggregates only; searches audited |
| Match-score tuning tool and setting | Built | needs real outcome data to be useful |
| Backup and restore drill | Built | `ops/backup/drill.sh` |
| Reported masked rooms (staff) | Built | grouped by room, with the room's last lines kept as evidence |
| Ban appeals | Built | public form proving the account, staff lift or keep with a reason, audited |
| AI scam check ("Look closer") | Built | member shares the last 8 messages once; rules in three languages; a guide, not a verdict |
| Text alerts to trusted contacts | Built | code read out by the contact first; Twilio; never run against a real provider |
| My reports | Built | reporter sees waiting / action taken / no action, never who was reported |
| Live stage: ask to join, invites, up to 7 guests | Removed from the MVP | needs two real phones to check camera and sound; LiveKit media path never run on a device |

> Rows marked "Removed from the MVP" are cut for the first launch. The code is kept at the git tag `archive/full-scope` (see `docs/PURGE.md`).
