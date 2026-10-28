-- Compatibility quiz (internal/quiz). A member answers curated multiple-choice
-- questions about values and daily life; two people's quiz match is the share
-- of questions they both answered where they chose the same option. Answers are
-- private: nobody sees another person's choices, only a rounded percentage once
-- at least 8 questions are shared.
CREATE TABLE compat_answers (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  qid        VARCHAR(30) NOT NULL,
  choice     SMALLINT NOT NULL CHECK (choice BETWEEN 0 AND 3),
  answered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, qid)
);
CREATE INDEX idx_compat_answers_q ON compat_answers (qid, choice);
