-- Investor pledges (internal/investors). A member pledges an amount; money is
-- received outside the app (bank transfer) and the owner marks the pledge
-- confirmed. Only confirmed pledges count. Ownership is derived, never stored:
-- the founder holds 51% and the confirmed money shares the remaining 49% in
-- proportion to what each person put in, so every new confirmation dilutes
-- everyone else's percentage.
CREATE TABLE investments (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  amount_cents BIGINT NOT NULL CHECK (amount_cents BETWEEN 100 AND 10000000),
  status       VARCHAR(10) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','cancelled')),
  show_name    BOOLEAN NOT NULL DEFAULT TRUE,
  reference    VARCHAR(60) NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  decided_at   TIMESTAMPTZ,
  decided_by   UUID REFERENCES users(id)
);
CREATE INDEX idx_investments_user ON investments (user_id, created_at DESC);
CREATE INDEX idx_investments_status ON investments (status, created_at);
