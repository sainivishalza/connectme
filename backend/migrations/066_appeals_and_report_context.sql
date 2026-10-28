-- Staff tools (internal/moderation).
-- 1. A report can point at the thing it was about, so staff can see every
--    report about one masked room together (the room itself is deleted soon
--    after it ends, the report keeps the lines).
ALTER TABLE reports ADD COLUMN ref_kind VARCHAR(20), ADD COLUMN ref_id UUID;
CREATE INDEX idx_reports_ref ON reports (ref_kind, ref_id) WHERE ref_id IS NOT NULL;

-- 2. Appeals. A banned member cannot sign in, so they ask by proving they own
--    the account (email and password); staff read the message and either lift
--    the ban or turn it down with a reason. One open appeal per member.
CREATE TABLE appeals (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  message       VARCHAR(1000) NOT NULL,
  status        VARCHAR(10) NOT NULL DEFAULT 'open' CHECK (status IN ('open','upheld','lifted')),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  decided_at    TIMESTAMPTZ,
  decided_by    UUID REFERENCES users(id) ON DELETE SET NULL,
  decision_note VARCHAR(300) NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX idx_appeals_open ON appeals (user_id) WHERE status = 'open';
