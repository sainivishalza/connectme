-- Weekly summary email (internal/digest). Opt-in, off by default. It carries
-- counts only (no names, no message text) and one link to switch it off.
ALTER TABLE users
  ADD COLUMN weekly_digest   BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN last_digest_at  TIMESTAMPTZ;
CREATE INDEX idx_users_digest ON users (last_digest_at) WHERE weekly_digest;
