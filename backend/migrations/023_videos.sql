-- Short videos (F2.3) and video profile reels (F2.11).
--
-- A short is a public clip in the video feed; only Open-mode members can post
-- one (a face and a voice identify a person). A reel is the clip on a profile
-- and follows the SAME visibility rule as photos: playable only by someone who
-- may see the owner's full profile. The file lives in the media store under
-- blob_id and is served through short-lived signed URLs.
CREATE TABLE videos (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       VARCHAR(5) NOT NULL CHECK (kind IN ('short','reel')),
  blob_id    UUID NOT NULL,
  mime       VARCHAR(20) NOT NULL CHECK (mime IN ('video/mp4','video/quicktime')),
  seconds    SMALLINT NOT NULL CHECK (seconds BETWEEN 1 AND 30),
  bytes      INT NOT NULL CHECK (bytes > 0),
  caption    VARCHAR(150) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- One reel per profile.
CREATE UNIQUE INDEX idx_videos_one_reel ON videos (owner_id) WHERE kind = 'reel';
CREATE INDEX idx_videos_feed ON videos (created_at DESC) WHERE kind = 'short';
CREATE INDEX idx_videos_owner ON videos (owner_id, created_at DESC);

CREATE TABLE video_likes (
  video_id   UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (video_id, user_id)
);
