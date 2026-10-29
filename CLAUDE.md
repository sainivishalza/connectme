# ConnectMe: notes for anyone (human or AI) changing this repository

Go API in `backend/`, Flutter app in `mobile/`, Python FastAPI in `ai-service/`, Node realtime relay in `realtime/`. Postgres/PostGIS, Redis, MongoDB.
Tests that touch the databases need the compose stack and these variables (otherwise they skip silently): `TEST_POSTGRES_URL`, `TEST_REDIS_URL`, `TEST_MONGO_URL`.

## Security rules for every change

These are rules, not suggestions. Each one exists because of a real finding in `docs/SECURITY_AUDIT.md`.

1. **Register every new route and WebSocket event.** Add it to `docs/SECURITY_ROUTES.md` (regenerate with `UPDATE_SECURITY_ROUTES=1 go test ./internal/integration -run TestWriteSecurityRoutes`) with its auth, ownership check, rate limit and role. The route-registry test fails if you do not.
2. **Every route that takes an id uses the shared authorization helper** and has a cross-user test in `backend/internal/integration`: a stranger gets **404** (not 403), an unauthenticated caller 401, a blocked user 404.
3. **SQL only through parameters.** Dynamic identifiers (column or sort names) come from an allowlist in code, never from the request.
4. **Never log** secrets, tokens, passwords, Authorization headers, message bodies, ciphertext, coordinates, request bodies, email addresses or phone numbers. Use `utils.MaskEmail` / `utils.MaskPhone` if you must mention one. The log tests will fail if you do.
5. **Outbound fetches go through the safe-fetch client** (`pkg/safefetch`) whenever any part of the URL or host can be influenced by a user. **Uploads go through the shared upload validator** (`pkg/uploadcheck`). Do not write a second one.
6. **Request structs are explicit.** List the fields, decode with `DisallowUnknownFields`, never bind a request straight into a database model (`role`, `is_verified`, `tier`, `coins` must be impossible to send).
7. **Never commit a secret and never put a server key in the Flutter app.** The app may only read `API_BASE` and `REALTIME_BASE` from `--dart-define`. If you find a secret, report `file:line` and a redacted prefix only, and say it must be rotated.
8. **New dependencies need a reason** in the pull request and must pass `govulncheck`, `pip-audit` and `npm audit`.
9. **Moderation and CSAM checks fail closed.** If screening cannot run, refuse the request (`503 moderation_unavailable`); never let content through unchecked.
10. **Session checks fail closed for staff and for writes** when Redis is unavailable.
11. **Never edit or delete an existing migration.** New migrations only; never drop or rewrite data.
12. **Never weaken a control to make a test pass.** No skipped tests, no `//nolint`, no loosened assertions, no commented-out code. Every fix gets a test that fails before the fix and passes after.
13. **Before you say a task is finished**, run `make check` (build, vet, `go test -race`, `flutter analyze`, `flutter test`, `pytest`, realtime tests, `govulncheck`) and state the results. If a check cannot run on your machine (for example `-race` needs a C compiler), say so plainly and do not claim it passed.

## Working rules

- Install the git hooks once: `make hooks` (secret scan, gofmt, go vet before every commit).
- Do not delete working features without asking first; the full app is kept at the tag `archive/full-scope`.
- Product or legal questions are decided by the owner, not guessed. Ask.
