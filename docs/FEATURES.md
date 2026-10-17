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
| F1.8 | Voice and video calls | Built, needs LiveKit | one-to-one over LiveKit; video in anonymous matches needs mutual reveal; incoming calls found by polling, no push yet |
| F1.9 | Burn-after-reading | Built | |
| F1.10 | Location message board | Built | hotel board |
| F1.11 | End-to-end encryption | Built | Signal protocol, 1:1 and groups |
| F1.12 | Double dates | Built | |
| F1.13 | Group dates | Built | |
| F1.14 | Group chat with AI icebreakers | Partial | template-based suggestions; no AI that joins the chat |
| F1.15 | Coins | Built | |
| F1.16 | Gifts | Built | |
| F1.17 | Silver / Gold / Platinum | Built | Stripe requests validated against Stripe's API spec; real test-mode run still needed |
| F1.18 | Boosts, super likes, invisibility | Built | |
| F1.19 | Photo ID and liveness | Partial | interface and stub only: needs a provider |
| F1.20 | CSAM hash matching | Partial | interface and stub only: needs a licensed provider (launch blocker) |
| F1.21 | Content moderation | Partial | text rules + staff dashboard; images are checked structurally, no nudity/violence model |
| F1.22 | Rate limiting | Built | audited on every route at runtime |
| F1.23 | Block, report, unmatch | Built | |
| F1.24 | Auto-delete after hotel checkout | Built | |
| F1.25 | Erase and export | Built | erasure is immediate |

## Phase 2

| # | Feature | Status | Notes |
|---|---|---|---|
| F2.1 | Live streaming | Built, needs LiveKit | with chat, host mute/kick, staff end |
| F2.2 | Gifts in live streams | Built | 70% host / 30% platform |
| F2.3 | Short video feed | Built, off by default | MP4/MOV up to 30 s, Open-mode posters only, likes, reports, staff review queue. Needs `ENABLE_VIDEO=true`; production also needs a real scanner behind `VIDEO_SCANNER=http` (interface built, no provider chosen) |
| F2.4 | Audio rooms | Built, needs LiveKit | host promotes up to 7 speakers, raise hand, anonymous lounges with generated aliases. Media path not run with real audio |
| F2.5 | Social games | Built (core) | truth, dare, icebreaker prompts from a curated bank; two-person couple quiz with a hidden-until-both-finish comparison. Not built: truth-or-dare *turn state*, group games, live party games |
| F2.6 | Nearby groups (interest-based, hotel guests) | Built | members-only posts; interest groups within ~25 km; a group per hotel for people checked in; owner moderation |
| F2.7 | Little Universe | Built | anonymous nicknames, 48 h posts, nearby |
| F2.8 | Masked Truth | Built (core) | anonymous truth-or-dare pairing on the random-chat queue. Not built: turn state, group rounds |
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
| F3.4 | AI Wingman for group dates (full agent) | Partial | template-based icebreakers only; a real agent needs an LLM provider |
| F3.5 | Sponsored live concerts and hotel events | Built (events), not streaming | partner-scheduled events with coin tickets, capacity and refunds. A partner cannot broadcast: streaming stays member-hosted (F2.1) |
| F3.6 | Event-based group hangouts (hotel mixers) | Built | hotel-guests-only mixers, RSVPs deleted with the stay, attendance as a count |
| F3.7 | Partner booking with commission | Built (requests, payment, ledger) | booking requests, staff confirm with a price, the member pays through Stripe Checkout, admin refunds, commission and amount owed reported. No automated payout to partners; never run against real Stripe |

## Beyond the PRD

Partner portal (`/partner/`) for venue managers: bookings, events and earnings, scoped to their own partner.

Moderator dashboard, Prometheus metrics, alert rules and a Grafana dashboard, CI, Kubernetes manifests, SMTP email, S3 media storage, password change and sign out everywhere, runbook, draft legal documents, and the redesign. See `DECISIONS.md` for why.

## What needs something from you

Real ID/liveness and CSAM providers, a content-moderation model, Stripe test keys, a push-notification provider (calls and messages while the app is closed), a LiveKit server for streaming and calls, physical devices and a Mac for iOS builds, a cluster, and the legal and penetration reviews.
