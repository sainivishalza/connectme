-- Human-reviewed photo ID check (F1.19) for deployments without a paid
-- identity-verification provider.
--
-- A member asks for a check and is given a random 4-digit code. They upload a
-- photo of an ID document and a selfie holding a paper with that code. A
-- moderator compares the faces, the code and the date of birth, then approves
-- or rejects. The images live in a private store only while the check is open:
-- they are deleted the moment a decision is made (or after a short expiry),
-- and what remains is the decision, who made it and when. Staff views of the
-- images are written to staff_audit_log.
CREATE TABLE id_checks (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status        VARCHAR(10) NOT NULL CHECK (status IN ('awaiting','pending','approved','rejected','expired')),
  code          CHAR(4) NOT NULL,
  expires_at    TIMESTAMPTZ NOT NULL,
  id_key        TEXT,
  selfie_key    TEXT,
  submitted_at  TIMESTAMPTZ,
  reviewed_by   UUID REFERENCES users(id) ON DELETE SET NULL,
  reviewed_at   TIMESTAMPTZ,
  reject_reason VARCHAR(20),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- one open check per member
CREATE UNIQUE INDEX idx_id_checks_open ON id_checks (user_id) WHERE status IN ('awaiting','pending');
CREATE INDEX idx_id_checks_queue ON id_checks (submitted_at) WHERE status = 'pending';
CREATE INDEX idx_id_checks_user ON id_checks (user_id, created_at DESC);
CREATE INDEX idx_id_checks_files ON id_checks (id) WHERE id_key IS NOT NULL OR selfie_key IS NOT NULL;
