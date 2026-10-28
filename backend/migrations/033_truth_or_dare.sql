-- Turn-based Truth or Dare (F2.5 / F2.8). One game per match: whose turn it is,
-- whether they are choosing truth or dare or answering the prompt they drew,
-- and how many skips each person has left. Prompts come from the curated bank
-- in code; answers go in the (end-to-end encrypted) chat and are never stored
-- here. The game disappears with the match.
CREATE TABLE td_games (
  match_id   UUID PRIMARY KEY REFERENCES matches(id) ON DELETE CASCADE,
  turn_user  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  phase      VARCHAR(8) NOT NULL DEFAULT 'choose' CHECK (phase IN ('choose','answer')),
  kind       VARCHAR(5) CHECK (kind IN ('truth','dare')),
  prompt     VARCHAR(300),
  round      INT NOT NULL DEFAULT 1,
  skips      JSONB NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK ((phase = 'choose' AND kind IS NULL AND prompt IS NULL) OR (phase = 'answer' AND kind IS NOT NULL AND prompt IS NOT NULL))
);
