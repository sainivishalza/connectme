-- Host payouts (internal/payouts). Hosts earn coins from gifts in live rooms
-- (live_gifts.host_coins). Until now staff settled that by hand with no record.
-- A host asks to be paid out; staff pay outside the app and record the transfer
-- here. Each gift is stamped with the payout it belongs to, so a gift can never
-- be paid out twice and a rejected request frees its gifts again.
CREATE TABLE host_payouts (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  host_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  coins        INT  NOT NULL CHECK (coins > 0),
  status       VARCHAR(10) NOT NULL DEFAULT 'requested' CHECK (status IN ('requested','paid','rejected')),
  amount_cents INT CHECK (amount_cents IS NULL OR amount_cents > 0),
  method       VARCHAR(20) CHECK (method IS NULL OR method IN ('bank_transfer','upi','paypal','other')),
  reference    VARCHAR(80),
  note         VARCHAR(200) NOT NULL DEFAULT '',
  requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  decided_at   TIMESTAMPTZ,
  decided_by   UUID REFERENCES users(id) ON DELETE SET NULL
);
-- One open request per host; a transfer reference is recorded once.
CREATE UNIQUE INDEX idx_host_payouts_open ON host_payouts (host_id) WHERE status = 'requested';
CREATE UNIQUE INDEX idx_host_payouts_ref ON host_payouts (reference) WHERE reference IS NOT NULL;
CREATE INDEX idx_host_payouts_host ON host_payouts (host_id, requested_at DESC);

ALTER TABLE live_gifts ADD COLUMN payout_id UUID REFERENCES host_payouts(id) ON DELETE SET NULL;
CREATE INDEX idx_live_gifts_unsettled ON live_gifts (room_id) WHERE payout_id IS NULL AND host_coins > 0;
