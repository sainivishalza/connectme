-- A short video can carry a sound from the built-in library (internal/sounds).
-- The loop is played alongside the clip by the app; nothing is mixed into the
-- video file, and the library is synthesised so there is nothing to license.
ALTER TABLE videos ADD COLUMN sound_id VARCHAR(20) NOT NULL DEFAULT '';
