-- Video scanning and human review. Every upload is scanned before it is
-- served: "allow" goes live, "flag" is held for a moderator, "block" is
-- refused (and queued as an automatic report). A scanner that errors fails
-- closed: the clip is held, never published unreviewed.
ALTER TABLE videos
  ADD COLUMN status      VARCHAR(8) NOT NULL DEFAULT 'live' CHECK (status IN ('live','held')),
  ADD COLUMN scan_note   VARCHAR(200) NOT NULL DEFAULT '',
  ADD COLUMN scanner     VARCHAR(20) NOT NULL DEFAULT '',
  ADD COLUMN reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN reviewed_at TIMESTAMPTZ;
CREATE INDEX idx_videos_held ON videos (created_at) WHERE status = 'held';
