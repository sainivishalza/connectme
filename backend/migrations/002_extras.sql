-- Tables required by the build steps but missing from the original spec schema.

CREATE TABLE swipes (
  actor_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  target_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  action     VARCHAR(10) NOT NULL CHECK (action IN ('like','pass','superlike')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (actor_id, target_id),
  CHECK (actor_id <> target_id)
);
CREATE INDEX idx_swipes_target ON swipes (target_id, action);

CREATE TABLE blocks (
  blocker_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  blocked_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (blocker_id, blocked_id),
  CHECK (blocker_id <> blocked_id)
);

CREATE TABLE reports (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  reporter_id UUID REFERENCES users(id) ON DELETE SET NULL,
  reported_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reason      VARCHAR(50) NOT NULL,
  details     TEXT,
  evidence    JSONB,                       -- reporter-supplied decrypted messages (E2E chats)
  status      VARCHAR(20) NOT NULL DEFAULT 'open' CHECK (status IN ('open','reviewing','actioned','dismissed')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_reports_status ON reports (status, created_at);

-- Append-only coin ledger. `ref` makes grants/spends idempotent (e.g. stripe event id).
CREATE TABLE coin_ledger (
  id         BIGSERIAL PRIMARY KEY,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  delta      INT NOT NULL CHECK (delta <> 0),
  reason     VARCHAR(30) NOT NULL,
  ref        VARCHAR(255) UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_coin_ledger_user ON coin_ledger (user_id);
CREATE VIEW coin_balances AS
  SELECT user_id, SUM(delta)::BIGINT AS balance FROM coin_ledger GROUP BY user_id;

CREATE TABLE gift_catalog (
  id          VARCHAR(30) PRIMARY KEY,
  name        VARCHAR(50) NOT NULL,
  price_coins INT NOT NULL CHECK (price_coins > 0),
  active      BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE hotel_board_posts (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  hotel_id   UUID NOT NULL REFERENCES hotels(id),
  checkin_id UUID NOT NULL REFERENCES hotel_checkins(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  message    VARCHAR(280) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_board_hotel ON hotel_board_posts (hotel_id, created_at DESC);

CREATE TABLE hotel_drinks (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  checkin_id UUID NOT NULL REFERENCES hotel_checkins(id) ON DELETE CASCADE,
  from_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  to_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Mutual reveal: photos/name unlock only when both match members have a row.
CREATE TABLE reveal_consents (
  match_id   UUID NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (match_id, user_id)
);

-- Email verification / password reset. Only a SHA-256 of the token is stored.
CREATE TABLE email_tokens (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose    VARCHAR(20) NOT NULL CHECK (purpose IN ('verify_email','reset_password')),
  token_hash BYTEA NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  used_at    TIMESTAMPTZ
);

CREATE TABLE group_invites (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id   UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  inviter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  invitee_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status     VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','declined')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (group_id, invitee_id)
);

-- Idempotent Stripe webhook handling.
CREATE TABLE stripe_events (
  id          VARCHAR(255) PRIMARY KEY,
  received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- GDPR/CCPA: erasure requests are honoured within 30 days.
CREATE TABLE erasure_requests (
  user_id      UUID PRIMARY KEY,
  requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  due_at       TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '30 days',
  completed_at TIMESTAMPTZ
);
