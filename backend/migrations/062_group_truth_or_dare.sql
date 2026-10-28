-- Truth or Dare for more than two people, and Masked Truth rooms.
--
-- group_td is the turn engine: players take turns in a fixed order; the player
-- whose turn it is picks truth or dare, gets a prompt from the curated bank,
-- then finishes (done, or skip: two skips each). The same engine runs in two
-- places: a matched group chat (answers go in the end-to-end encrypted chat and
-- are never stored) and a masked room (below).
CREATE TABLE group_td (
  room_id    UUID PRIMARY KEY,
  play_order UUID[] NOT NULL,
  turn_user  UUID NOT NULL,
  phase      VARCHAR(8) NOT NULL DEFAULT 'choose' CHECK (phase IN ('choose','answer')),
  kind       VARCHAR(5) CHECK (kind IN ('truth','dare')),
  prompt     VARCHAR(300),
  round      INT NOT NULL DEFAULT 1,
  skips      JSONB NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Masked rooms: 3 to 5 strangers, all under made-up names, for about 25
-- minutes. There is no chat, so a truth is answered in a short screened text
-- that everyone in the room sees; nothing is end-to-end encrypted here and the
-- screen says so. Rooms and their answers are deleted an hour after they end.
CREATE TABLE masked_waiting (
  user_id    UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE masked_rooms (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ends_at    TIMESTAMPTZ NOT NULL
);
CREATE TABLE masked_members (
  room_id UUID NOT NULL REFERENCES masked_rooms(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  alias   VARCHAR(40) NOT NULL,
  left_at TIMESTAMPTZ,
  PRIMARY KEY (room_id, user_id)
);
CREATE INDEX idx_masked_members_user ON masked_members (user_id) WHERE left_at IS NULL;
CREATE TABLE masked_answers (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id    UUID NOT NULL REFERENCES masked_rooms(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       VARCHAR(5) NOT NULL,
  prompt     VARCHAR(300) NOT NULL,
  body       VARCHAR(200) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_masked_answers_room ON masked_answers (room_id, created_at);
