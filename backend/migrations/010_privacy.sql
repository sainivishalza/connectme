-- Erasure must not become a way to dodge a ban. When a banned account is
-- erased we keep only a one-way hash of the email so it cannot simply
-- re-register (legitimate-interest retention for platform safety).
CREATE TABLE banned_identities (
  email_hash BYTEA PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- erasure_requests (migration 002) already records requested_at / completed_at
-- without a foreign key, so the compliance record outlives the user row.
