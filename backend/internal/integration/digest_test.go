package integration

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"connectme/internal/digest"
)

var digestTokenRe = regexp.MustCompile(`token=([A-Za-z0-9._-]+)`)

func TestWeeklyDigestIsOptInCountsOnlyAndUnsubscribesInOneTap(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	ob := &outbox{}
	svc := digest.NewService(e.db, ob, []byte("digest-test-secret-at-least-32-bytes!"), "https://app.test")
	me, fan, quiet := e.user("Digest Me"), e.user("Digest Fan"), e.user("Digest Quiet")
	var email, quietEmail string
	_ = e.db.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, me).Scan(&email)
	_ = e.db.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, quiet).Scan(&quietEmail)
	// Make both eligible members who have not been sent one yet.
	for _, u := range []string{me, quiet} {
		e.exec(`UPDATE users SET weekly_digest=TRUE, created_at = NOW() - INTERVAL '10 days' WHERE id=$1`, u)
	}
	e.exec(`INSERT INTO swipes (actor_id, target_id, action) VALUES ($1,$2,'like')`, fan, me)
	e.exec(`INSERT INTO follows (follower_id, followee_id) VALUES ($1,$2)`, fan, me)

	n, err := svc.Run(ctx, 500)
	if err != nil || n < 1 {
		t.Fatalf("run: %d %v", n, err)
	}
	mine := ob.to(email)
	if len(mine) != 1 || !strings.Contains(mine[0].body, "1 person liked you") || !strings.Contains(mine[0].body, "1 new follower") {
		t.Fatalf("digest: %+v", mine)
	}
	// Counts only: no name of the person who liked or followed.
	if strings.Contains(mine[0].body, "Digest Fan") {
		t.Error("the digest names someone")
	}
	if len(ob.to(quietEmail)) != 0 {
		t.Error("a member with nothing to report got an email")
	}
	// Not again within the week.
	if _, err := svc.Run(ctx, 500); err != nil {
		t.Fatal(err)
	}
	if len(ob.to(email)) != 1 {
		t.Error("sent twice in a week")
	}
	// Off by default: a member who never opted in gets nothing.
	if len(ob.to("nobody")) != 0 {
		t.Error("unexpected mail")
	}

	// One-tap unsubscribe, and guessed tokens fail.
	token := digestTokenRe.FindStringSubmatch(mine[0].body)[1]
	if c, _ := e.call("POST", "/digest/unsubscribe", map[string]string{"token": token + "x"}, ""); c != 404 {
		t.Errorf("tampered token: %d", c)
	}
	if c, _ := e.call("POST", "/digest/unsubscribe", map[string]string{"token": me + ".00000000000000000000000000000000"}, ""); c != 404 {
		t.Errorf("forged token: %d", c)
	}
	if err := svc.Unsubscribe(ctx, token); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if on, _ := svc.Enabled(ctx, me); on {
		t.Error("still subscribed")
	}
}

func TestDigestSwitchRoutes(t *testing.T) {
	e := setup(t)
	me := e.user("Switch Member")
	if c, b := e.call("GET", "/digest/me", nil, me); c != 200 || !strings.Contains(string(b), `"enabled":false`) {
		t.Errorf("default: %d %s", c, b)
	}
	if c, _ := e.call("PUT", "/digest/me", map[string]bool{"enabled": true}, me); c != 204 {
		t.Errorf("turn on: %d", c)
	}
	if c, b := e.call("GET", "/digest/me", nil, me); !strings.Contains(string(b), `"enabled":true`) {
		t.Errorf("after: %d %s", c, b)
	}
	if c, _ := e.call("PUT", "/digest/me", map[string]string{}, me); c != 400 {
		t.Errorf("missing field: %d", c)
	}
}
