-- Co-hosts: up to two people the host invites to share a live room. A co-host
-- can broadcast, mute and remove viewers and pin the line, but cannot end the
-- room or add other co-hosts. They exist only for that room.
CREATE TABLE live_cohosts (
  room_id    UUID NOT NULL REFERENCES live_rooms(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (room_id, user_id)
);
