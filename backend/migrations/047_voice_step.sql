-- Staged reveal for anonymous matches: before photos and names, two people can
-- agree to hear each other's voice intro. Each side consents on their own; the
-- voice unlocks only when both have. The full reveal (reveal_consents) is a
-- separate, later step.
CREATE TABLE voice_consents (
  match_id   UUID NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (match_id, user_id)
);
