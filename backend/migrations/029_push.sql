-- Push notifications. A device row holds the provider's delivery token for one
-- install; tokens are unique (a token belongs to whoever registered it last,
-- so a shared phone never notifies the previous account). Preferences are per
-- kind and default to on. Nothing here stores notification content: payloads
-- are generic and built at send time.
CREATE TABLE push_devices (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform   VARCHAR(8) NOT NULL CHECK (platform IN ('android','ios','web')),
  token      VARCHAR(1024) NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_push_devices_user ON push_devices (user_id);
CREATE INDEX idx_push_devices_seen ON push_devices (last_seen);

CREATE TABLE push_prefs (
  user_id    UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  messages   BOOLEAN NOT NULL DEFAULT TRUE,
  matches    BOOLEAN NOT NULL DEFAULT TRUE,
  calls      BOOLEAN NOT NULL DEFAULT TRUE,
  bookings   BOOLEAN NOT NULL DEFAULT TRUE,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
