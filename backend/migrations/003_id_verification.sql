-- is_verified = email verified. ID/liveness result is stored as a timestamp only;
-- raw ID images are never persisted.
ALTER TABLE users ADD COLUMN id_verified_at TIMESTAMPTZ;
