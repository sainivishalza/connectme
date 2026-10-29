# MFA design: TOTP for staff and admin (for your approval, nothing is built)

**Scope.** Accounts with the `moderator` or `admin` role, the people who can view ID documents, ban members, end rooms and manage investor pledges and sponsor placements. Ordinary members are not affected. (The partner portal no longer exists.)

**Decision taken from your delegation:** TOTP, staff and admin only, mandatory before the dashboard opens.

## What the person sees

1. Sign in with email and password as today.
2. **First time:** the dashboard shows "Set up two-step sign-in": a QR code and the same secret as text. They scan it in an authenticator app, type the six-digit code, and are shown **ten recovery codes once**. Nothing else in the dashboard opens until this is done.
3. **Every later sign-in:** after the password, a second box asks for the six-digit code (or a recovery code). Then the dashboard opens.
4. The dashboard asks for a code again after 12 hours, and after any password change or reset.

## How the server enforces it

- Staff access tokens carry an `amr` claim (authentication methods). A password sign-in gives `["pwd"]`; after the code is accepted the server issues a new pair with `["pwd","mfa"]`.
- Every staff route (the 31 in `docs/SECURITY_ROUTES.md`, found by the role check) requires `mfa` in `amr` and fails with 403 `mfa_required` otherwise. A route-registry test proves it for all of them, so a new staff route cannot forget it.
- A token with only `pwd` and a staff role can call just: enrol, confirm, verify, logout, and the member-level routes any member can call.
- Refreshing a session keeps the `mfa` state and its timestamp; it never upgrades `pwd` to `mfa`.
- Redis unavailable: MFA verification and enrolment **fail closed** (503).

## Storage

New migration, new tables only (nothing dropped or rewritten):

| Table | Columns |
|---|---|
| `staff_totp` | `user_id` (primary key, references users), `secret_enc` (AES-256-GCM ciphertext), `nonce`, `key_id`, `confirmed_at` (null until the first valid code), `last_used_step` (the 30-second step number last accepted), `created_at` |
| `staff_recovery_codes` | `user_id`, `code_hash` (SHA-256 of the code with a per-code salt; the codes are random with at least 50 bits of entropy), `used_at`, `created_at` |

- The TOTP secret is encrypted at rest with a key from the environment (`TOTP_ENC_KEY`, 32 random bytes, base64). `key_id` allows rotation. The key is never in the database or the repository. Production refuses to start without it when any staff account exists.
- Recovery codes are shown once and stored only as hashes.

## Codes and limits

- RFC 6238: SHA-1, 30-second step, six digits, accept the previous and next step (clock drift).
- A step already used is rejected (`last_used_step`), so a code cannot be replayed even inside its window.
- Constant-time comparison.
- Rate limit: 5 wrong codes per user per 5 minutes and 20 per address per hour, backed by Redis. Ten wrong codes in a row lock MFA for that person for 15 minutes and write an audit entry.

## Reset and recovery

- **Lost phone, has recovery codes:** a recovery code stands in for the six digits once, and the dashboard prompts to re-enrol.
- **Lost phone, no codes:** another admin resets their MFA from the dashboard. That action needs the acting admin's fresh `mfa` token, revokes the target's sessions, is written to the staff audit log, and the target must re-enrol at next sign-in. An admin can not reset their own.
- **Last admin locked out:** a documented command-line step run by whoever has database access (`docs/RUNBOOK.md`), also audited.
- "Forgot password" never switches MFA off.

## Audit

Enrol, confirm, verify success and failure, recovery-code use, reset, lock. All go to the existing staff audit log.

## Tests (each written to fail first)

Enrol, confirm and verify; replay of a used step; drift window; wrong code limits and the 15-minute lock; recovery code once only; `pwd`-only token refused on all 31 staff routes; refresh cannot upgrade; MFA outage returns 503; secret is not readable in the database; reset by another admin works and self-reset is refused.

## Needs your approval

1. Is "mandatory on first staff sign-in, no grace period" what you want?
2. Is 12 hours the right re-prompt interval?
3. Authenticator-app TOTP only for now (WebAuthn/security keys later)?
4. OK that the dashboard generates the QR code itself in the browser (no outside service)?

Nothing is implemented until you answer.
