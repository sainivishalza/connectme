package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"connectme/internal/levels"
)

// giftTo records a gift in the ledger the boards read (the sending flow itself
// is covered by the payments tests).
func (e *env) giftTo(from, to, gift string) {
	e.t.Helper()
	e.exec(`INSERT INTO gifts_sent (from_user, to_user, gift_id, coins)
	        SELECT $1::uuid, $2::uuid, id, price_coins FROM gift_catalog WHERE id = $3`, from, to, gift)
}

type leaderboardJSON struct {
	Entries []struct {
		Rank  int    `json:"rank"`
		Ref   string `json:"ref"`
		Name  string `json:"name"`
		Gifts int    `json:"gifts"`
		Level struct {
			Level int    `json:"level"`
			Title string `json:"title"`
		} `json:"level"`
	} `json:"entries"`
	MyRank int `json:"my_rank"`
}

func (e *env) leaderboard(uid, kind, period string) (int, leaderboardJSON, string) {
	e.t.Helper()
	c, b := e.call("GET", "/levels/board/"+kind+"?period="+period, nil, uid)
	var bd leaderboardJSON
	if c == 200 {
		if err := json.Unmarshal(b, &bd); err != nil {
			e.t.Fatal(err)
		}
	}
	return c, bd, string(b)
}

func TestLevelsAndLeaderboardsComeFromGiftsAndRespectPrivacy(t *testing.T) {
	e := setup(t)
	e.exec(`DELETE FROM gifts_sent`) // a clean ledger so rankings in this test are exact
	host, host2, fan, fan2, watcher := e.user("Star Host"), e.user("Second Host"), e.user("Big Fan"), e.user("Small Fan"), e.user("Watcher")
	e.grant(fan, 1000)
	e.grant(fan2, 1000)

	// Starting point: level 1, nothing public.
	_, b := e.call("GET", "/levels/me", nil, host)
	if !strings.Contains(string(b), `"level":1`) || !strings.Contains(string(b), `"show_charm":true`) || !strings.Contains(string(b), `"show_supporter":false`) {
		t.Fatalf("start: %s", b)
	}

	// Gifts: the big fan sends 3 champagne (150 coins to host 1) and a rose to host 2; the small fan one rose to host 2.
	for i := 0; i < 3; i++ {
		e.giftTo(fan, host, "champagne")
	}
	e.giftTo(fan, host2, "rose")
	e.giftTo(fan2, host2, "rose")

	// Host 1 received 150 coins: Charm level 2 (100+). Their own view shows the money; others do not.
	_, b = e.call("GET", "/levels/me", nil, host)
	var mine struct {
		Charm struct {
			Level int   `json:"level"`
			Total int64 `json:"total"`
		} `json:"charm"`
	}
	_ = json.Unmarshal(b, &mine)
	if mine.Charm.Level != 2 || mine.Charm.Total != 150 {
		t.Errorf("host charm: %s", b)
	}

	c, hosts, raw := e.leaderboard(watcher, "hosts", "week")
	if c != 200 || len(hosts.Entries) != 2 || hosts.Entries[0].Name != "Star Host" || hosts.Entries[0].Rank != 1 || hosts.Entries[0].Gifts != 3 {
		t.Fatalf("hosts board: %d %s", c, raw)
	}
	if hosts.Entries[0].Level.Level != 2 || hosts.Entries[1].Name != "Second Host" {
		t.Errorf("hosts order/levels: %s", raw)
	}
	for _, leak := range []string{"total", "coins", "150", "next_at", "progress"} {
		if strings.Contains(raw, `"`+leak+`"`) {
			t.Errorf("leaderboard leaks %q: %s", leak, raw)
		}
	}

	// A badge appears on the host's card for others; the app shows it next to the name.
	lat, lng := randomBase()
	e.locate(host, lat, lng)
	e.locate(watcher, lat+0.002, lng)
	_, nb := e.call("GET", "/users/nearby?radius_km=25", nil, watcher)
	if !strings.Contains(string(nb), `"charm_level":2`) {
		t.Errorf("badge missing from the card: %s", nb)
	}

	// Supporters are private by default: nobody is listed until they opt in.
	if _, sup, _ := e.leaderboard(watcher, "supporters", "week"); len(sup.Entries) != 0 {
		t.Errorf("supporters listed without consent: %+v", sup)
	}
	if c, _ := e.call("PUT", "/levels/me/visibility", map[string]bool{"show_charm": true, "show_supporter": true}, fan); c != 204 {
		t.Fatalf("opt in: %d", c)
	}
	if c, _ := e.call("PUT", "/levels/me/visibility", map[string]bool{"show_charm": true, "show_supporter": true}, fan2); c != 204 {
		t.Fatalf("opt in: %d", c)
	}
	_, sup, raw := e.leaderboard(watcher, "supporters", "week")
	if len(sup.Entries) != 2 || sup.Entries[0].Name != "Big Fan" || sup.Entries[1].Name != "Small Fan" || sup.Entries[0].Gifts != 4 {
		t.Errorf("supporters: %s", raw)
	}
	if _, mineBoard, _ := e.leaderboard(fan2, "supporters", "all"); mineBoard.MyRank != 2 {
		t.Errorf("my rank: %+v", mineBoard)
	}

	// Switching a ladder off removes the badge and the row; going anonymous does too.
	e.call("PUT", "/levels/me/visibility", map[string]bool{"show_charm": false, "show_supporter": false}, host)
	if _, h, _ := e.leaderboard(watcher, "hosts", "week"); len(h.Entries) != 1 || h.Entries[0].Name != "Second Host" {
		t.Errorf("hidden host still listed: %+v", h)
	}
	_, nb = e.call("GET", "/users/nearby?radius_km=25", nil, watcher)
	if strings.Contains(string(nb), "charm_level") {
		t.Errorf("badge shown after switching it off: %s", nb)
	}
	e.call("POST", "/users/me/mode", map[string]string{"mode": "incognito"}, host2)
	if _, h, _ := e.leaderboard(watcher, "hosts", "week"); len(h.Entries) != 0 {
		t.Errorf("an incognito host is on the board: %+v", h)
	}

	// Blocks hide people from each other's boards; bad input is refused; the period filter works.
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(fan)}, watcher)
	if _, s, _ := e.leaderboard(watcher, "supporters", "week"); len(s.Entries) != 1 || s.Entries[0].Name == "Big Fan" {
		t.Errorf("a blocked member is on my board: %+v", s)
	}
	for _, q := range []string{"/levels/board/everyone", "/levels/board/hosts?period=year"} {
		if c, _ := e.call("GET", q, nil, watcher); c != 422 {
			t.Errorf("%s: %d", q, c)
		}
	}
	if _, err := e.db.Exec(context.Background(), `UPDATE gifts_sent SET created_at = NOW() - INTERVAL '40 days'`); err != nil {
		t.Fatal(err)
	}
	if _, w, _ := e.leaderboard(fan2, "supporters", "week"); len(w.Entries) != 0 {
		t.Errorf("old gifts counted for the week: %+v", w)
	}
	if _, a, _ := e.leaderboard(fan2, "supporters", "all"); len(a.Entries) == 0 {
		t.Errorf("old gifts missing from all-time")
	}
	if c, _ := e.call("PUT", "/levels/me/visibility", map[string]any{"show_charm": true}, watcher); c != 400 {
		t.Errorf("partial visibility body: %d", c)
	}
}

