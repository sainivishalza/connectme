-- Posts: Nearby Feed / Moments (F2.10), Little Universe (F2.7) and Missed
-- Connections (F2.9). One table, three kinds:
--   moment   short public post shown to people nearby; the author appears the
--            way their profile mode allows (named when Open, anonymous when not)
--   universe anonymous post with a made-up nickname and no link to a profile
--   missed   "I think I saw someone at ...", visible only to people currently
--            checked in at the same venue, and deleted with the author's stay
-- Locations are snapped to a ~1 km grid before they are stored: a post never
-- holds anyone's exact position.
CREATE TABLE posts (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       VARCHAR(10) NOT NULL CHECK (kind IN ('moment','universe','missed')),
  body       VARCHAR(280) NOT NULL,
  alias      VARCHAR(40),
  location   GEOGRAPHY(POINT, 4326),
  hotel_id   UUID REFERENCES hotels(id),
  checkin_id UUID REFERENCES hotel_checkins(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL,
  CHECK ((kind IN ('moment','universe') AND location IS NOT NULL)
      OR (kind = 'missed' AND hotel_id IS NOT NULL AND checkin_id IS NOT NULL)),
  CHECK (kind <> 'universe' OR alias IS NOT NULL)
);
CREATE INDEX idx_posts_location ON posts USING GIST (location) WHERE kind IN ('moment','universe');
CREATE INDEX idx_posts_kind_created ON posts (kind, created_at DESC);
CREATE INDEX idx_posts_hotel ON posts (hotel_id, created_at DESC) WHERE kind = 'missed';
CREATE INDEX idx_posts_author ON posts (author_id, created_at DESC);
CREATE INDEX idx_posts_expires ON posts (expires_at);

CREATE TABLE post_likes (
  post_id    UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (post_id, user_id)
);

-- "That's me": someone at the venue answers a missed-connection post. The
-- author sees the claimant the way the claimant's mode allows and may accept,
-- which creates a match; nobody is told if they are declined.
CREATE TABLE missed_claims (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id     UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  claimant_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status      VARCHAR(10) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','declined')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (post_id, claimant_id)
);
CREATE INDEX idx_missed_claims_post ON missed_claims (post_id, created_at);
