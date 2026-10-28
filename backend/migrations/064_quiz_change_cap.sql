-- Changing a quiz answer is limited per day (internal/quiz). Without a limit a
-- member could flip one answer at a time and watch another person's match
-- percentage move, which would reveal that person's private answers.
CREATE TABLE compat_changes (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_compat_changes_user ON compat_changes (user_id, changed_at DESC);