// Rankings are cached, but who may see whom never is: a block takes effect on
// the very next request, and the viewer's own position is always theirs.
func TestLeaderboardCacheServesTheRankingButNotBlocksOrPositions(t *testing.T) {
	e := setup(t)
	e.exec(`DELETE FROM gifts_sent`)
	host, fan, other := e.user("Cache Host"), e.user("Cache Fan"), e.user("Cache Other")
	e.grant(fan, 1000)
	e.giftTo(fan, host, "rose")

	levels.CacheTTL = time.Hour
	t.Cleanup(func() { levels.CacheTTL = 0 })
	svc := levels.NewService(e.db)
	ctx := context.Background()

	first, err := svc.Leaderboard(ctx, other, "hosts", "all", 20)
	if err != nil || len(first.Entries) != 1 || first.Entries[0].Ref != e.refOf(host) {
		t.Fatalf("first read: %+v %v", first, err)
	}
	// A new gift does not show up until the cache expires...
	host2 := e.user("Cache Host Two")
	e.giftTo(fan, host2, "rose")
	if again, _ := svc.Leaderboard(ctx, other, "hosts", "all", 20); len(again.Entries) != 1 {
		t.Errorf("cache was not used: %d entries", len(again.Entries))
	}
	// ...but a block applies at once, and my own rank is mine, not the cached one's.
	e.exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1::uuid, $2::uuid)`, other, host)
	if hidden, _ := svc.Leaderboard(ctx, other, "hosts", "all", 20); len(hidden.Entries) != 0 {
		t.Errorf("blocked host still listed: %+v", hidden.Entries)
	}
	if mine, _ := svc.Leaderboard(ctx, host, "hosts", "all", 20); mine.MyRank != 1 {
		t.Errorf("host's own rank: %d", mine.MyRank)
	}
}
