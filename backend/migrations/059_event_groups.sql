-- Event groups: everyone who has a spot at a partner event can talk to the
-- others going, on a board that exists until the day after the event. Only
-- people with an RSVP can read or write; names show only for members in Open
-- mode; everything is deleted a day after the event ends.
CREATE TABLE event_messages (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body       VARCHAR(280) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_event_messages_event ON event_messages (event_id, created_at DESC);
