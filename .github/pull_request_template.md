## What changed and why

<!-- One or two sentences. -->

## Security checklist (tick or write "n/a")

- [ ] **Routes / events:** every new endpoint or WebSocket event is listed in `docs/SECURITY_ROUTES.md` (run the generator) with its auth, ownership check, rate limit and role.
- [ ] **Object ids (BOLA):** every route that takes an id uses the shared authorization helper and has a cross-user test (stranger gets 404).
- [ ] **Auth / sessions / tokens:** changes to login, tokens, roles or lockout are covered by a test that fails without the change.
- [ ] **SQL:** parameters only; any dynamic column or sort name comes from an allowlist.
- [ ] **Uploads:** every upload goes through the shared upload validator (size, magic bytes, re-encode, random name).
- [ ] **Outbound URLs:** every fetch of a URL a user can influence goes through the safe-fetch client.
- [ ] **Logging:** nothing logs secrets, tokens, message bodies, ciphertext, coordinates, request bodies, emails or phone numbers.
- [ ] **Request structs:** explicit fields, `DisallowUnknownFields`, no binding straight into a database model.
- [ ] **Moderation / CSAM:** content checks fail closed.
- [ ] **Dependencies:** new or upgraded packages are justified and pass `govulncheck` / `pip-audit` / `npm audit`.
- [ ] **Secrets:** nothing secret committed; no server key in the Flutter app.
- [ ] **Checks run** (paste the result): `make check` (build, vet, `go test -race`, `flutter analyze`, `flutter test`, `pytest`, realtime tests, `govulncheck`).
