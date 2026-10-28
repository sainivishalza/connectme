-- Comments on posts (internal/social). A comment lives and dies with its post
-- and is visible to exactly the people who can see the post. Comments are not
-- allowed on Missed Connections posts, which are deliberately one-to-one.
CREATE TABLE post_comments (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id    UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body       VARCHAR(280) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_post_comments_post ON post_comments (post_id, created_at, id);
CREATE INDEX idx_post_comments_author ON post_comments (author_id, created_at DESC);
