-- A second group game next to Would you rather: "Most likely to". It shares
-- the one-round-at-a-time rule and the polls table; a vote here names the
-- member voted for. Only totals per member are ever shown, never who voted.
ALTER TABLE group_polls ADD COLUMN kind VARCHAR(4) NOT NULL DEFAULT 'wyr' CHECK (kind IN ('wyr','mlt'));
ALTER TABLE group_poll_votes ADD COLUMN voted_for UUID REFERENCES users(id) ON DELETE CASCADE;
