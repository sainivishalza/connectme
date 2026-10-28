-- Sponsored placements (PRD: brand placements in the feed). An admin creates a
-- placement with advertiser, headline and a short text, an optional https link,
-- a run window, and optionally a place and radius. Members on Silver or above
-- never see them (the "no ads" perk). Only aggregate counters are kept: how
-- often a placement was shown and clicked, never who saw or clicked it.
CREATE TABLE sponsor_placements (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  advertiser      VARCHAR(60) NOT NULL,
  headline        VARCHAR(80) NOT NULL,
  body            VARCHAR(200) NOT NULL DEFAULT '',
  link_url        VARCHAR(300),
  starts_at       TIMESTAMPTZ NOT NULL,
  ends_at         TIMESTAMPTZ NOT NULL,
  location        GEOGRAPHY(POINT, 4326),
  radius_km       INT CHECK (radius_km BETWEEN 1 AND 200),
  max_impressions INT CHECK (max_impressions > 0),
  active          BOOLEAN NOT NULL DEFAULT TRUE,
  impressions     INT NOT NULL DEFAULT 0,
  clicks          INT NOT NULL DEFAULT 0,
  created_by      UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (ends_at > starts_at),
  CHECK ((location IS NULL) = (radius_km IS NULL))
);
CREATE INDEX idx_sponsor_window ON sponsor_placements (ends_at) WHERE active;
