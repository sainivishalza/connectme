-- Boost visibility window.
ALTER TABLE profiles ADD COLUMN boosted_until TIMESTAMPTZ;

-- Everything created during a hotel stay hangs off the check-in, so deleting
-- the check-in row purges it (auto-delete 24h after checkout).
ALTER TABLE swipes ADD COLUMN checkin_id UUID REFERENCES hotel_checkins(id) ON DELETE CASCADE;
CREATE INDEX idx_swipes_checkin ON swipes (checkin_id) WHERE checkin_id IS NOT NULL;

-- One active check-in per user; one drink per sender/recipient per stay.
CREATE UNIQUE INDEX idx_active_checkin ON hotel_checkins (user_id) WHERE checked_out_at IS NULL;
CREATE UNIQUE INDEX idx_hotel_drinks_once ON hotel_drinks (checkin_id, from_id, to_id);
CREATE INDEX idx_drinks_to ON hotel_drinks (to_id);

-- Moderation: resolution audit.
ALTER TABLE reports
  ADD COLUMN resolved_by UUID REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN resolved_at TIMESTAMPTZ;
