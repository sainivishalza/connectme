-- One one-time prekey per requester per conversation per day. Without this a
-- matched person could fetch a victim's key bundle in a loop and use up all of
-- their one-time prekeys, weakening the first message of every later chat. A
-- repeat fetch still gets the identity and signed prekey (X3DH works without a
-- one-time prekey); it just does not consume another.
CREATE TABLE prekey_fetches (
  match_id   UUID NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  requester  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (match_id, requester)
);
