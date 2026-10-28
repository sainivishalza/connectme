-- Rich profile details and prompt answers.
--
-- Two groups of fields, because Incognito and Hotel profiles must stay
-- anonymous (see internal/users/details.go):
--   * lifestyle answers (goal, kids, drinking, smoking, exercise, pets,
--     languages) say what someone wants, not who they are, so they are shown
--     on anonymous cards and can be filtered on;
--   * identifying answers (height, job title, education, hometown, zodiac) and
--     the prompt answers are shown only on profiles that are visible in full.
-- Deliberately NOT collected: religion, politics, sexual orientation beyond the
-- existing "looking for", health, or employer/school names. Special-category
-- data adds legal weight and little matching value for a first version.
CREATE TABLE profile_details (
  user_id    UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  height_cm  SMALLINT CHECK (height_cm BETWEEN 120 AND 230),
  job_title  VARCHAR(60),
  education  VARCHAR(12) CHECK (education IN ('high_school','trade','college','bachelor','master','doctorate','other')),
  hometown   VARCHAR(60),
  zodiac     VARCHAR(12) CHECK (zodiac IN ('aries','taurus','gemini','cancer','leo','virgo','libra','scorpio','sagittarius','capricorn','aquarius','pisces')),
  goal       VARCHAR(14) CHECK (goal IN ('long_term','short_term','casual','friends','figuring_out')),
  kids       VARCHAR(10) CHECK (kids IN ('want','dont_want','open','have','unsure')),
  drinking   VARCHAR(8)  CHECK (drinking IN ('never','social','often')),
  smoking    VARCHAR(8)  CHECK (smoking IN ('never','social','often')),
  exercise   VARCHAR(10) CHECK (exercise IN ('never','sometimes','often')),
  pets       VARCHAR(6)  CHECK (pets IN ('dog','cat','both','none','other')),
  languages  TEXT[] NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_profile_details_goal ON profile_details (goal) WHERE goal IS NOT NULL;

CREATE TABLE profile_prompts (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  slot       SMALLINT NOT NULL CHECK (slot BETWEEN 1 AND 3),
  prompt_key VARCHAR(30) NOT NULL,
  answer     VARCHAR(200) NOT NULL,
  PRIMARY KEY (user_id, slot),
  UNIQUE (user_id, prompt_key)
);
