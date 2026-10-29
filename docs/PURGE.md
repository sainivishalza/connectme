# MVP scope purge

Branch `mvp-purge`. The full app is kept at the git tag `archive/full-scope` (commit `f464a2b`).
Plan and findings: `docs/PURGE_REPORT.md`. Reusable prompt: `docs/PURGE_PROMPT.md`.

## What was removed, and why

Only features with a real reason to go were removed. Everything else stays, so the app keeps its feature set.

| Removed | Why |
|---|---|
| Live streams (`live`), audio rooms (`audio`) | The biggest safety and cost risk for a first launch: real-time moderation, no live-video scanner, per-minute LiveKit cost. LiveKit stays for calls. |
| Payouts (`payouts`), partners (`partners`), event groups (`eventgroups`), the partner portal (`portal`), paid hotel/partner bookings in `payments` | Paying out money and venue partnerships need identity, tax and business work not planned for launch. Hotel mode is not affected. |

Also changed so the kept features keep working:

- `livekit` is a new small package. It holds the LiveKit provider that calls use (Block 0).
- Levels and the leaderboards now read gifts sent in chat (`gifts_sent`) instead of live gifts. The "hosts" board lists the top receivers of chat gifts.
- `signal` and the realtime signal path keep only the `masked` and `game` room kinds.
- Moderation no longer has "end live stream" or ban-ends-live/audio hooks. Staff dashboard: the Live tab, the Host payouts tab and the live-rooms chart are gone.
- The app no longer has the Live and Audio tiles in Explore, the Events tile, the earnings link on the levels screen, the "bookings" and "live" notification switches, or the push-tap route to events.

## Kept on purpose (so nothing is lost without a reason)

quiz, sounds, videos, chapters, digest, sponsors, investors, levels, forums, communities, random chat, games, signal, groups, social, calls, follows.
Investors and videos are switched off by default (`ENABLE_INVESTORS`, `ENABLE_VIDEO`), so they do no harm until a lawyer and a video scanner are in place.

## Results

| | Before (`f464a2b`) | After |
|---|---|---|
| `go build`, `go vet` | pass | pass |
| Go packages in `internal/` | 42 | 37 |
| Go packages with tests, all passing | 19 | 19 |
| Top-level Go test functions | 320 | 281 |
| Non-test Go lines | 32,150 | 27,314 |
| Dart lines in `mobile/lib` (excluding generated l10n) | 23,948 | 20,565 |
| `flutter analyze` | no issues | no issues |
| Flutter tests | 359 passed | 310 passed |
| Flutter test files | 65 | 55 |
| Realtime (Node) tests | not run before | 9 passed |
| Files changed vs `main` | | 110 files, +485 / -13,190 |

Every dropped test belonged to a removed feature (Go: live, audio, co-hosts, polls, partners, payouts, portal, event groups, bookings; Flutter: live, audio, partners, event groups, payouts). Two kept test files were rewritten: the levels leaderboard tests now seed gifts in `gifts_sent` instead of going live, and the explore-hub test no longer expects the removed tiles.

## Environment variables

None became unused. LiveKit variables stay (calls). `ENABLE_VIDEO`, `VIDEO_SCANNER*` and `ENABLE_INVESTORS`, `INVESTOR_*` stay because those features stay.

## Orphaned tables (not dropped)

The migrations are untouched. These tables are no longer written by any code:
`live_rooms`, `live_gifts`, `live_events`, `live_cohosts`, `live_polls`, `live_poll_votes`, `live_room_bans`, `live_stage_requests`, `audio_rooms`, `audio_members`, `host_payouts`, `partners`, `partner_managers`, `partner_payouts`, `bookings`, `events`, `event_rsvps`, `event_messages`.

Two database functions still read them harmlessly: `charm_coins()` and `support_coins()` (they also add chat gifts, which is what now counts).

## Open items before launch

- These tables still hold personal data until a drop migration is written. If real users exist, write that migration and make sure account erasure still works.
- The database function `charm_coins()` and `support_coins()` can be rewritten to use chat gifts only.
- `ops/backup/drill.sh` lists `live_rooms` and `live_gifts`; they still exist, so the drill passes, but the list can be trimmed.
- Hindi and Spanish translations were pruned automatically; they have not had a native review.
- `flutter build apk --debug` and a run on a phone were not repeated after the purge.

## Rollback

Restore one thing from the full app:

```bash
git checkout archive/full-scope -- backend/internal/live mobile/lib/features/live
```

Undo a whole step with `git revert <commit>`. The commits are `b53e11b` (LiveKit package), `ba0cbac` (payouts and partners) and `e86d6f6` (live and audio).
Restoring a feature also means restoring its wiring in `server.go`, the router and the tests; reverting the commit does that in one step.
