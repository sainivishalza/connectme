-- Social games (F2.5). Truth, dare and icebreaker prompts come from a curated
-- bank in the code and need no storage. The couple quiz is a session between
-- the two people of a match: each answers the same questions privately, and
-- the other person's answers are revealed only once BOTH have finished.
--
-- Quiz answers are low-sensitivity multiple-choice preferences, stored on the
-- server (not end-to-end encrypted) so both devices can be compared; they are
-- deleted with the match and with either account.
CREATE TABLE quiz_sessions (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  match_id   UUID NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  set_id     VARCHAR(30) NOT NULL,
  started_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_quiz_sessions_match ON quiz_sessions (match_id, created_at DESC);

CREATE TABLE quiz_answers (
  session_id UUID NOT NULL REFERENCES quiz_sessions(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  question   SMALLINT NOT NULL CHECK (question >= 0),
  choice     SMALLINT NOT NULL CHECK (choice >= 0),
  answered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (session_id, user_id, question)
);
