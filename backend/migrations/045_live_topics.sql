-- A live room has a topic (so people can browse by interest) and the host can
-- pin one short line for everyone who joins.
ALTER TABLE live_rooms
  ADD COLUMN topic  VARCHAR(12) NOT NULL DEFAULT 'chat'
    CHECK (topic IN ('chat','music','talent','gaming','dating','wellness','learning')),
  ADD COLUMN pinned VARCHAR(120) NOT NULL DEFAULT '';
CREATE INDEX idx_live_rooms_topic ON live_rooms (topic) WHERE status = 'live';
