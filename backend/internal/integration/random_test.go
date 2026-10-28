package integration

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"connectme/internal/random"
	"connectme/internal/users"
)

type randStatus struct {
	State   string `json:"state"`
	Session *struct {
		MatchID   string     `json:"match_id"`
		ExpiresAt *time.Time `json:"expires_at"`
		KeptByMe  bool       `json:"kept_by_me"`
		Kept      bool       `json:"kept"`
		Peer      struct {
			Ref       string `json:"ref"`
			Anonymous bool   `json:"anonymous"`
		} `json:"peer"`
	} `json:"session"`
}

func (e *env) randJoin(uid string) randStatus {
	e.t.Helper()
	c, b := e.call("POST", "/random/join", nil, uid)
	if c != 200 {
		e.t.Fatalf("join: %d %s", c, b)
	}
	var s randStatus
	decodeInto(e.t, b, &s)
	return s
}

func (e *env) randStatus(uid string) randStatus {
	e.t.Helper()
	c, b := e.call("GET", "/random/status", nil, uid)
	if c != 200 {
		e.t.Fatalf("status: %d %s", c, b)
	}
	var s randStatus
	decodeInto(e.t, b, &s)
	return s
}

// freshQueue empties the (global) waiting queue so earlier tests' leftovers
// cannot be paired with this test's people.
func (e *env) freshQueue() {
	e.t.Helper()
	e.exec(`DELETE FROM random_waiting`)
}

func (e *env) randSvc() *random.Service {
	return random.NewService(e.db, users.NewService(e.db, users.LocalStore{Dir: e.t.TempDir()}, nil))
}

