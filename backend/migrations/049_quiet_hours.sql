-- Quiet hours: during the member's chosen window, pushes other than incoming
-- calls are not sent (they are dropped, not queued: the app shows everything
-- when opened). Times are minutes after local midnight; tz_min is the offset of
-- the member's local time from UTC in minutes, sent by the app.
ALTER TABLE push_prefs
  ADD COLUMN quiet_on    BOOLEAN  NOT NULL DEFAULT FALSE,
  ADD COLUMN quiet_start SMALLINT NOT NULL DEFAULT 1320 CHECK (quiet_start BETWEEN 0 AND 1439),
  ADD COLUMN quiet_end   SMALLINT NOT NULL DEFAULT 480  CHECK (quiet_end BETWEEN 0 AND 1439),
  ADD COLUMN tz_min      SMALLINT NOT NULL DEFAULT 0    CHECK (tz_min BETWEEN -840 AND 840);
