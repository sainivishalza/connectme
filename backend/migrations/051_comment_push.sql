-- Notification choice for "someone replied to your post".
ALTER TABLE push_prefs ADD COLUMN comments BOOLEAN NOT NULL DEFAULT TRUE;
