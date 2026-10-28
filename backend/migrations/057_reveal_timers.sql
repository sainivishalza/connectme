-- Reveal timers: in an anonymous match one person proposes "let's reveal in N
-- hours"; if the other agrees, both consents to the full reveal are recorded
-- automatically at that time. Either person can cancel until then. A proposal
-- nobody answers lapses after a day.
CREATE TABLE reveal_schedules (
  match_id    UUID PRIMARY KEY REFERENCES matches(id) ON DELETE CASCADE,
  proposed_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reveal_at   TIMESTAMPTZ NOT NULL,
  accepted_by UUID REFERENCES users(id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_reveal_schedules_due ON reveal_schedules (reveal_at) WHERE accepted_by IS NOT NULL;
