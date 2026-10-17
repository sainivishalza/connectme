-- Live streaming. A room is one broadcaster and many viewers (media flows
-- through LiveKit; this schema holds only lifecycle and gift accounting).
CREATE TABLE live_rooms (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  host_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      VARCHAR(80) NOT NULL,
  status     VARCHAR(10) NOT NULL DEFAULT 'live' CHECK (status IN ('live','ended')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ended_at   TIMESTAMPTZ
);
-- At most one live room per host.
CREATE UNIQUE INDEX idx_live_one_per_host ON live_rooms (host_id) WHERE status = 'live';
CREATE INDEX idx_live_status ON live_rooms (status, created_at DESC);

CREATE TABLE live_gifts (
  id             BIGSERIAL PRIMARY KEY,
  room_id        UUID NOT NULL REFERENCES live_rooms(id) ON DELETE CASCADE,
  from_user      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  gift_id        VARCHAR(30) NOT NULL REFERENCES gift_catalog(id),
  coins          INT NOT NULL CHECK (coins > 0),
  host_coins     INT NOT NULL CHECK (host_coins >= 0),
  platform_coins INT NOT NULL CHECK (platform_coins >= 0),
  request_id     VARCHAR(64) NOT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (host_coins + platform_coins = coins)
);
-- A retried request can never charge twice.
CREATE UNIQUE INDEX idx_live_gifts_request ON live_gifts (from_user, request_id);
CREATE INDEX idx_live_gifts_room ON live_gifts (room_id, created_at DESC);
