-- One-to-one voice and video calls between matched people (F1.8). Media goes
-- through LiveKit; this table is only the ringing/lifecycle state.
CREATE TABLE calls (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  match_id    UUID NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  caller_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  callee_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  video       BOOLEAN NOT NULL DEFAULT FALSE,
  status      VARCHAR(10) NOT NULL DEFAULT 'ringing'
              CHECK (status IN ('ringing','active','ended','declined','missed')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  answered_at TIMESTAMPTZ,
  ended_at    TIMESTAMPTZ,
  CHECK (caller_id <> callee_id)
);
-- Looking up "my current call" and "calls ringing for me" must be cheap.
CREATE INDEX idx_calls_callee_ringing ON calls (callee_id, created_at DESC) WHERE status = 'ringing';
CREATE INDEX idx_calls_caller_open ON calls (caller_id) WHERE status IN ('ringing','active');
CREATE INDEX idx_calls_callee_open ON calls (callee_id) WHERE status IN ('ringing','active');
