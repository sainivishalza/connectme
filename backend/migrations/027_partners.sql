-- Partners, sponsored events and bookings (F3.5, F3.6, F3.7).
--
-- A partner is a hotel, restaurant or activity provider added by an admin. It
-- sponsors events (hotel mixers, concerts, group hangouts) that members can
-- join, and takes booking requests. Money is modelled but never moved here:
-- event tickets are paid in coins (the existing ledger), and a booking's
-- amount and the platform's commission are recorded for settlement outside the
-- app (Stripe Connect or invoicing is a launch task, see docs/AUDIT.md).
CREATE TABLE partners (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind           VARCHAR(10) NOT NULL CHECK (kind IN ('hotel','restaurant','activity')),
  name           VARCHAR(80) NOT NULL,
  description    VARCHAR(300) NOT NULL DEFAULT '',
  address        VARCHAR(200) NOT NULL DEFAULT '',
  location       GEOGRAPHY(POINT, 4326) NOT NULL,
  hotel_id       UUID REFERENCES hotels(id) ON DELETE SET NULL,
  commission_bps INT NOT NULL DEFAULT 1000 CHECK (commission_bps BETWEEN 0 AND 3000),
  active         BOOLEAN NOT NULL DEFAULT TRUE,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_partners_location ON partners USING GIST (location);

CREATE TABLE events (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  partner_id        UUID NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
  kind              VARCHAR(10) NOT NULL CHECK (kind IN ('mixer','concert','hangout')),
  title             VARCHAR(80) NOT NULL,
  description       VARCHAR(500) NOT NULL DEFAULT '',
  starts_at         TIMESTAMPTZ NOT NULL,
  ends_at           TIMESTAMPTZ NOT NULL,
  capacity          INT CHECK (capacity > 0),
  price_coins       INT NOT NULL DEFAULT 0 CHECK (price_coins >= 0),
  hotel_guests_only BOOLEAN NOT NULL DEFAULT FALSE,
  status            VARCHAR(10) NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','cancelled')),
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (ends_at > starts_at)
);
CREATE INDEX idx_events_time ON events (ends_at) WHERE status = 'scheduled';

-- For events limited to a hotel's guests the RSVP hangs off the stay, so it is
-- deleted with it like everything else created during a stay.
CREATE TABLE event_rsvps (
  event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  checkin_id UUID REFERENCES hotel_checkins(id) ON DELETE CASCADE,
  coins_paid INT NOT NULL DEFAULT 0,
  ref        UUID NOT NULL DEFAULT gen_random_uuid(), -- ledger reference, so a re-join after cancelling is charged and refunded on its own
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (event_id, user_id)
);
CREATE INDEX idx_event_rsvps_user ON event_rsvps (user_id);

CREATE TABLE bookings (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  partner_id       UUID NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
  user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  party_size       SMALLINT NOT NULL CHECK (party_size BETWEEN 1 AND 12),
  requested_for    TIMESTAMPTZ NOT NULL,
  note             VARCHAR(200) NOT NULL DEFAULT '',
  status           VARCHAR(10) NOT NULL DEFAULT 'requested'
                   CHECK (status IN ('requested','confirmed','declined','cancelled','completed')),
  amount_cents     INT CHECK (amount_cents >= 0),
  commission_cents INT CHECK (commission_cents >= 0),
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_bookings_user ON bookings (user_id, created_at DESC);
CREATE INDEX idx_bookings_partner ON bookings (partner_id, status);
