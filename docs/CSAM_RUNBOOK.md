# CSAM and unsafe-content runbook

This is the engineering side. **Legal counsel must define the reporting duty
(for example reporting to NCMEC in the United States, or the equivalent body
in each market), what evidence may and must be kept, and for how long, before
launch.** Nothing here replaces that advice.

## What the code does

### 1. Public content fails closed

Bios, display names, prompts, notes sent with a like, comments, posts, group
text and photos are screened by the AI service. If screening cannot run, the
request is **refused** with `503` and `{"error":"moderation_unavailable"}`
plus `Retry-After: 5`. Cases covered: no AI service configured, the call
errors, it times out (`AI_TEXT_TIMEOUT_MS`, `AI_IMAGE_TIMEOUT_MS`), the circuit
breaker is open, or the AI service says no image model looked at the image.
A swipe without a note still works. The app shows: "We can't check this right
now. Please try again in a moment." (English, Hindi, Spanish).

The AI service itself answers `503`, never "allow", when no image model is
loaded or inference fails.

`MODERATION_DEV_BYPASS=true` restores the old behaviour for local development
only. Production refuses to start with it.

### 2. Every uploaded image is scanned before it is stored

`internal/csam` defines `Scanner.Scan(ctx, image, meta) (Verdict, error)`. It
runs on profile photos and ID-check photos before anything is written to
storage or shown. Any error or timeout refuses the upload (`503`).

- `CSAM_PROVIDER=stub` accepts everything. It is allowed only when `ENV` is
  `development` or `test`.
- A real provider adapter is **not written yet**. `internal/csam/photodna.go`
  is a skeleton that always answers "unavailable". Until an adapter exists,
  production cannot start (config refuses), and a non-stub provider in
  development refuses every photo.

### 3. A positive match

The upload is blocked and the image bytes are never written to normal
storage. The server then:

1. writes a `csam_hits` row: user id, SHA-256 of the image, source
   (`profile_photo` or `id_check`), the provider's reference, timestamp;
2. suspends the account (`is_banned`), which also stops refresh-token use;
3. opens a `csam_hit` report so staff see it in the moderation queue.

Nothing is auto-deleted. The hit record survives account erasure (`user_id` is
set to null, the hash stays).

## What engineering does **not** do, and counsel must decide

- Whether and how to **report** a match, to whom, and within what time.
- Whether the image itself must be **preserved** as evidence. Today the image
  is not stored at all, so there is nothing to preserve; if counsel requires
  preservation, a locked, access-logged evidence store has to be designed.
- How long `csam_hits` rows and the related report are retained.
- Who on staff may view them, and the on-call procedure when an alert arrives.
- Staff welfare and training for the people who review this content.

## Open risks

- **No real provider yet.** Choose one and apply for access now, approvals can
  take weeks: Microsoft PhotoDNA, Google Content Safety API, Thorn Safer, or
  Cloudflare's CSAM scanning tool. Hash matching only finds *known* material;
  keep the nudity and violence classifiers as well.
- **End-to-end encrypted chat media is ciphertext.** The server cannot scan it.
  Users can send images in chat that nobody scans. Reports include the
  reporter's decrypted evidence, but there is no server-side detection. This is
  an accepted limitation of end-to-end encryption and a decision for counsel
  and product.
- **Short videos** have no CSAM scanner (see `docs/AUDIT.md` 3.10). They are
  off by default (`ENABLE_VIDEO=false`).
- **Photos "held for review"** (nudity between the review and block
  thresholds) are accepted and visible while they wait in the queue. See the
  safety-branch report for the exact code path.
- **Voice intros** (audio) are not scanned.

## Before launch

1. Pick and contract a provider; write the adapter in `internal/csam`;
   add the provider name to `config.ImplementedCSAMProviders` in an `init`.
2. Legal sign-off on reporting and retention.
3. Test a real positive match end to end with the provider's test material
   (never with real abuse material).
4. Staff procedure and on-call rota for `csam_hit` reports.
