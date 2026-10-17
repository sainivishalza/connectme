-- public_ref is the ONLY identifier other users ever see. It is rotated whenever
-- the owner changes mode, so an anonymous card cannot be linked to the same
-- person's earlier open-mode profile. user_id is never exposed.
ALTER TABLE profiles
  ADD COLUMN public_ref UUID NOT NULL DEFAULT gen_random_uuid(),
  ADD COLUMN prev_mode VARCHAR(20) CHECK (prev_mode IN ('open','incognito')),
  ADD COLUMN location_updated_at TIMESTAMPTZ;

CREATE UNIQUE INDEX idx_profiles_public_ref ON profiles (public_ref);
CREATE INDEX idx_profiles_photos ON profiles USING GIN (photos jsonb_path_ops);

ALTER TABLE profiles
  ADD CONSTRAINT profiles_gender_chk CHECK (gender IS NULL OR gender IN ('man','woman','nonbinary','other')),
  ADD CONSTRAINT profiles_looking_for_chk CHECK (looking_for IS NULL OR looking_for IN ('men','women','everyone'));
