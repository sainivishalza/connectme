-- Chapter-based profiles (F3.3): up to five short narrative chapters ("The
-- year I moved abroad") on a profile. Chapters are personal stories, so they
-- follow the photo visibility rule exactly (users.SeesFull) and are never part
-- of an anonymous profile.
CREATE TABLE profile_chapters (
  id         BIGSERIAL PRIMARY KEY,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  position   SMALLINT NOT NULL CHECK (position BETWEEN 1 AND 5),
  title      VARCHAR(60) NOT NULL,
  body       VARCHAR(600) NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (user_id, position)
);
