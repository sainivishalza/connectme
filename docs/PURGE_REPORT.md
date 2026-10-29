# Purge report (Phase 1, read-only)

Nothing was deleted or edited. This file is the only thing written, besides `docs/PURGE_PROMPT.md`.
Commit under inspection: `f464a2b` on `main`.

## 1. Baseline

| Check | Result |
|---|---|
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test -p 1 ./...` with the Docker databases | 19 packages ok, 0 failures, 320 top-level Test functions |
| `flutter analyze` | no issues |
| `flutter test` | 359 passed, 0 failed |
| Non-test Go lines | 32,150 |
| Dart lines in `mobile/lib` (excluding generated l10n) | 23,948 |
| Flutter test files | 63 (65 entries in `mobile/test`) |
| `flutter build apk --debug` | not re-run now. It was built and installed on the phone earlier from this same code. |

No pre-existing failures. The test numbers were measured on this same commit earlier in the session.

## 2. Conflicts: kept code that uses code in the purge list

"Visible" = a user or staff member loses something they have today.

| # | Kept file | What it uses | Block | Proposed fix | Visible? |
|---|---|---|---|---|---|
| 1 | `users/details.go:8,290` | `levels.CharmLevels` (import) | B | Drop the charm-level lookup and the `charm_level` field. Flutter: `CharmBadge`, `charmLevel` in `profile.dart`, `deck_card`, `profile_card`, `follows_screen` | Yes: charm badge on profiles and the following list |
| 2 | `users/quiz.go`, `users/details.go:276` | table `compat_answers` (quiz) | A | Delete `attachQuiz` and `quiz_match`. Flutter: `QuizMatchBadge`, `quizMatch` in the profile model, `deck_card`, `profile_card` | Yes: "% quiz match" on cards |
| 3 | `users/voice.go:150` | table `videos` (profile reel) | A | Drop the `reels` query and `reel_s`. Flutter: "Watch video" button, `ReelSection` in edit profile | Yes |
| 4 | `users/voice.go:167` | table `profile_chapters` | A | Drop the `chapters` count. Flutter: "Read my story" button, `ChaptersSection` in edit profile | Yes |
| 5 | `moderation/service.go:12,46`, `staff.go`, `handler.go`, `staff_handler.go` | `videos` (import): held-video queue, remove/approve/ticket endpoints, `held_videos` stat | A | Remove the video staff tools | Staff only |
| 6 | `moderation/service.go`, `analytics.go:85`, `server.go` | live rooms, audio rooms and forums through interfaces (`StreamEnder`, `CallEnder`, `ForumRemover`) and the `live_rooms` count | B/C | Remove the matching staff endpoints (end stream, remove forum post, rooms list) and the analytics column | Staff only |
| 7 | `privacy/service.go:50`, `server.go:206` | the videos file purger | A | **Leak:** uploaded video files are deleted on account erasure only by this purger. Once videos go, any existing clips stay in storage. Needs a one-off cleanup of the video bucket/folder. | No, but a privacy gap |
| 8 | `payments/bookings.go`, `handler.go:42-43`, `webhook.go` | tables `bookings`, `partners` | D | The paid hotel/partner booking flow (`/payments/bookings/{id}/checkout` and `/refund`, webhook branch) lives in kept `payments`. Remove it together with `partners`. | Yes: booking payments and staff refunds |
| 9 | `signal/signal.go:91-95` and `realtime/src/server.js:24-25` | tables `live_rooms`, `masked_members` (games), `event_rsvps` (partners) | C/D | The `signal` package only serves four room kinds (`live`, `masked`, `event`, `game`). After the purge none is left, so `signal`, its route and the `realtime` signal path become dead. Needs a decision (see section 6). | Yes |
| 10 | `portal` (listed as "stays") | partners API (`/partners/manage`) | D | The portal is only a UI for partners. It is useless once `partners` goes. Needs a decision. | Yes |
| 11 | `push/service.go:163,278-301`, `notifications_screen.dart` | push preference columns and switches for `bookings` and `live` | C/D | Keep the DB columns, remove the two switches, or keep them as no-ops. Needs a decision. | Yes |
| 12 | `calls/service.go:18`, `audio/service.go:20` | `live.Provider` (LiveKit) | C | **Block 0:** move the provider to its own package first | No |
| 13 | `server/server.go` | constructors and mounts for every block, plus `followsSvc` passed into `live.SetAlerts` | all | Remove with each block | n/a |
| 14 | `admin/web/app.js:139,244,360` | payouts and videos pages | A/D | Remove those admin pages | Staff only |
| 15 | `ai-service/main.py:86` | `/moderate/video` | A | Remove the endpoint and its tests (ai-service is outside the "stays" list) | No |
| 16 | `chat_screen.dart:6,26,280,563` | `random_chat` banner and the `games` sheet ("Play a game" menu, Masked Truth entry) | B/C | Remove the banner, the menu item and the `openGames` parameter | Yes |
| 17 | `feed_screen.dart:11,12` | `communities` filter and `sponsors` ad slot | A/B | Remove the ad slot; remove the community feed option from the (undecided) social feed | Yes |
| 18 | `welcome_screen.dart` (onboarding), `profile_screen.dart`, `explore_screen.dart`, `app_router.dart` | steps/tiles/routes: quiz step, quiz and invest tiles, `/profile/levels`, `/profile/earnings`, Explore tiles (videos, audio, random, forums, live, events, groups), public paths `/invest` and `/digest/unsubscribe` | all | Remove the entries for deleted features | Yes |
| 19 | `follows_screen.dart:48` | `charmLevel` text | B | Drop the charm text | Yes |
| 20 | `ops/backup/drill.sh:47` | table list includes `live_rooms`, `live_gifts`, `investments` | all | The tables still exist, so the drill still passes. Update the list in the final step. | No |

Also touched but not conflicts: `eventgroups` reads `events`/`event_rsvps` (owned by `partners`); `levels` reads `live_gifts`; `communities` is used by `social` (kept, undecided). Because B runs before D and `eventgroups`/`levels` go with B, the order A, B, C, D is fine for the build.

## 3. Test files

Whole-file deletions (Go integration tests): `audio_test`, `audio_topics_test`, `chapters_test`, `cohost_test`, `communities_test`, `digest_test`, `eventgroups_test`, `forums_test`, `games_test`, `investors_test`, `levels_test`, `live_load_test`, `live_stage_test`, `live_test`, `live_topics_test`, `livekit_real_test`, `partner_payouts_test`, `partners_test`, `payouts_test`, `quiz_test`, `random_test`, `sponsors_test`, `truth_or_dare_test`, `video_comments_test`, `video_review_test`, `videos_test`, and `booking_payments_test` (with conflict 8).

Edits inside shared Go test files: `harness_test.go` (config options `EnableVideo`, `EnableInvestors`, the video scanner stand-in, `levels.CacheTTL`), `audit_routes_test.go` (route list entries), `feeds_load_test.go` (videos, forums, partners, communities seeds and endpoints), `pkg/config/config_test.go` (video scanner tests).

Flutter whole-file deletions: `audio_test`, `audio_topics_test`, `cohost_test`, `comments_live_topics_test`, `communities_test`, `compat_quiz_test`, `digest_test`, `event_group_test`, `forums_test`, `games_test`, `gift_burst_test`, `invest_test`, `levels_follows_test`, `live_poll_test`, `live_stage_test`, `live_test`, `most_likely_test`, `party_game_test`, `payouts_test`, `random_test`, `room_theme_test`, `truth_or_dare_test`, `video_comments_test`, `video_sound_test`, `videos_test`, `wyr_test`.
Mixed Flutter files that need case-by-case edits: `a11y_test.dart` (investors, levels, live, payouts, quiz, videos), `partners_test.dart` (a chapters case plus partner cases; the partner cases go with Block D), `comments_live_topics_test.dart` (live and social).

## 4. Per-block plan

| Block | Go lines (with tests) | Dart lib lines | Translation keys used only by the block | Env vars that become unused |
|---|---|---|---|---|
| 0 (refactor) | 0 removed | 0 | 0 | none |
| A: quiz, sounds, videos, chapters, digest, sponsors, investors | 3,291 | 1,503 | 13 | `ENABLE_VIDEO`, `VIDEO_SCANNER`, `VIDEO_SCANNER_URL`, `VIDEO_SCANNER_KEY`, `ENABLE_INVESTORS`, `INVESTOR_OWNER_NAME`, `INVESTOR_MODEL`, `INVESTOR_PRICE_PER_PERCENT`, `INVESTOR_PAYMENT_NOTE` |
| B: levels, forums, communities, eventgroups, random | 2,376 | 1,623 | 14 | none found |
| C: audio, games, live | 4,726 | 4,259 | 34 | none (LiveKit stays for calls) |
| D: payouts, partners | 1,967 | 702 | 2 | none found |
| **Total** | **12,360** | **8,087** | **63** | |

Whole-package deletions, per block, are the packages in the list above plus their Flutter folders (`eventgroups` has no Flutter folder; its screen lives under `partners`).
Files edited in kept code: see section 2. About 60 files in total.

Env vars were checked in `backend/pkg/config/config.go`. Only the video and investor settings are specific to a block.

## 5. Privacy findings

- Account erasure runs `DELETE FROM users`. All checked tables of removed features reference `users` with `ON DELETE CASCADE` or `SET NULL`, so their rows disappear with the user even after the code is gone. Tables checked: videos, video_comments, video_likes, profile_chapters, compat_answers, compat_changes, host_payouts, live_gifts, live_rooms, audio_rooms, audio_members, partner_managers, bookings, forum_threads, forum_replies, community_members, event_messages, random_waiting, td_games, masked_members.
- **`investments` is `ON DELETE RESTRICT`.** Deleting an account of anyone who pledged will fail. That is true today as well. Before launch, either keep an investors-erasure path or drop the table in a migration after exporting what you must keep.
- **Video files in storage** are only removed by the videos purger (conflict 7). After Block A, existing clips are never erased. Clear the storage before launch.
- The tables are orphaned, not empty. They still hold personal data (video captions and comments, chapters, quiz answers, forum posts, bookings, payout records) until a drop migration is written. Treat that as a launch blocker if any real users exist.
- `group_td` has no direct `users` foreign key (it links through its game rows), so it needs a look when the migration is written.

## 6. Decisions (made by Claude, with the owner's delegation)

1. **Live and audio rooms: out of the MVP.** They are the biggest safety and cost risk for a first launch. Live video needs real-time moderation and staffing, there is no live-video abuse scanner behind it, and LiveKit is billed per minute. Gift revenue cannot flow anyway while Stripe is on hold. The code stays in `archive/full-scope` and is the first feature to bring back after launch. LiveKit stays for calls.
2. **Gifts and coins stay, chat-only. Payouts and partners' payouts go.** Paying people out brings identity checks, tax rules and fraud risk we should not take on at launch. Coins and gifts in chat keep a simple way to earn later.
3. **Paid hotel/partner bookings leave `payments` with `partners` (Block D).** Hotel mode itself stays (check-in, geofence, board, drink all run in `geo`, which does not use `partners`). Venues and events need business partners we do not have yet, and Stripe is on hold.
4. **`signal`, the realtime signal path and `portal` are removed in a new Block E**, after D, because nothing will use them. Chat realtime stays.
5. **Remove the "bookings" and "live" notification switches** from the app. The database columns stay, and the Go preferences struct is trimmed only if the columns have defaults (checked when Block D/C run).
6. **groups, social, calls and follows all stay for the MVP.** Group dates are in the original product spec, calls are core to dating, follows is tiny and low-risk, and social carries the hotel board and missed connections. Review them after the first closed beta using real usage. One flag for that review: the anonymous "Little Universe" posts need moderation capacity before launch. The community option in the social feed goes with Block B.

Order: 0, A, B, C, D, E. Each needs your "approved block X" in chat before it runs.

## 7. Undecided packages (report only)

| Package | Lines | Depends on it |
|---|---|---|
| groups (backend) | 1,841 | `server.go` only |
| social (backend) | 1,206 | `server.go`, `communities` (purged in B), one integration test |
| calls (backend) | 456 | `server.go` only; uses `live.Provider` (Block 0) |
| follows (backend) | 234 | `server.go`; passed to `live.SetAlerts` (goes with C) |
| Mobile group_dates, social, calls, follows | not measured here | the router and, for social, the feed (conflict 17) |

## 8. What I was unsure about

- The coupling scan found table references by name, so a table with a very common name could be over-counted. I checked the flagged hits by hand (section 2) and the rest owner-only hits were confirmed as local.
- The translation key counts come from a simple word match. The real pruning in Phase 2 compares keys before and after.
- `flutter build apk --debug` was not re-run in Phase 1.

STOP. Waiting for your reply before any block runs.
