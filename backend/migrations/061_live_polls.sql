-- Live polls: a host (or co-host) asks the room a question with 2 to 4 answers;
-- viewers vote once; the totals are shown live to everyone. One poll runs at a
-- time and it disappears with the room.
CREATE TABLE live_polls (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id    UUID NOT NULL REFERENCES live_rooms(id) ON DELETE CASCADE,
  question   VARCHAR(80) NOT NULL,
  options    TEXT[] NOT NULL CHECK (array_length(options, 1) BETWEEN 2 AND 4),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  closed_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_live_polls_open ON live_polls (room_id) WHERE closed_at IS NULL;
CREATE TABLE live_poll_votes (
  poll_id UUID NOT NULL REFERENCES live_polls(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  choice  SMALLINT NOT NULL CHECK (choice BETWEEN 0 AND 3),
  PRIMARY KEY (poll_id, user_id)
);
