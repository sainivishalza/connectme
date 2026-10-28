-- A trusted contact can also be reached by text message (internal/safety).
-- The number is only used after the contact has read a one-time code to the
-- member, so nobody can have texts sent to a stranger's number.
ALTER TABLE trusted_contacts
  ADD COLUMN phone                VARCHAR(20),
  ADD COLUMN phone_confirmed_at   TIMESTAMPTZ,
  ADD COLUMN phone_code_hash      BYTEA,
  ADD COLUMN phone_code_expires   TIMESTAMPTZ,
  ADD COLUMN phone_attempts       SMALLINT NOT NULL DEFAULT 0,
  ADD COLUMN phone_sends          SMALLINT NOT NULL DEFAULT 0,
  ADD COLUMN phone_sends_since    TIMESTAMPTZ;
