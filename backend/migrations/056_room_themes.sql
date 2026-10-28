-- A live room can have a look: a colour theme the host picks, shown to viewers
-- as the tint behind the chat and the list card. Decoration only.
ALTER TABLE live_rooms
  ADD COLUMN theme VARCHAR(10) NOT NULL DEFAULT 'plain'
    CHECK (theme IN ('plain','sunset','neon','forest','ocean','velvet'));
