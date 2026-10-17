-- Append-only record of everything staff see or do. The application has no
-- code path that updates or deletes rows here, and a trigger makes that a
-- database fact: rows can never be deleted, and the only permitted update is
-- the foreign-key SET NULL that happens when a user (or report) is erased,
-- which clears an id but keeps the entry.
CREATE TABLE staff_audit_log (
  id             BIGSERIAL PRIMARY KEY,
  actor_id       UUID REFERENCES users(id) ON DELETE SET NULL,
  actor_role     VARCHAR(20) NOT NULL,
  action         VARCHAR(40) NOT NULL,
  target_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  report_id      UUID REFERENCES reports(id) ON DELETE SET NULL,
  detail         JSONB NOT NULL DEFAULT '{}',
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_staff_audit_created ON staff_audit_log (created_at DESC);
CREATE INDEX idx_staff_audit_target  ON staff_audit_log (target_user_id, created_at DESC);

CREATE FUNCTION staff_audit_guard() RETURNS trigger AS $$
BEGIN
  IF TG_OP = 'UPDATE'
     AND NEW.id = OLD.id AND NEW.actor_role = OLD.actor_role AND NEW.action = OLD.action
     AND NEW.detail = OLD.detail AND NEW.created_at = OLD.created_at
     AND (NEW.actor_id IS NULL OR NEW.actor_id = OLD.actor_id)
     AND (NEW.target_user_id IS NULL OR NEW.target_user_id = OLD.target_user_id)
     AND (NEW.report_id IS NULL OR NEW.report_id = OLD.report_id) THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'staff_audit_log is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER staff_audit_no_change BEFORE UPDATE OR DELETE ON staff_audit_log
  FOR EACH ROW EXECUTE FUNCTION staff_audit_guard();
