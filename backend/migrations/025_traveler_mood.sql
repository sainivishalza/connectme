-- Mood-based hotel discovery (F3.2): during a stay a guest can say what kind of
-- traveller they are tonight, and filter the hotel feed by it. The mood hangs
-- off the check-in, so it disappears with the stay.
ALTER TABLE hotel_checkins
  ADD COLUMN traveler_mood VARCHAR(10) CHECK (traveler_mood IN ('business','company','guide'));
