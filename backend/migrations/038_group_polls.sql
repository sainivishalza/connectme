-- Group "Would you rather" for a matched group chat. A round is one question
-- from a curated bank; every member of either group votes A or B and sees only
-- the totals, never who voted for what. Nothing is kept beyond the totals and
-- a member's own vote, and the rounds disappear with the group match.
CREATE TABLE group_polls (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  group_match_id UUID NOT NULL REFERENCES group_matches(id) ON DELETE CASCADE,
  question       INT  NOT NULL CHECK (question >= 0),
  started_by     UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  closed_at      TIMESTAMPTZ
);
-- one running round per chat
CREATE UNIQUE INDEX idx_group_polls_open ON group_polls (group_match_id) WHERE closed_at IS NULL;
CREATE INDEX idx_group_polls_recent ON group_polls (group_match_id, created_at DESC);

CREATE TABLE group_poll_votes (
  poll_id  UUID NOT NULL REFERENCES group_polls(id) ON DELETE CASCADE,
  user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  choice   CHAR(1) NOT NULL CHECK (choice IN ('a','b')),
  voted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (poll_id, user_id)
);
