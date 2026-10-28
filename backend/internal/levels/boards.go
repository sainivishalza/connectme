package levels

import (
	"context"
	"fmt"
	"time"
)

// Entry is one row of a leaderboard. It carries a level and a count of gifts,
// never an amount of money.
type Entry struct {
	Rank  int    `json:"rank"`
	Ref   string `json:"ref"`
	Name  string `json:"name"`
	Level Badge  `json:"level"`
	Gifts int    `json:"gifts"`
	Me    bool   `json:"me,omitempty"`
}

// Board is a leaderboard and where the caller stands on it, if they are on it.
type Board struct {
	Kind    string  `json:"kind"`
	Period  string  `json:"period"`
	Entries []Entry `json:"entries"`
	MyRank  int     `json:"my_rank,omitempty"`
}

// periodStart maps a period name to its start; "all" has none.
func (s *Service) periodStart(period string) (*time.Time, bool) {
	now := s.now()
	switch period {
	case "week":
		t := now.AddDate(0, 0, -7)
		return &t, true
	case "month":
		t := now.AddDate(0, 0, -30)
		return &t, true
	case "all":
		return nil, true
	}
	return nil, false
}

// CacheTTL is how long a ranking is reused before it is computed again. The
// ranking sums every gift ever sent, so computing it on each request would
// not scale; a few seconds of staleness on a leaderboard is fine. Tests that
// need to see a change at once set it to zero.
var CacheTTL = 30 * time.Second

// ranked is one member of the viewer-independent ranking.
type ranked struct {
	uid, ref, name string
	gifts          int
	total          int64
}

type cached struct {
	at   time.Time
	rows []ranked
}

// ranking returns the top 200 visible members for a board, from the cache
// when it is fresh. Who may *see* each of them (blocks) is applied afterwards,
// per viewer, so the cache is shared by everyone.
func (s *Service) ranking(ctx context.Context, kind, period string, since *time.Time) ([]ranked, error) {
	key := kind + "/" + period
	s.mu.Lock()
	c, ok := s.cache[key]
	s.mu.Unlock()
	if ok && CacheTTL > 0 && s.now().Sub(c.at) < CacheTTL {
		return c.rows, nil
	}
	rows, err := s.db.Query(ctx, `
		WITH score AS (
		  SELECT r.host_id AS uid, SUM(lg.host_coins)::bigint AS coins, COUNT(*)::int AS gifts
		    FROM live_gifts lg JOIN live_rooms r ON r.id = lg.room_id
		   WHERE $2::text = 'hosts' AND ($1::timestamptz IS NULL OR lg.created_at >= $1) GROUP BY r.host_id
		  UNION ALL
		  SELECT uid, SUM(coins)::bigint, COUNT(*)::int FROM (
		    SELECT lg.from_user AS uid, lg.coins, lg.created_at FROM live_gifts lg
		    UNION ALL SELECT g.from_user, g.coins, g.created_at FROM gifts_sent g) x
		   WHERE $2::text = 'supporters' AND ($1::timestamptz IS NULL OR created_at >= $1) GROUP BY uid)
		SELECT score.uid::text, p.public_ref::text, COALESCE(p.display_name,''), score.gifts,
		       CASE $2::text WHEN 'hosts' THEN charm_coins(score.uid) ELSE support_coins(score.uid) END
		  FROM score
		  JOIN profiles p ON p.user_id = score.uid
		  JOIN users u ON u.id = score.uid AND u.is_verified AND NOT u.is_banned
		 WHERE p.mode = 'open' AND NOT p.invisible
		   AND CASE $2::text WHEN 'hosts' THEN p.show_charm ELSE p.show_supporter END
		 ORDER BY score.coins DESC, score.uid LIMIT 200`, since, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ranked
	for rows.Next() {
		var r ranked
		if err := rows.Scan(&r.uid, &r.ref, &r.name, &r.gifts, &r.total); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[key] = cached{at: s.now(), rows: out}
	s.mu.Unlock()
	return out, nil
}

// Leaderboard ranks visible members. Kind "hosts" ranks by gift value received
// in live rooms; "supporters" ranks by gifts sent (live and chat), and lists
// only people who chose to show their Supporter level. Nobody appears who is
// not shown in full on their profile (Open mode, not invisible, not banned),
// who switched their level off, or who is blocked with the viewer either way.
func (s *Service) Leaderboard(ctx context.Context, viewerID, kind, period string, limit int) (*Board, error) {
	since, ok := s.periodStart(period)
	if !ok || (kind != "hosts" && kind != "supporters") {
		return nil, ErrBadRequest
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}
	all, err := s.ranking(ctx, kind, period, since)
	if err != nil {
		return nil, err
	}
	// Blocks are applied fresh on every call, never from the cache.
	hidden := map[string]bool{}
	brows, err := s.db.Query(ctx, `
		SELECT CASE WHEN blocker_id = $1::uuid THEN blocked_id ELSE blocker_id END::text
		  FROM blocks WHERE blocker_id = $1::uuid OR blocked_id = $1::uuid`, viewerID)
	if err != nil {
		return nil, err
	}
	for brows.Next() {
		var id string
		if brows.Scan(&id) == nil {
			hidden[id] = true
		}
	}
	brows.Close()
	b := &Board{Kind: kind, Period: period, Entries: []Entry{}}
	rank := 0
	for _, r := range all {
		if hidden[r.uid] {
			continue
		}
		rank++
		e := Entry{Rank: rank, Ref: r.ref, Name: r.name, Level: For(r.total).Badge(), Gifts: r.gifts, Me: r.uid == viewerID}
		if e.Me {
			b.MyRank = rank
		}
		if rank <= limit {
			b.Entries = append(b.Entries, e)
		}
	}
	return b, nil
}

var ErrBadRequest = fmt.Errorf("unknown board or period")
