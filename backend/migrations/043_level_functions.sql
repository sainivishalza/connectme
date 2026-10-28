-- Lifetime gift totals behind the levels (internal/levels). Kept as functions so
-- the queries that use them stay plain static SQL.

-- Coins a member has earned from gifts: the host's share of live gifts plus gifts sent to them in chat.
CREATE FUNCTION charm_coins(uid UUID) RETURNS BIGINT LANGUAGE SQL STABLE AS $$
  SELECT (COALESCE((SELECT SUM(lg.host_coins) FROM live_gifts lg JOIN live_rooms r ON r.id = lg.room_id WHERE r.host_id = uid), 0)
        + COALESCE((SELECT SUM(g.coins) FROM gifts_sent g WHERE g.to_user = uid), 0))::bigint
$$;

-- Coins a member has spent on gifts, live and in chat.
CREATE FUNCTION support_coins(uid UUID) RETURNS BIGINT LANGUAGE SQL STABLE AS $$
  SELECT (COALESCE((SELECT SUM(lg.coins) FROM live_gifts lg WHERE lg.from_user = uid), 0)
        + COALESCE((SELECT SUM(g.coins) FROM gifts_sent g WHERE g.from_user = uid), 0))::bigint
$$;
