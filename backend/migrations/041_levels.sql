-- Levels (internal/levels) are derived from the gift ledgers. These two switches
-- decide what other people may see: the Charm level (from gifts received) is
-- shown by default on a visible profile; the Supporter level (from gifts sent)
-- is private unless the member turns it on.
ALTER TABLE profiles
  ADD COLUMN show_charm     BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN show_supporter BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX idx_live_rooms_host ON live_rooms (host_id);
CREATE INDEX idx_gifts_from ON gifts_sent (from_user, created_at DESC);
CREATE INDEX idx_live_gifts_from ON live_gifts (from_user, created_at DESC);