func TestRandomPairingGivesBothAnAnonymousPeer(t *testing.T) {
	e := setup(t)
	e.freshQueue()
	a, b := e.user("Alice Real"), e.user("Bob Real")
	e.exec(`UPDATE profiles SET tags='{hiking,jazz}', intro='hello there' WHERE user_id IN ($1,$2)`, a, b)

	if s := e.randJoin(a); s.State != "waiting" {
		t.Fatalf("first joiner: %q", s.State)
	}
	if s := e.randStatus(a); s.State != "waiting" {
		t.Errorf("waiting status: %q", s.State)
	}
	sb := e.randJoin(b)
	if sb.State != "matched" || sb.Session == nil {
		t.Fatalf("second joiner not paired: %+v", sb)
	}
	sa := e.randStatus(a)
	if sa.State != "matched" || sa.Session == nil || sa.Session.MatchID != sb.Session.MatchID {
		t.Fatalf("first joiner not told about the pairing: %+v", sa)
	}
	// Anonymous both ways: no names, no photos, no age, no email.
	for who, raw := range map[string][]byte{"a": mustJSON(t, sa), "b": mustJSON(t, sb)} {
		s := strings.ToLower(string(raw))
		for _, leak := range []string{"alice", "bob", "email", "date_of_birth"} {
			if strings.Contains(s, leak) {
				t.Errorf("%s's view leaks %q: %s", who, leak, s)
			}
		}
	}
	if !sa.Session.Peer.Anonymous || !sb.Session.Peer.Anonymous {
		t.Error("peer not anonymous")
	}
	if sa.Session.ExpiresAt == nil {
		t.Error("session has no expiry")
	}
	// The match is an incognito random match, whatever mode each person is in.
	if n := e.count(`SELECT COUNT(*) FROM matches WHERE id=$1::uuid AND origin='random' AND mode='incognito'`, sa.Session.MatchID); n != 1 {
		t.Error("match not recorded as incognito random")
	}
	// Both can use the encrypted chat endpoints.
	for _, u := range []string{a, b} {
		if c, _ := e.call("GET", "/chat/matches/"+sa.Session.MatchID+"/access", nil, u); c != 204 {
			t.Errorf("chat access during session: %d", c)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRandomNeverPairsBlockedBannedUnverifiedOrAlreadyMatched(t *testing.T) {
	e := setup(t)
	e.freshQueue()
	me := e.user("Me")
	blocked, banned, matchedBefore := e.user("Blocked"), e.user("Banned"), e.user("Known")
	unverified := e.newUser("user", false, "Unv")
	e.exec(`UPDATE users SET is_banned=true WHERE id=$1`, banned)
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(blocked)}, me)
	u1, u2 := me, matchedBefore
	if u2 < u1 {
		u1, u2 = u2, u1
	}
	e.exec(`INSERT INTO matches (user1_id, user2_id, status, mode) VALUES ($1,$2,'rejected','open')`, u1, u2)

	if c, _ := e.call("POST", "/random/join", nil, unverified); c != 403 {
		t.Errorf("unverified joined: %d", c)
	}
	if c, _ := e.call("POST", "/random/join", nil, banned); c != 403 && c != 401 {
		t.Errorf("banned joined: %d", c)
	}
	// Put each unsuitable person in the queue (directly, since they would
	// otherwise pair with each other), then join as me: nobody is eligible.
	for _, u := range []string{blocked, matchedBefore} {
		e.exec(`INSERT INTO random_waiting (user_id) VALUES ($1)`, u)
	}
	if s := e.randJoin(me); s.State != "waiting" {
		t.Errorf("paired with someone unsuitable: %+v", s)
	}
}

func TestRandomPairsPeopleWithMoreSharedInterestsFirst(t *testing.T) {
	e := setup(t)
	e.freshQueue()
	me, far, near := e.user("Me"), e.user("Far"), e.user("Near")
	e.exec(`UPDATE profiles SET tags='{chess,hiking,jazz}' WHERE user_id=$1`, me)
	e.exec(`UPDATE profiles SET tags='{knitting}' WHERE user_id=$1`, far)
	e.exec(`UPDATE profiles SET tags='{hiking,jazz}' WHERE user_id=$1`, near)
	// Queue both directly (they would pair with each other otherwise); far waited longer.
	e.exec(`INSERT INTO random_waiting (user_id, joined_at) VALUES ($1, NOW() - INTERVAL '20 seconds')`, far)
	e.exec(`INSERT INTO random_waiting (user_id, joined_at) VALUES ($1, NOW() - INTERVAL '5 seconds')`, near)
	s := e.randJoin(me)
	if s.State != "matched" || s.Session == nil {
		t.Fatalf("not paired: %+v", s)
	}
	if e.randStatus(near).State != "matched" {
		t.Error("the person with shared interests was not chosen")
	}
	if e.randStatus(far).State != "waiting" {
		t.Error("the unrelated person should still be waiting")
	}
}

func TestRandomExpiryEndsChatAndNeverRepairs(t *testing.T) {
	e := setup(t)
	e.freshQueue()
	a, b := e.user("A"), e.user("B")
	e.randJoin(a)
	m := e.randJoin(b).Session.MatchID

	e.exec(`UPDATE matches SET expires_at = NOW() - INTERVAL '1 second' WHERE id=$1::uuid`, m)
	// Effective immediately, before the job runs: no chat, and not in the list.
	if c, _ := e.call("GET", "/chat/matches/"+m+"/access", nil, a); c == 204 {
		t.Error("chat still open after expiry")
	}
	_, body := e.call("GET", "/matching/matches", nil, a)
	if strings.Contains(string(body), m) {
		t.Error("expired random chat still listed")
	}
	if s := e.randStatus(a); s.State != "idle" {
		t.Errorf("status after expiry: %q", s.State)
	}
	// Another expiry job (a running dev server shares this database) may get
	// to the row first, so the count Expire returns proves nothing: the state
	// of the match is what matters.
	if _, err := e.randSvc().Expire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.count(`SELECT COUNT(*) FROM matches WHERE id=$1::uuid AND status='rejected'`, m) != 1 {
		t.Error("expiry job did not end the match")
	}
	// The same two people are never paired again.
	e.randJoin(a)
	if s := e.randJoin(b); s.State != "waiting" {
		t.Errorf("re-paired after a finished chat: %+v", s)
	}
}

func TestRandomKeepNeedsBothPeople(t *testing.T) {
	e := setup(t)
	e.freshQueue()
	a, b := e.user("A"), e.user("B")
	e.randJoin(a)
	m := e.randJoin(b).Session.MatchID
	keep := func(u string) randStatus {
		c, body := e.call("POST", "/random/"+m+"/keep", nil, u)
		if c != 200 {
			t.Fatalf("keep: %d %s", c, body)
		}
		var s randStatus
		var sess struct {
			KeptByMe bool       `json:"kept_by_me"`
			Kept     bool       `json:"kept"`
			Expires  *time.Time `json:"expires_at"`
		}
		decodeInto(t, body, &sess)
		s.Session = &struct {
			MatchID   string     `json:"match_id"`
			ExpiresAt *time.Time `json:"expires_at"`
			KeptByMe  bool       `json:"kept_by_me"`
			Kept      bool       `json:"kept"`
			Peer      struct {
				Ref       string `json:"ref"`
				Anonymous bool   `json:"anonymous"`
			} `json:"peer"`
		}{ExpiresAt: sess.Expires, KeptByMe: sess.KeptByMe, Kept: sess.Kept}
		return s
	}
	one := keep(a)
	if one.Session.Kept || one.Session.ExpiresAt == nil || !one.Session.KeptByMe {
		t.Errorf("one person keeping must not make it permanent: %+v", one.Session)
	}
	keep(a) // idempotent
	if e.count(`SELECT cardinality(kept_by) FROM matches WHERE id=$1::uuid`, m) != 1 {
		t.Error("keeping twice counted twice")
	}
	both := keep(b)
	if !both.Session.Kept || both.Session.ExpiresAt != nil {
		t.Errorf("both kept but still expiring: %+v", both.Session)
	}
	// It survives the expiry job and stays a normal match; each is free to start another random chat.
	e.exec(`UPDATE matches SET expires_at = NULL WHERE id=$1::uuid`, m)
	if _, err := e.randSvc().Expire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c, _ := e.call("GET", "/chat/matches/"+m+"/access", nil, a); c != 204 {
		t.Errorf("kept chat lost: %d", c)
	}
	if s := e.randJoin(a); s.State != "waiting" {
		t.Errorf("a kept chat blocked a new random chat: %q", s.State)
	}
	// An outsider cannot keep someone else's chat.
	if c, _ := e.call("POST", "/random/"+m+"/keep", nil, e.user("Outsider")); c != 404 {
		t.Errorf("outsider keep: %d", c)
	}
}

func TestRandomConcurrentJoinsPairEveryoneExactlyOnce(t *testing.T) {
	e := setup(t)
	e.freshQueue()
	const n = 12
	ids := make([]string, n)
	for i := range ids {
		ids[i] = e.user("Racer")
	}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			e.call("POST", "/random/join", nil, id)
		}(id)
	}
	wg.Wait()
	matched := 0
	for _, id := range ids {
		switch e.randStatus(id).State {
		case "matched":
			matched++
		case "waiting":
		default:
			t.Error("unexpected state")
		}
		if k := e.count(`SELECT COUNT(*) FROM matches WHERE origin='random' AND status='matched' AND $1::uuid IN (user1_id, user2_id)`, id); k > 1 {
			t.Errorf("user in %d random chats", k)
		}
	}
	if matched != n {
		t.Errorf("%d of %d matched; with an even count everyone should be paired", matched, n)
	}
}

