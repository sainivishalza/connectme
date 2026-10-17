-- Public in-stream events (chat lines and gift announcements). They live and
-- die with the room: rooms are deleted 24 h after they end and these rows
-- cascade. Chat is public text in a public stream, so unlike direct messages
-- it is not end-to-end encrypted and is subject to text screening.
CREATE TABLE live_events (
  id         BIGSERIAL PRIMARY KEY,
  room_id    UUID NOT NULL REFERENCES live_rooms(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       VARCHAR(8) NOT NULL CHECK (kind IN ('chat','gift')),
  body       VARCHAR(200) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_live_events_room ON live_events (room_id, id);
