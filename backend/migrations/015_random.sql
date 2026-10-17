-- Anonymous random matching (F1.6).
--
-- A random pairing is an ordinary row in `matches` (so the existing end-to-end
-- encrypted chat, reveal consent, block/report and retention all apply) with
-- origin='random', mode forced to incognito, and an expiry. If BOTH people
-- choose to keep chatting before it expires, expires_at is cleared and it
-- becomes a normal incognito match; otherwise it is ended and its messages
-- are purged by the chat retention sweeper.
ALTER TABLE matches
  ADD COLUMN origin     VARCHAR(10) NOT NULL DEFAULT 'swipe' CHECK (origin IN ('swipe','random')),
  ADD COLUMN expires_at TIMESTAMPTZ,
  ADD COLUMN kept_by    UUID[] NOT NULL DEFAULT '{}';

CREATE INDEX idx_matches_random_expiry ON matches (expires_at)
  WHERE origin = 'random' AND status = 'matched' AND expires_at IS NOT NULL;

-- People currently waiting for a partner. Rows are short-lived (the service
-- drops entries older than 90 s) and vanish with the account.
CREATE TABLE random_waiting (
  user_id   UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_random_waiting_joined ON random_waiting (joined_at);
