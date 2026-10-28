-- Dev only. Creates (once) four demo people and places them within a few
-- kilometres of the test user's CURRENT location, so Discover has cards to
-- look at wherever you are. Re-run any time the test user's location changes
-- (the app updates it from the device on every launch).
--
--   docker exec -i dating-app-postgres-1 psql -U <user> -d <db> < ops/dev/seed_demo_people.sql
--
-- Default viewer: reporter@example.test. For another account:
--   (echo "SET demo.viewer = 'you@example.com';"; cat ops/dev/seed_demo_people.sql) | docker exec -i ... psql ...
DO $$
DECLARE
  me geography;
  uid uuid;
  r record;
  n int := 0;
BEGIN
  SELECT location INTO me FROM profiles
   WHERE user_id = (SELECT id FROM users WHERE email = COALESCE(current_setting('demo.viewer', true), 'reporter@example.test'));
  IF me IS NULL THEN
    RAISE EXCEPTION 'the viewer has no location yet: open the app once and allow location';
  END IF;

  IF (SELECT count(*) FROM users WHERE email LIKE 'demo-%@example.test') = 0 THEN
    FOR r IN SELECT * FROM (VALUES
      ('Maya',  'open',      29, 'Climbing, slow mornings and bad puns.',        'Looking for someone who laughs at their own jokes.',      '{climbing,coffee,jazz}', 'curious'),
      ('Idris', 'open',      33, 'Chef by day, vinyl by night.',                  'Will cook for you. Will not share the last dumpling.',    '{cooking,vinyl,travel}', 'easygoing'),
      ('Hidden','incognito', 31, 'Quiet bookshop type with loud opinions.',       '',                                                        '{books,film,long walks}','private'),
      ('Hidden','incognito', 27, 'Night swimmer. Early riser. Contradiction.',    '',                                                        '{swimming,photography}', 'playful')
    ) AS t(name, mode, age, intro, bio, tags, mood) LOOP
      INSERT INTO users (email, password_hash, date_of_birth, is_verified)
      VALUES ('demo-' || md5(random()::text) || '@example.test', 'x',
              (CURRENT_DATE - (r.age * 365 + 100) * INTERVAL '1 day')::date, TRUE)
      RETURNING id INTO uid;
      INSERT INTO profiles (user_id, display_name, intro, bio, gender, tags, mood, mode)
      VALUES (uid, r.name, r.intro, NULLIF(r.bio, ''), 'woman', r.tags::text[], r.mood, r.mode);
    END LOOP;
  END IF;

  -- Spread them 0.5-3 km around the test user (about 0.009 degrees per km).
  FOR r IN SELECT p.user_id FROM profiles p JOIN users u ON u.id = p.user_id
            WHERE u.email LIKE 'demo-%@example.test' ORDER BY u.email LOOP
    n := n + 1;
    UPDATE profiles
       SET location = ST_SetSRID(ST_MakePoint(
             ST_X(me::geometry) + 0.006 * n * (CASE WHEN n % 2 = 0 THEN 1 ELSE -1 END),
             ST_Y(me::geometry) + 0.005 * n * (CASE WHEN n % 3 = 0 THEN -1 ELSE 1 END)), 4326)::geography,
           location_updated_at = NOW()
     WHERE user_id = r.user_id;
  END LOOP;
END $$;

SELECT u.email, p.display_name, p.mode,
       round((ST_Distance(p.location, (SELECT location FROM profiles WHERE user_id = (SELECT id FROM users WHERE email = COALESCE(current_setting('demo.viewer', true), 'reporter@example.test')))) / 1000)::numeric, 1) AS km_from_test_user
  FROM profiles p JOIN users u ON u.id = p.user_id
 WHERE u.email LIKE 'demo-%@example.test' ORDER BY km_from_test_user;

-- Rich profile details and prompts for the demo people (idempotent).
WITH demo AS (
  SELECT u.id, row_number() OVER (ORDER BY u.email) AS n
    FROM users u WHERE u.email LIKE 'demo-%@example.test'
), d AS (
  SELECT id, n,
    (ARRAY['long_term','casual','long_term','friends'])[((n - 1) % 4) + 1] AS goal,
    (ARRAY['want','open','dont_want','open'])[((n - 1) % 4) + 1] AS kids,
    (ARRAY['social','never','social','often'])[((n - 1) % 4) + 1] AS drinking,
    (ARRAY['never','never','social','never'])[((n - 1) % 4) + 1] AS smoking,
    (ARRAY['often','sometimes','often','never'])[((n - 1) % 4) + 1] AS exercise,
    (ARRAY['dog','cat','both','none'])[((n - 1) % 4) + 1] AS pets,
    (ARRAY['Product designer','Chef','Nurse','Teacher'])[((n - 1) % 4) + 1] AS job,
    (ARRAY['Lisbon','Cape Town','Porto','Krakow'])[((n - 1) % 4) + 1] AS home,
    (ARRAY[168, 181, 174, 177])[((n - 1) % 4) + 1] AS height
  FROM demo
)
INSERT INTO profile_details (user_id, height_cm, job_title, education, hometown, goal, kids, drinking, smoking, exercise, pets, languages)
SELECT id, height, job, 'bachelor', home, goal, kids, drinking, smoking, exercise, pets, ARRAY['en','pt']
  FROM d
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO profile_prompts (user_id, slot, prompt_key, answer)
SELECT u.id, v.slot, v.k, v.a
  FROM (SELECT id, row_number() OVER (ORDER BY email) AS n FROM users WHERE email LIKE 'demo-%@example.test') u
  JOIN (VALUES
    (1, 1, 'perfect_sunday', 'Market, a long lunch, and a nap I will deny taking.'),
    (1, 2, 'geek_out', 'Sourdough starters. I have named mine.'),
    (2, 1, 'best_trip', 'A week of trains through the Alps with no plan.'),
    (2, 2, 'green_flag', 'Someone who is kind to the waiter.'),
    (3, 1, 'fun_fact', 'I once cooked for forty people with one working burner.'),
    (3, 2, 'date_idea', 'A long walk that ends at a good bakery.'),
    (4, 1, 'dont_hate_me', 'Think pineapple belongs on pizza.'),
    (4, 2, 'song_on_repeat', 'Anything with a trumpet in it.')
  ) AS v(n, slot, k, a) ON v.n = u.n
ON CONFLICT DO NOTHING;
