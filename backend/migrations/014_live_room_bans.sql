-- Per-room moderation by the host: a muted viewer can watch but not chat; a
-- kicked viewer cannot rejoin. Rows vanish with the room.
CREATE TABLE live_room_bans (
  room_id    UUID NOT NULL REFERENCES live_rooms(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       VARCHAR(5) NOT NULL CHECK (kind IN ('mute','kick')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (room_id, user_id)
);
