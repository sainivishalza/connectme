-- Asking to join a live room (a viewer raises a hand) and being invited up by
-- the host. Accepting either one makes the person a co-host (live_cohosts), so
-- they can publish video and audio. Rows are short-lived and only ever hold who
-- asked and who was asked.
CREATE TABLE live_stage_requests (
  room_id    UUID NOT NULL REFERENCES live_rooms(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       VARCHAR(10) NOT NULL CHECK (kind IN ('request','invite')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (room_id, user_id)
);
CREATE INDEX idx_live_stage_room ON live_stage_requests (room_id, kind, created_at);
