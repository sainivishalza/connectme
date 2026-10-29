-- A scanner match on an uploaded image. The image itself is never stored.
-- user_id is kept (SET NULL) if the account is later erased, so the record
-- survives for legal reporting. Retention is a legal decision (docs/CSAM_RUNBOOK.md).
CREATE TABLE csam_hits (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        UUID REFERENCES users(id) ON DELETE SET NULL,
  content_sha256 CHAR(64) NOT NULL,
  source         VARCHAR(30) NOT NULL,
  provider_ref   TEXT NOT NULL DEFAULT '',
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_csam_hits_user ON csam_hits (user_id, created_at);