func TestRandomCancelAndRateLimit(t *testing.T) {
	e := setup(t)
	e.freshQueue()
	a := e.user("Waiter")
	e.randJoin(a)
	if c, _ := e.call("POST", "/random/cancel", nil, a); c != 204 {
		t.Fatalf("cancel: %d", c)
	}
	if s := e.randStatus(a); s.State != "idle" {
		t.Errorf("after cancel: %q", s.State)
	}
	var last int
	for i := 0; i < 22; i++ {
		last, _ = e.call("POST", "/random/join", nil, a)
	}
	if last != 429 {
		t.Errorf("joining is not rate limited: %d", last)
	}
}

func TestMaskedTruthQueueIsSeparateFromPlainChat(t *testing.T) {
	e := setup(t)
	e.freshQueue()
	chatter, player1, player2 := e.user("Chatter"), e.user("PlayerOne"), e.user("PlayerTwo")

	join := func(uid, style string) randStatus {
		c, b := e.call("POST", "/random/join", map[string]string{"style": style}, uid)
		if c != 200 {
			t.Fatalf("join %s: %d %s", style, c, b)
		}
		var s randStatus
		decodeInto(t, b, &s)
		return s
	}
	if s := join(chatter, "chat"); s.State != "waiting" {
		t.Fatalf("chatter: %q", s.State)
	}
	// A player does not get paired with someone who only wants a plain chat.
	if s := join(player1, "truth"); s.State != "waiting" {
		t.Fatalf("player paired across queues: %q", s.State)
	}
	s := join(player2, "truth")
	if s.State != "matched" || s.Session == nil {
		t.Fatalf("players not paired: %+v", s)
	}
	var style string
	var origin string
	if err := e.db.QueryRow(context.Background(), `SELECT style, origin FROM matches WHERE id=$1::uuid`, s.Session.MatchID).Scan(&style, &origin); err != nil {
		t.Fatal(err)
	}
	if style != "truth" || origin != "random" {
		t.Errorf("match: style=%q origin=%q", style, origin)
	}
	// Still anonymous.
	if !s.Session.Peer.Anonymous {
		t.Error("masked truth peer is not anonymous")
	}
	var raw struct {
		Session struct {
			Style string `json:"style"`
		} `json:"session"`
	}
	_, b := e.call("GET", "/random/status", nil, player1)
	decodeInto(t, b, &raw)
	if raw.Session.Style != "truth" {
		t.Errorf("status style: %q", raw.Session.Style)
	}
	// The plain chatter is still waiting, and a plain joiner pairs with them.
	if st := e.randStatus(chatter); st.State != "waiting" {
		t.Errorf("chatter: %q", st.State)
	}
	if c, _ := e.call("POST", "/random/join", map[string]string{"style": "cheese"}, e.user("Odd")); c != 422 {
		t.Errorf("bad style: %d", c)
	}
}
