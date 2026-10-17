-- Group dates: size rules are enforced in the database as well as the service.
ALTER TABLE groups
  ADD COLUMN target_size    SMALLINT NOT NULL DEFAULT 2,
  ADD COLUMN activity_label VARCHAR(50),
  ADD COLUMN description    VARCHAR(280),
  ADD COLUMN location       GEOGRAPHY(POINT, 4326),
  ADD COLUMN status         VARCHAR(12) NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
  ADD CONSTRAINT groups_size_chk CHECK (
        (type = 'double' AND target_size = 2)
     OR (type = 'group'  AND target_size BETWEEN 2 AND 6)),
  ADD CONSTRAINT groups_activity_chk CHECK (activity IN ('dinner','hiking','concert','bar_crawl','custom'));

-- Erasing a user must not delete a group the other members still use.
ALTER TABLE groups ALTER COLUMN created_by DROP NOT NULL;
ALTER TABLE groups DROP CONSTRAINT groups_created_by_fkey;
ALTER TABLE groups ADD CONSTRAINT groups_created_by_fkey
  FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE group_members
  ADD COLUMN role VARCHAR(10) NOT NULL DEFAULT 'member' CHECK (role IN ('owner','member'));

CREATE INDEX idx_groups_location ON groups USING GIST (location);

-- Every current member of a group must consent for the group to "like" another.
CREATE TABLE group_match_consents (
  group_id        UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  target_group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (group_id, target_group_id, user_id),
  CHECK (group_id <> target_group_id)
);

-- A matched pair of groups; its id is also the merged conversation id.
CREATE TABLE group_matches (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  group_a    UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  group_b    UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  status     VARCHAR(10) NOT NULL DEFAULT 'matched' CHECK (status IN ('matched','ended')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (group_a < group_b),
  UNIQUE (group_a, group_b)
);
CREATE INDEX idx_group_matches_b ON group_matches (group_b);

CREATE TABLE group_dates (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  group_match_id UUID NOT NULL REFERENCES group_matches(id) ON DELETE CASCADE,
  venue          VARCHAR(120) NOT NULL,
  address        VARCHAR(200),
  starts_at      TIMESTAMPTZ NOT NULL,
  proposed_by    UUID REFERENCES users(id) ON DELETE SET NULL,
  status         VARCHAR(10) NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','confirmed','cancelled')),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_group_dates_match ON group_dates (group_match_id);

CREATE TABLE group_date_confirmations (
  date_id UUID NOT NULL REFERENCES group_dates(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (date_id, user_id)
);

-- Mailbox for client-encrypted group-key material (E2E sender keys). The
-- server relays opaque blobs; they are deleted once fetched.
CREATE TABLE group_key_envelopes (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  group_match_id UUID NOT NULL REFERENCES group_matches(id) ON DELETE CASCADE,
  from_user      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  to_user        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  ciphertext     TEXT NOT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_envelopes_to ON group_key_envelopes (to_user, group_match_id);
