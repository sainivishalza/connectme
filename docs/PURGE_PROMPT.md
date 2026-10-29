# The Great Purge (revised prompt)

Paste everything inside the fence into a fresh session. Nothing is deleted until the owner replies `approved block X`.

```text
ROLE: You are preparing and then carrying out a scope-reduction refactor on the ConnectMe monorepo (Go backend in backend/, Flutter app in mobile/, plus admin/, realtime/, ai-service/, ops/). Goal: cut non-core features so the MVP can ship. Correctness over speed. Small verified steps.

THE ONE RULE ABOVE ALL OTHERS
Phase 1 is READ-ONLY. In Phase 1 you may run builds, tests and searches and write files under docs/ only. You must not delete, move or edit source files.
Phase 2 deletes ONE block at a time, and ONLY after I reply "approved block X" in chat. A reply about one block never approves another. If I have not said it, ask.
Anything that removes behaviour a kept feature has today (a field, a menu entry, an endpoint, a screen section) must be listed and confirmed by me before it is removed, even inside an approved block.

HARD RULES
- Never use --no-verify, never force-push, never push to main.
- Never comment out code or tests, add //nolint, skip tests, or loosen assertions to get green. Individual test cases and list entries that belong to a removed feature may be removed; assertions of kept behaviour may not be touched.
- Never edit or delete anything in backend/migrations/.
- Do not change behaviour of kept code except removing references to deleted code.
- Fix breakage by deleting dead references, never by stubbing.
- One commit per block, and the next block starts only when the current block is green and committed.

WHAT STAYS: backend auth, users, matching, chat, geo, idcheck, moderation, safety, privacy, push, payments, wallet, entitlements, gifs, signal, aiclient, scamcheck, admin, portal, server. Mobile auth, profile, discovery, chat, push, safety, settings, onboarding, explore, hotel_mode, payments.
KEEP-FOR-NOW (undecided, never delete in this run): backend groups, social, calls, follows; mobile group_dates, social, calls, follows.
CALLS STAYS, so LiveKit stays. calls and audio currently import live.Provider; see Block 0.

BLOCKS (backend package under backend/internal/<name> plus the Flutter folder under mobile/lib/features/<name>; mobile "random" is random_chat):
- Block 0 (refactor, deletes nothing): move the LiveKit provider out of the live package into its own small package, and point calls and audio at it. Same behaviour, tests unchanged.
- Block A: quiz, sounds, videos, chapters, digest, sponsors, investors (videos goes here because it imports sounds)
- Block B: levels, forums, communities, eventgroups, random
- Block C: audio, games, live
- Block D: payouts first, then partners
Check the order again from the Phase 1 findings; a block must never leave the build broken.

=== PHASE 1: PRE-FLIGHT AND REPORT (read-only) ===

1. git status must be clean, else stop and tell me.
2. Read backend/go.mod (the module path is the one in the file; import paths look like <module>/internal/<pkg>, NOT <module>/backend/internal/<pkg>), internal/server/server.go, cmd/server/main.go, pkg/config, the mobile router and nav files, README.md.
3. Baseline on main with the README commands. Go tests need the docker databases and, in this environment, TEST_POSTGRES_URL/TEST_REDIS_URL/TEST_MONGO_URL exported from .env plus -p 1, otherwise they silently skip. Record: go build, go vet, go test (packages ok, failures, number of top-level tests), flutter analyze, flutter test (count), non-test Go lines, Dart lines in mobile/lib (excluding generated l10n), Flutter test file count. Also run flutter build apk --debug if the Android toolchain is present. Record pre-existing failures; do not fix them.
4. Coupling scan for EVERY package in Blocks A-D. Imports are not enough. For each, list every use from outside the package, in all of these forms:
   a. Go imports of <module>/internal/<pkg>.
   b. Raw SQL touching the feature's tables or columns (for example users reading videos, profile_chapters, quiz tables, charm levels). Get the table names from the package and its migrations.
   c. JSON fields and API paths the kept code or the Flutter models read (reel_s, chapters, quiz_match, charm_level and similar).
   d. Flutter imports of the feature folder, router entries, menu and tab entries, onboarding steps, Explore tiles, public paths, Riverpod providers.
   e. Tests: for each test file that touches a deleted feature, say whether the whole file goes or only some cases/entries (route audit lists, harness config, load tests, a11y tests).
   f. Other services: admin/ web UI, realtime/ events, ai-service endpoints (for example /moderate/video), ops/ (compose, k8s, workflows, loadtest), .env.example.
5. Collect ALL conflicts first; do not stop at the first one. Produce one table: kept file and line, what it uses, which block, proposed fix, whether the fix removes user-visible behaviour.
6. Privacy check: the tables of removed features stay in the database. For each, say whether the account-erase flow (users/erase.go and privacy) still clears that user's rows, and whether the table holds personal data. List any that need a drop migration before launch.
7. Per block: line counts (with and without tests), the files that would be deleted, the files that would be edited, the l10n keys that would be dropped (referenced at HEAD, unreferenced after), the env vars that become unused.
8. Write docs/PURGE_REPORT.md with the baseline, the conflict table, the per-block plan and the privacy findings. Then STOP and wait for my reply.

=== PHASE 2: EXECUTE ONE APPROVED BLOCK ===

0. Once, before the first block: git tag -a archive/full-scope -m "Full-scope snapshot before MVP purge"; git -c http.proxy="$HTTPS_PROXY" push origin archive/full-scope; git switch -c mvp-purge. Confirm with git tag -l and git branch --show-current. Copy docs/PURGE_REPORT.md onto the branch.
1. Re-run the importer and SQL searches for this block. If anything is new compared with the report, stop and tell me.
2. Apply the approved fixes for the conflicts, then delete the Go packages, the Flutter folders and the tests that belong only to the block. Edit mixed test files case by case.
3. Remove wiring: mounts and constructors in server.go and cmd/server/main.go, config fields and env vars used only by deleted code (and their config tests), loadtest scenarios, router entries, menu/tab/Explore/onboarding entries, providers, route-audit list entries, .env.example lines. Prune l10n keys in en, hi and es by comparing keys referenced at HEAD with keys referenced now; leave keys that were already unused. Run flutter gen-l10n.
4. go mod tidy. Remove pubspec dependencies only if nothing remaining imports them (LiveKit stays).
5. Verify in this order: cd backend && go build ./... && go vet ./... && go test -p 1 ./... (with the database env from the baseline); cd mobile && flutter pub get && flutter analyze && flutter test. Everything must be as green as the baseline. State how many tests disappeared and that each belonged to a deleted feature.
6. Commit: "purge: block <X> (<package list>)" with the Co-Authored-By trailer. Report the block result and ask which block is next.

=== FINAL (after the last approved block) ===
1. Update README.md, docs/FEATURES.md, docs/ROUTES.md, docker-compose.yml, ops/k8s and .github workflows so nothing references deleted features.
2. Write docs/PURGE.md: baseline vs after, removed packages and Flutter features, env vars now unused, orphaned tables and migrations (do not drop them), privacy findings still open, and the rollback recipe: git checkout archive/full-scope -- <path>. A single block can be rolled back with git revert of its commit.
3. Run the full verification once more, plus flutter build apk --debug, and git diff --shortstat main.
4. git -c http.proxy="$HTTPS_PROXY" push -u origin mvp-purge. Do NOT merge.

REPORT FORMAT: (a) non-test Go LOC and Dart LOC before vs after; (b) test counts before vs after, backend and Flutter, with the reason for each drop; (c) every conflict found and how it was resolved, with my approval noted; (d) undecided packages: what depends on each and its line count; (e) open privacy items; (f) anything you were unsure about. Be literal. If something failed or was skipped, say so.
```

## Questions to answer before running it

- Is Live (just finished and polished) really out of the MVP?
- Gifts and coins stay in chat while live and payouts go. Is that the intended launch shape?
- When will groups, social, calls and follows be decided?
