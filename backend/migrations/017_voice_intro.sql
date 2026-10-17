-- Voice intro clip (F2.12): a short spoken introduction on a profile.
-- It follows the SAME visibility rule as photos: playable only by someone who
-- may see the owner's full profile (a voice identifies a person), never for an
-- anonymous profile. The audio lives in the media store under voice_id.
ALTER TABLE profiles
  ADD COLUMN voice_id      UUID,
  ADD COLUMN voice_seconds SMALLINT CHECK (voice_seconds BETWEEN 1 AND 30),
  ADD COLUMN voice_mime    VARCHAR(20),
  ADD CONSTRAINT profiles_voice_consistent CHECK (
    (voice_id IS NULL AND voice_seconds IS NULL AND voice_mime IS NULL)
    OR (voice_id IS NOT NULL AND voice_seconds IS NOT NULL AND voice_mime IS NOT NULL));
