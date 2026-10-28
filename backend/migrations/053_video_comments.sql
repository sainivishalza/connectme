-- Comments on short videos (internal/videos). Shorts are posted only by members
-- shown in full, so commenters are named only if they are too; everyone else is
-- "Someone". A comment lives and dies with its video.
CREATE TABLE video_comments (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  video_id   UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
  author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body       VARCHAR(280) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_video_comments_video ON video_comments (video_id, created_at, id);
CREATE INDEX idx_video_comments_author ON video_comments (author_id, created_at DESC);
