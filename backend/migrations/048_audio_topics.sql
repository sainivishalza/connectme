-- Voice rooms (internal/audio) get a topic so people can find a conversation
-- they want to be in, anonymous or not.
ALTER TABLE audio_rooms
  ADD COLUMN topic VARCHAR(12) NOT NULL DEFAULT 'open'
    CHECK (topic IN ('open','love','stories','music','advice','nightowls','confess'));
