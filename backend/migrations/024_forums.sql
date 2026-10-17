-- Forums (F3.1): topic boards with threads and replies. Boards for dating
-- advice and travel are seeded; admins add city boards. Authors are shown by
-- name only while they are in Open mode, otherwise under a per-thread alias
-- derived at read time (never stored with the post).
CREATE TABLE forum_boards (
  slug        VARCHAR(40) PRIMARY KEY CHECK (slug ~ '^[a-z0-9-]{2,40}$'),
  title       VARCHAR(60) NOT NULL,
  description VARCHAR(200) NOT NULL DEFAULT '',
  kind        VARCHAR(10) NOT NULL CHECK (kind IN ('advice','travel','city')),
  position    INT NOT NULL DEFAULT 100,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO forum_boards (slug, title, description, kind, position) VALUES
  ('dating-advice', 'Dating advice', 'Ask, answer and swap stories about meeting people.', 'advice', 1),
  ('first-dates',   'First dates',   'Ideas, nerves and what actually worked.',            'advice', 2),
  ('travel-tips',   'Travel tips',   'Where to go, what to skip, how to stay safe.',       'travel', 3),
  ('solo-travel',   'Solo travel',   'Going it alone, and finding company on the way.',    'travel', 4);

CREATE TABLE forum_threads (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  board         VARCHAR(40) NOT NULL REFERENCES forum_boards(slug) ON DELETE CASCADE,
  author_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title         VARCHAR(120) NOT NULL,
  body          VARCHAR(2000) NOT NULL,
  replies       INT NOT NULL DEFAULT 0,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_reply_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_forum_threads_board ON forum_threads (board, last_reply_at DESC);
CREATE INDEX idx_forum_threads_author ON forum_threads (author_id, created_at DESC);

CREATE TABLE forum_replies (
  id         BIGSERIAL PRIMARY KEY,
  thread_id  UUID NOT NULL REFERENCES forum_threads(id) ON DELETE CASCADE,
  author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body       VARCHAR(1000) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_forum_replies_thread ON forum_replies (thread_id, id);
CREATE INDEX idx_forum_replies_author ON forum_replies (author_id, created_at DESC);
