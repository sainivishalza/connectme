-- Core schema. Deviations from the original spec are listed in docs/DECISIONS.md.
-- Chat messages are NOT stored in Postgres (MongoDB, ciphertext only).

CREATE TABLE users (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email         VARCHAR(255) NOT NULL,
  phone         VARCHAR(20),
  password_hash VARCHAR(255) NOT NULL,
  date_of_birth DATE NOT NULL,
  role          VARCHAR(20) NOT NULL DEFAULT 'user' CHECK (role IN ('user','moderator','admin')),
  is_verified   BOOLEAN NOT NULL DEFAULT FALSE,
  is_banned     BOOLEAN NOT NULL DEFAULT FALSE,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
  -- 18+ is enforced server-side at registration (a CHECK on CURRENT_DATE is not
  -- immutable and breaks dump/restore).
);
CREATE UNIQUE INDEX idx_users_email_lower ON users (LOWER(email));

CREATE TABLE profiles (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  display_name VARCHAR(50),
  bio          TEXT,
  intro        VARCHAR(140),                 -- one-line intro shown in incognito
  gender       VARCHAR(20),
  looking_for  VARCHAR(20),
  mode         VARCHAR(20) NOT NULL DEFAULT 'open' CHECK (mode IN ('open','incognito','hotel')),
  photos       JSONB NOT NULL DEFAULT '[]',
  tags         TEXT[] NOT NULL DEFAULT '{}',
  location     GEOGRAPHY(POINT, 4326),       -- exact point: server-side only, never serialized
  mood         VARCHAR(50),
  last_active  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE hotels (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name       VARCHAR(255) NOT NULL,
  address    TEXT,
  geofence   GEOGRAPHY(POLYGON, 4326) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE hotel_checkins (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  hotel_id       UUID NOT NULL REFERENCES hotels(id),
  checked_in_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  checked_out_at TIMESTAMPTZ,
  -- Set at check-in to the max-stay fallback, moved to checkout+24h on checkout.
  auto_delete_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE matches (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user1_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  user2_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status     VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','matched','rejected')),
  mode       VARCHAR(20) NOT NULL DEFAULT 'open' CHECK (mode IN ('open','incognito','hotel')),
  -- Deleting a check-in cascades to every hotel-bound match (auto-delete).
  checkin_id UUID REFERENCES hotel_checkins(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT matches_ordered CHECK (user1_id < user2_id),
  CONSTRAINT matches_unique UNIQUE (user1_id, user2_id)
);

CREATE TABLE groups (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name       VARCHAR(100),
  type       VARCHAR(20) NOT NULL CHECK (type IN ('double','group')),
  activity   VARCHAR(50),
  created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE group_members (
  group_id  UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (group_id, user_id)
);

-- Financial records are retained after account erasure (user_id nulled).
CREATE TABLE payments (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID REFERENCES users(id) ON DELETE SET NULL,
  type         VARCHAR(20) NOT NULL CHECK (type IN ('subscription','coins','gift')),
  amount_cents INT NOT NULL CHECK (amount_cents >= 0),
  currency     VARCHAR(3) NOT NULL,
  stripe_id    VARCHAR(255),
  status       VARCHAR(20) NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE subscriptions (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tier       VARCHAR(20) NOT NULL CHECK (tier IN ('silver','gold','platinum')),
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ,
  status     VARCHAR(20) NOT NULL
);

CREATE INDEX idx_profiles_location ON profiles USING GIST (location);
CREATE INDEX idx_profiles_tags     ON profiles USING GIN (tags);
CREATE INDEX idx_hotels_geofence   ON hotels USING GIST (geofence);
CREATE INDEX idx_matches_user1     ON matches (user1_id);
CREATE INDEX idx_matches_user2     ON matches (user2_id);
CREATE INDEX idx_matches_checkin   ON matches (checkin_id) WHERE checkin_id IS NOT NULL;
CREATE INDEX idx_checkins_hotel    ON hotel_checkins (hotel_id);
CREATE INDEX idx_checkins_user     ON hotel_checkins (user_id);
CREATE INDEX idx_checkins_delete   ON hotel_checkins (auto_delete_at);
CREATE INDEX idx_payments_user     ON payments (user_id);
CREATE INDEX idx_subs_user         ON subscriptions (user_id);
CREATE INDEX idx_group_members_usr ON group_members (user_id);
