ALTER TABLE subscriptions
  ADD COLUMN stripe_subscription_id VARCHAR(255) UNIQUE,
  ADD COLUMN cancel_at_period_end   BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW();
CREATE INDEX idx_subs_active ON subscriptions (user_id, status);

-- stripe_id holds the PaymentIntent id for coin purchases; coins is what was granted.
ALTER TABLE payments ADD COLUMN coins INT;
CREATE UNIQUE INDEX idx_payments_stripe_id ON payments (stripe_id) WHERE stripe_id IS NOT NULL;

ALTER TABLE profiles ADD COLUMN invisible BOOLEAN NOT NULL DEFAULT FALSE;

-- Boost history: free weekly boosts are counted from here.
CREATE TABLE boost_log (
  id         BIGSERIAL PRIMARY KEY,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source     VARCHAR(10) NOT NULL CHECK (source IN ('coins','perk')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_boost_log_user ON boost_log (user_id, created_at DESC);

-- Gifts live in a table of their own; when sent inside a hotel stay they hang
-- off the check-in and are deleted with it.
CREATE TABLE gifts_sent (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  from_user  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  to_user    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  gift_id    VARCHAR(30) NOT NULL REFERENCES gift_catalog(id),
  coins      INT NOT NULL,
  request_id VARCHAR(64),
  checkin_id UUID REFERENCES hotel_checkins(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_gifts_request ON gifts_sent (from_user, request_id) WHERE request_id IS NOT NULL;
CREATE INDEX idx_gifts_to ON gifts_sent (to_user, created_at DESC);

INSERT INTO gift_catalog (id, name, price_coins) VALUES
  ('rose',         'Rose',          10),
  ('drink',        'Drink',         15),
  ('champagne',    'Champagne',     50),
  ('room_service', 'Room service',  80)
ON CONFLICT (id) DO NOTHING;
