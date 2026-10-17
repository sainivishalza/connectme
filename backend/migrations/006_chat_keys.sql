-- Signal-protocol key directory. The server only distributes PUBLIC keys; it
-- never holds private keys and can never read message contents.
CREATE TABLE key_bundles (
  user_id           UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  registration_id   INT  NOT NULL,
  identity_key      TEXT NOT NULL,
  signed_prekey_id  INT  NOT NULL,
  signed_prekey     TEXT NOT NULL,
  signed_prekey_sig TEXT NOT NULL,
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE one_time_prekeys (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key_id     INT  NOT NULL,
  public_key TEXT NOT NULL,
  PRIMARY KEY (user_id, key_id)
);
