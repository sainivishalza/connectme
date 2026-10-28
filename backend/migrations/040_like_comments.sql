-- A short message sent along with a like or super like ("Your trail photo made
-- me smile"). Plain text on the server like a bio, because a match does not
-- exist yet to carry an encrypted message. It is shown to the person who was
-- liked, and after a match, only when the sender's profile is visible in full:
-- free text is the easiest way for an anonymous member to give themselves away.
ALTER TABLE swipes ADD COLUMN comment VARCHAR(140);
