-- Partner portal: which members manage which partner. A manager runs only
-- that partner's events and bookings; admins add and remove managers. Removing
-- a user or a partner removes the link.
CREATE TABLE partner_managers (
  partner_id UUID NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (partner_id, user_id)
);
CREATE INDEX idx_partner_managers_user ON partner_managers (user_id);
