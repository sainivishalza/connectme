-- Safety toolkit (internal/safety): trusted contacts and date plans with a
-- check-in. A member names someone they trust; that person must confirm the
-- address (so nobody can use the app to send mail to a stranger). A date plan
-- tells that contact where and when, and what time the member promises to
-- check in by. If the check-in never comes, the contact is emailed. Contacts
-- and plans are private to the member and deleted with the account.
CREATE TABLE trusted_contacts (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         VARCHAR(60) NOT NULL,
  email        VARCHAR(254) NOT NULL,
  token_hash   BYTEA NOT NULL,
  confirmed_at TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (user_id, email)
);
CREATE UNIQUE INDEX idx_trusted_contacts_token ON trusted_contacts (token_hash);

CREATE TABLE date_plans (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  contact_id  UUID NOT NULL REFERENCES trusted_contacts(id) ON DELETE CASCADE,
  match_id    UUID REFERENCES matches(id) ON DELETE SET NULL,
  place       VARCHAR(120) NOT NULL,
  meet_at     TIMESTAMPTZ NOT NULL,
  checkin_by  TIMESTAMPTZ NOT NULL,
  who         VARCHAR(60) NOT NULL DEFAULT '',   -- the other person's name, only if the member chose to share it
  extensions  SMALLINT NOT NULL DEFAULT 0,
  reminded    BOOLEAN NOT NULL DEFAULT FALSE,
  status      VARCHAR(10) NOT NULL DEFAULT 'active' CHECK (status IN ('active','safe','alerted','cancelled')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  alerted_at  TIMESTAMPTZ
);
CREATE INDEX idx_date_plans_due ON date_plans (checkin_by) WHERE status = 'active';
CREATE INDEX idx_date_plans_user ON date_plans (user_id, created_at DESC);
