package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"connectme/internal/auth"
	"connectme/internal/wallet"
)

func TestBlockHidesEverywhereAndEndsMatch(t *testing.T) {
	e := setup(t)
	a, b := e.user("Ann Block"), e.user("Ben Block")
	e.swipe(a, e.refOf(b), "like")
	_, r := e.swipe(b, e.refOf(a), "like")
	if !r.Matched {
		t.Fatal("setup: expected match")
	}
	refA, refB := e.refOf(a), e.refOf(b)

	if c, _ := e.call("POST", "/moderation/blocks", map[string]string{"ref": refA}, a); c != 422 {
		t.Fatalf("self block: %d", c)
	}
	if c, _ := e.call("POST", "/moderation/blocks", map[string]string{"ref": refB}, a); c != 204 {
		t.Fatalf("block: %d", c)
	}
	// Hidden in both directions; match gone.
	if c, _ := e.call("GET", "/users/"+refB, nil, a); c != 404 {
		t.Fatalf("blocker sees blocked: %d", c)
	}
	if c, _ := e.call("GET", "/users/"+refA, nil, b); c != 404 {
		t.Fatalf("blocked sees blocker: %d", c)
	}
	if c, _ := e.swipe(b, refA, "like"); c != 404 {
		t.Fatalf("blocked can still swipe: %d", c)
	}
	if _, body := e.call("GET", "/matching/matches", nil, a); strings.Contains(string(body), r.MatchID) {
		t.Fatal("match survived a block")
	}
	// Unblock restores visibility.
	e.call("DELETE", "/moderation/blocks/"+refB, nil, a)
	if c, _ := e.call("GET", "/users/"+refB, nil, a); c != 200 {
		t.Fatalf("after unblock: %d", c)
	}
}

func TestReportAndModeratorActions(t *testing.T) {
	e := setup(t)
	reporter, bad := e.user("Reporter"), e.user("Offender")
	mod := e.newUser("moderator", true, "Mod")
	admin := e.newUser("admin", true, "Admin")
	refBad := e.refOf(bad)

	if c, _ := e.call("POST", "/moderation/reports", map[string]any{"ref": refBad, "reason": "nonsense"}, reporter); c != 422 {
		t.Fatalf("bad reason: %d", c)
	}
	c, _ := e.call("POST", "/moderation/reports",
		map[string]any{"ref": refBad, "reason": "harassment", "details": "<b>rude</b>", "evidence": []string{"msg one", "msg two"}}, reporter)
	if c != 201 {
		t.Fatalf("report: %d", c)
	}
	// The queue is oldest-first and capped, so a long-lived dev database would
	// push this test's report off the first page: close everything else.
	e.exec(`UPDATE reports SET status='dismissed' WHERE status='open' AND reported_id <> $1`, bad)
	// Ordinary users cannot read the queue.
	if c, _ := e.call("GET", "/moderation/reports", nil, reporter); c != 403 {
		t.Fatalf("user reading queue: %d", c)
	}
	_, body := e.callAs("GET", "/moderation/reports", nil, mod, "moderator")
	s := string(body)
	if !strings.Contains(s, refBad) || !strings.Contains(s, "msg two") || strings.Contains(s, "<b>") {
		t.Fatalf("queue: %s", s)
	}
	var reportID string
	if err := e.db.QueryRow(context.Background(),
		`SELECT id::text FROM reports WHERE reported_id=$1`, bad).Scan(&reportID); err != nil {
		t.Fatal(err)
	}

	// A live session for the offender, issued before the ban.
	tokens := auth.NewTokenStore(e.rdb.Client, time.Hour)
	refresh, _, err := tokens.Issue(context.Background(), bad, "")
	if err != nil {
		t.Fatal(err)
	}

	if c, _ := e.callAs("POST", "/moderation/reports/"+reportID+"/resolve", map[string]string{"action": "ban"}, mod, "moderator"); c != 204 {
		t.Fatalf("ban: %d", c)
	}
	if e.count(`SELECT COUNT(*) FROM users WHERE id=$1 AND is_banned`, bad) != 1 {
		t.Fatal("user not banned")
	}
	if _, _, err := tokens.Rotate(context.Background(), refresh); err == nil {
		t.Fatal("banned user's refresh token still works")
	}
	if c, _ := e.call("GET", "/users/"+refBad, nil, reporter); c != 404 {
		t.Fatalf("banned profile visible: %d", c)
	}
	if c, _ := e.callAs("POST", "/moderation/reports/"+reportID+"/resolve", map[string]string{"action": "dismiss"}, mod, "moderator"); c != 422 {
		t.Fatalf("double resolve: %d", c)
	}

	// Moderators cannot ban staff; admins can.
	e.call("POST", "/moderation/reports", map[string]any{"ref": e.refOf(mod), "reason": "spam"}, reporter)
	var modReport string
	_ = e.db.QueryRow(context.Background(), `SELECT id::text FROM reports WHERE reported_id=$1`, mod).Scan(&modReport)
	if c, _ := e.callAs("POST", "/moderation/reports/"+modReport+"/resolve", map[string]string{"action": "ban"}, mod, "moderator"); c != 403 {
		t.Fatalf("moderator banning moderator: %d", c)
	}
	if c, _ := e.callAs("POST", "/moderation/reports/"+modReport+"/resolve", map[string]string{"action": "ban"}, admin, "admin"); c != 204 {
		t.Fatalf("admin banning moderator: %d", c)
	}
}

func TestWalletNeverGoesNegativeUnderConcurrency(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	u := e.user("Spender")
	if _, err := wallet.Grant(ctx, e.db, u, 10, "test", ""); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := e.db.Begin(ctx)
			if err != nil {
				return
			}
			defer tx.Rollback(ctx)
			if err := wallet.Spend(ctx, tx, u, 5, "test", ""); err != nil {
				return
			}
			if tx.Commit(ctx) == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	bal, _ := wallet.Balance(ctx, e.db, u)
	if ok != 2 || bal != 0 {
		t.Fatalf("successful spends=%d balance=%d, want 2 and 0", ok, bal)
	}
	// Idempotent grant (refs are globally unique, so make this run's own).
	ref := fmt.Sprintf("evt_%d", time.Now().UnixNano())
	if applied, _ := wallet.Grant(ctx, e.db, u, 7, "buy", ref); !applied {
		t.Fatal("first grant should apply")
	}
	if applied, _ := wallet.Grant(ctx, e.db, u, 7, "buy", ref); applied {
		t.Fatal("repeat grant with same ref must not apply")
	}
	if bal, _ := wallet.Balance(ctx, e.db, u); bal != 7 {
		t.Fatalf("balance %d want 7", bal)
	}
}
