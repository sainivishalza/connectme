-- Partner payout ledger. The app collects booking payments through Stripe and
-- owes each partner "paid minus commission" for bookings that were completed.
-- Moving the money is still done outside the app (bank transfer, Stripe Connect
-- later); this ledger records each settlement so a booking is never paid out
-- twice and the partner can see what was paid and when.
CREATE TABLE partner_payouts (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  partner_id   UUID NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
  amount_cents INT  NOT NULL CHECK (amount_cents > 0),
  bookings     INT  NOT NULL CHECK (bookings > 0),
  method       VARCHAR(20) NOT NULL CHECK (method IN ('bank_transfer','stripe_connect','cash','other')),
  reference    VARCHAR(80) NOT NULL,
  note         VARCHAR(200) NOT NULL DEFAULT '',
  created_by   UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  -- the same transfer reference cannot be recorded twice for a partner
  UNIQUE (partner_id, reference)
);
CREATE INDEX idx_payouts_partner ON partner_payouts (partner_id, created_at DESC);

ALTER TABLE bookings ADD COLUMN payout_id UUID REFERENCES partner_payouts(id);
CREATE INDEX idx_bookings_unsettled ON bookings (partner_id)
  WHERE payment_status = 'paid' AND status = 'completed' AND payout_id IS NULL;
