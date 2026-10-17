-- Audio rooms: voice-only LiveKit rooms. The host promotes listeners to
-- speakers. Anonymous lounges show made-up aliases and never names or refs.
CREATE TABLE audio_rooms (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  host_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      VARCHAR(80) NOT NULL,
  anonymous  BOOLEAN NOT NULL DEFAULT FALSE,
  status     VARCHAR(10) NOT NULL DEFAULT 'live' CHECK (status IN ('live','ended')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ended_at   TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_audio_one_per_host ON audio_rooms (host_id) WHERE status = 'live';
CREATE INDEX idx_audio_status ON audio_rooms (status, created_at DESC);

-- id doubles as the LiveKit identity so other participants never see user ids.
-- alias is the label others see: a real display name only in named rooms for
-- Open-mode members, a made-up alias otherwise.
CREATE TABLE audio_members (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id     UUID NOT NULL REFERENCES audio_rooms(id) ON DELETE CASCADE,
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role        VARCHAR(10) NOT NULL CHECK (role IN ('host','speaker','listener')),
  alias       VARCHAR(80) NOT NULL,
  hand_raised BOOLEAN NOT NULL DEFAULT FALSE,
  joined_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  left_at     TIMESTAMPTZ,
  kicked      BOOLEAN NOT NULL DEFAULT FALSE,
  UNIQUE (room_id, user_id)
);
CREATE INDEX idx_audio_members_room ON audio_members (room_id) WHERE left_at IS NULL;
