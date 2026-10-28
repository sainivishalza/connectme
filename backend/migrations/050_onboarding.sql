-- Guided onboarding: set when a member finishes (or skips) the welcome steps so
-- the app does not show them again. Everyone who already has a profile counts
-- as onboarded.
ALTER TABLE profiles ADD COLUMN onboarded_at TIMESTAMPTZ;
UPDATE profiles SET onboarded_at = NOW();
