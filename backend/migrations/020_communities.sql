-- Nearby groups (F2.6): interest communities near you and automatic
-- "guests at this hotel" communities. Members post to a members-only feed
-- (kind 'community' in the posts table).
--
-- Hotel communities follow the stay: membership is bound to the member's
-- check-in and a guest's posts there are bound to it too, so both disappear
-- with the stay's 24-hour retention like everything else from a hotel.
CREATE TABLE communities (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name        VARCHAR(60) NOT NULL,
  description VARCHAR(200) NOT NULL DEFAULT '',
  kind        VARCHAR(10) NOT NULL CHECK (kind IN ('interest','hotel')),
  tag         VARCHAR(30),
  hotel_id    UUID REFERENCES hotels(id) ON DELETE CASCADE,
  center      GEOGRAPHY(POINT, 4326),            -- snapped to a ~1 km grid
  radius_km   SMALLINT NOT NULL DEFAULT 25 CHECK (radius_km BETWEEN 1 AND 100),
  created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK ((kind = 'interest' AND center IS NOT NULL AND hotel_id IS NULL)
      OR (kind = 'hotel' AND hotel_id IS NOT NULL))
);
CREATE UNIQUE INDEX uniq_hotel_community ON communities (hotel_id) WHERE kind = 'hotel';
CREATE INDEX idx_communities_center ON communities USING GIST (center) WHERE kind = 'interest';

CREATE TABLE community_members (
  community_id UUID NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role         VARCHAR(8) NOT NULL DEFAULT 'member' CHECK (role IN ('owner','member')),
  joined_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  -- Hotel communities only: the membership ends with this stay.
  checkin_id   UUID REFERENCES hotel_checkins(id) ON DELETE CASCADE,
  PRIMARY KEY (community_id, user_id)
);
CREATE INDEX idx_community_members_user ON community_members (user_id);

-- Posts may now belong to a community.
ALTER TABLE posts ADD COLUMN community_id UUID REFERENCES communities(id) ON DELETE CASCADE;
ALTER TABLE posts DROP CONSTRAINT posts_kind_check;
ALTER TABLE posts DROP CONSTRAINT posts_check;
ALTER TABLE posts DROP CONSTRAINT posts_check1;
ALTER TABLE posts ADD CONSTRAINT posts_kind_check CHECK (kind IN ('moment','universe','missed','community'));
ALTER TABLE posts ADD CONSTRAINT posts_check CHECK (
     (kind IN ('moment','universe') AND location IS NOT NULL)
  OR (kind = 'missed' AND hotel_id IS NOT NULL AND checkin_id IS NOT NULL)
  OR (kind = 'community' AND community_id IS NOT NULL));
ALTER TABLE posts ADD CONSTRAINT posts_check1 CHECK (kind <> 'universe' OR alias IS NOT NULL);
CREATE INDEX idx_posts_community ON posts (community_id, created_at DESC) WHERE kind = 'community';
