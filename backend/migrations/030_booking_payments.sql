-- Paying for a confirmed booking through Stripe Checkout. The amount is the
-- one staff recorded when confirming (never client-supplied); the webhook marks
-- the booking paid only when the paid amount equals it. Refunds are made by an
-- admin through Stripe and recorded when Stripe's webhook confirms them.
ALTER TABLE payments DROP CONSTRAINT payments_type_check;
ALTER TABLE payments ADD CONSTRAINT payments_type_check CHECK (type IN ('subscription','coins','gift','booking'));

ALTER TABLE bookings
  ADD COLUMN payment_status VARCHAR(10) NOT NULL DEFAULT 'unpaid' CHECK (payment_status IN ('unpaid','paid','refunded')),
  ADD COLUMN paid_cents     INT CHECK (paid_cents >= 0),
  ADD COLUMN paid_at        TIMESTAMPTZ,
  ADD COLUMN payment_intent VARCHAR(255) UNIQUE;
