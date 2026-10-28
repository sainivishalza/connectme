-- Follow a member whose profile you can see in full (a host, a poster). The
-- follower list is private to the person being followed; the count is public
-- on a visible profile. Following never reveals an anonymous member: it is only
-- offered on profiles shown in full, and an anonymous follower is shown to the
-- person they follow as the same anonymous card as anywhere else.
CREATE TABLE follows (
  follower_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  followee_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (follower_id, followee_id),
  CHECK (follower_id <> followee_id)
);
CREATE INDEX idx_follows_followee ON follows (followee_id, created_at DESC);

-- Notification choice for "someone you follow is live".
ALTER TABLE push_prefs ADD COLUMN live BOOLEAN NOT NULL DEFAULT TRUE;
