-- Compatibility score computed asynchronously by the AI service after a match.
-- reasons holds short human-readable strings; age-derived reasons are never
-- stored for incognito/hotel matches (they would leak age before reveal).
ALTER TABLE matches
  ADD COLUMN compat_score   SMALLINT CHECK (compat_score BETWEEN 0 AND 100),
  ADD COLUMN compat_reasons JSONB NOT NULL DEFAULT '[]',
  ADD COLUMN scored_at      TIMESTAMPTZ;
