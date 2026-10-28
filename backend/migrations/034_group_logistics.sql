-- Group-date logistics fee (PRD: a small per-person fee for AI-coordinated
-- logistics). Anyone in either group of a scheduled date can pay once, in
-- coins, to unlock the coordinated plan for that date: venue ideas, icebreakers
-- and a step-by-step outing plan. The plan is generated once per date and
-- shared by everyone who paid. A cancelled date refunds everyone.
CREATE TABLE group_date_plans (
  date_id      UUID PRIMARY KEY REFERENCES group_dates(id) ON DELETE CASCADE,
  plan         JSONB NOT NULL,
  generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE group_date_logistics (
  date_id    UUID NOT NULL REFERENCES group_dates(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  coins      INT NOT NULL CHECK (coins > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (date_id, user_id)
);
