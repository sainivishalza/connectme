package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fThread struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Author  string `json:"author"`
	Replies int    `json:"replies"`
	Mine    bool   `json:"mine"`
}

type fReply struct {
	ID     int64  `json:"id"`
	Body   string `json:"body"`
	Author string `json:"author"`
	Mine   bool   `json:"mine"`
}

func (e *env) newThread(uid, board, title, body string) (int, fThread) {
	e.t.Helper()
	c, b := e.call("POST", "/forums/boards/"+board+"/threads", map[string]string{"title": title, "body": body}, uid)
	var t fThread
	if c == 201 {
		decodeInto(e.t, b, &t)
	}
	return c, t
}

func (e *env) threadList(uid, board string) []fThread {
	e.t.Helper()
	c, b := e.call("GET", "/forums/boards/"+board+"/threads", nil, uid)
	if c != 200 {
		e.t.Fatalf("list: %d %s", c, b)
	}
	var r struct {
		Threads []fThread `json:"threads"`
	}
	decodeInto(e.t, b, &r)
	return r.Threads
}

func (e *env) replyList(uid, thread string) []fReply {
	e.t.Helper()
	c, b := e.call("GET", "/forums/threads/"+thread+"/replies", nil, uid)
	if c != 200 {
		e.t.Fatalf("replies: %d %s", c, b)
	}
	var r struct {
		Replies []fReply `json:"replies"`
	}
	decodeInto(e.t, b, &r)
	return r.Replies
}

// board makes a private board so threads left by earlier runs never interfere.
func (e *env) board() string {
	e.t.Helper()
	slug := "t-" + strings.ToLower(strings.ReplaceAll(e.t.Name(), "_", "-"))[:0] + fmt.Sprint(time.Now().UnixNano())
	e.exec(`INSERT INTO forum_boards (slug, title, kind) VALUES ($1, 'Test board', 'advice')`, slug)
	return slug
}

func TestForumsThreadsRepliesAndIdentity(t *testing.T) {
	e := setup(t)
	board := e.board()
	open1, open2, shy, shy2 := e.user("Olive"), e.user("Orson"), e.user("Shy"), e.user("Shy Two")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id IN ($1,$2)`, shy, shy2)

	// Seeded boards exist.
	c, b := e.call("GET", "/forums/boards", nil, open1)
	if c != 200 || !strings.Contains(string(b), "dating-advice") || !strings.Contains(string(b), "travel-tips") {
		t.Fatalf("boards: %d %s", c, b)
	}

	c, th := e.newThread(open1, board, "How do I start a chat?", "I always freeze after the match.")
	if c != 201 || th.Author != "Olive" || !th.Mine {
		t.Fatalf("thread: %d %+v", c, th)
	}
	// An Incognito author is an alias, never their name, stable inside a thread.
	_, anon := e.newThread(shy, board, "Is it okay to stay anonymous?", "Asking for myself.")
	list := e.threadList(open2, board)
	if len(list) != 2 {
		t.Fatalf("list: %+v", list)
	}
	for _, x := range list {
		if x.Mine {
			t.Error("another person's thread marked mine")
		}
		if x.Body != "" {
			t.Error("list returned thread bodies")
		}
		if strings.Contains(x.Author, "Shy") {
			t.Errorf("incognito author leaked: %q", x.Author)
		}
	}
	if _, got := e.getThread(open2, anon.ID); got.Author == "Shy" || !strings.Contains(got.Author, " ") || got.Body == "" {
		t.Errorf("thread view: %+v", got)
	}

	for _, who := range []string{open2, shy, shy2, shy} {
		if c, b := e.call("POST", "/forums/threads/"+th.ID+"/replies", map[string]string{"body": "Try asking about their tags."}, who); c != 201 {
			t.Fatalf("reply: %d %s", c, b)
		}
	}
	rs := e.replyList(open1, th.ID)
	if len(rs) != 4 || rs[0].Author != "Orson" || rs[0].Mine {
		t.Fatalf("replies: %+v", rs)
	}
	// Same person, same alias in a thread; different people, different aliases.
	if rs[1].Author != rs[3].Author || rs[1].Author == rs[2].Author || strings.Contains(rs[1].Author, "Shy") {
		t.Errorf("aliases: %q %q %q", rs[1].Author, rs[2].Author, rs[3].Author)
	}
	// The same person has a different alias in another thread (not linkable).
	e.call("POST", "/forums/threads/"+anon.ID+"/replies", map[string]string{"body": "Replying on my own thread too."}, shy)
	if own := e.replyList(shy, anon.ID); own[0].Author == rs[1].Author || !own[0].Mine {
		t.Errorf("alias reused across threads: %q vs %q", own[0].Author, rs[1].Author)
	}
	// Cursor: only replies after an id.
	_, b = e.call("GET", fmt.Sprintf("/forums/threads/%s/replies?after=%d", th.ID, rs[2].ID), nil, open1)
	if !strings.Contains(string(b), "Try asking") || strings.Count(string(b), `"id"`) != 1 {
		t.Errorf("after cursor: %s", b)
	}
	// Reply counts show in the list, and the most recently active thread leads.
	l := e.threadList(open2, board)
	if l[0].ID != anon.ID || l[1].ID != th.ID || l[1].Replies != 4 {
		t.Errorf("order/count: %+v", l)
	}

	// Own delete only.
	if c, _ := e.call("DELETE", "/forums/threads/"+th.ID, nil, open2); c != 404 {
		t.Errorf("other deleted thread: %d", c)
	}
	if c, _ := e.call("DELETE", fmt.Sprintf("/forums/replies/%d", rs[0].ID), nil, open2); c != 204 {
		t.Errorf("own reply delete: %d", c)
	}
	if got := e.replyList(open1, th.ID); len(got) != 3 {
		t.Errorf("after delete: %d", len(got))
	}
	if _, got := e.getThread(open1, th.ID); got.Replies != 3 {
		t.Errorf("reply count after delete: %d", got.Replies)
	}
	if c, _ := e.call("DELETE", "/forums/threads/"+th.ID, nil, open1); c != 204 {
		t.Errorf("own thread delete: %d", c)
	}
}

func (e *env) getThread(uid, id string) (int, fThread) {
	e.t.Helper()
	c, b := e.call("GET", "/forums/threads/"+id, nil, uid)
	var t fThread
	if c == 200 {
		decodeInto(e.t, b, &t)
	}
	return c, t
}

func TestForumsRulesBlocksReportsAndStaff(t *testing.T) {
	e := setup(t)
	board := e.board()
	author, reader, blocked := e.user("Writer"), e.user("Reader"), e.user("Blocked")
	unverified := e.newUser("user", false, "Unv")
	banned := e.user("Banned")
	e.exec(`UPDATE users SET is_banned=true WHERE id=$1`, banned)

	if c, _ := e.newThread(unverified, "travel-tips", "Best hostels in Lisbon?", "Looking for suggestions."); c != 403 {
		t.Errorf("unverified: %d", c)
	}
	if c, _ := e.newThread(author, "no-such-board", "A title here", "Some body text"); c != 404 {
		t.Errorf("missing board: %d", c)
	}
	for _, bad := range [][2]string{{"abc", "long enough body"}, {"A fine title", "x"}, {strings.Repeat("t", 121), "long enough body"}} {
		if c, _ := e.newThread(author, "travel-tips", bad[0], bad[1]); c != 422 {
			t.Errorf("bad input %q: %d", bad[0], c)
		}
	}
	c, th := e.newThread(author, board, "Best hostels in Lisbon?", "Looking for suggestions.")
	if c != 201 {
		t.Fatalf("thread: %d", c)
	}
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(blocked)}, author)
	if c, _ := e.getThread(blocked, th.ID); c != 404 {
		t.Errorf("blocked reads thread: %d", c)
	}
	if l := e.threadList(blocked, board); len(l) != 0 {
		t.Errorf("blocked sees thread in list: %+v", l)
	}

	// Reports.
	if c, _ := e.call("POST", "/forums/threads/"+th.ID+"/report", map[string]string{"reason": "bogus"}, reader); c != 422 {
		t.Errorf("bad reason: %d", c)
	}
	if c, _ := e.call("POST", "/forums/threads/"+th.ID+"/report", map[string]string{"reason": "spam"}, author); c != 404 {
		t.Errorf("self report: %d", c)
	}
	if c, b := e.call("POST", "/forums/threads/"+th.ID+"/report", map[string]string{"reason": "spam"}, reader); c != 204 {
		t.Fatalf("report: %d %s", c, b)
	}
	_, rp := e.call("POST", "/forums/threads/"+th.ID+"/replies", map[string]string{"body": "Great question."}, reader)
	var rep fReply
	decodeInto(t, rp, &rep)
	if c, _ := e.call("POST", fmt.Sprintf("/forums/replies/%d/report", rep.ID), map[string]string{"reason": "harassment"}, author); c != 204 {
		t.Errorf("report reply: %d", c)
	}

	// Staff removal needs ban permission and a reason, and is audited.
	admin := e.newUser("admin", true, "Admin")
	if c, _ := e.callAs("POST", fmt.Sprintf("/moderation/forum/reply/%d/remove", rep.ID), map[string]string{"reason": "no"}, admin, "admin"); c != 422 {
		t.Errorf("short reason: %d", c)
	}
	if c, b := e.callAs("POST", fmt.Sprintf("/moderation/forum/reply/%d/remove", rep.ID), map[string]string{"reason": "harassing the author"}, admin, "admin"); c != 204 {
		t.Fatalf("remove reply: %d %s", c, b)
	}
	if c, b := e.callAs("POST", "/moderation/forum/thread/"+th.ID+"/remove", map[string]string{"reason": "violates the rules"}, admin, "admin"); c != 204 {
		t.Fatalf("remove thread: %d %s", c, b)
	}
	if c, _ := e.getThread(reader, th.ID); c != 404 {
		t.Errorf("removed thread still readable: %d", c)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM staff_audit_log WHERE action='remove_forum_post'`).Scan(&n); err != nil || n < 2 {
		t.Errorf("audit rows: %d %v", n, err)
	}

	// Admins add city boards; ordinary members cannot.
	if c, _ := e.call("POST", "/forums/boards", map[string]string{"slug": "lisbon", "title": "Lisbon", "kind": "city"}, reader); c != 403 {
		t.Errorf("member created board: %d", c)
	}
	if c, b := e.callAs("POST", "/forums/boards", map[string]string{"slug": "lisbon-" + e.refOf(admin)[:6], "title": "Lisbon", "kind": "city"}, admin, "admin"); c != 201 {
		t.Errorf("admin board: %d %s", c, b)
	}
	if c, _ := e.callAs("POST", "/forums/boards", map[string]string{"slug": "Bad Slug", "title": "Lisbon", "kind": "city"}, admin, "admin"); c != 422 {
		t.Errorf("bad slug: %d", c)
	}
	_ = banned
}

func TestForumsDailyLimits(t *testing.T) {
	e := setup(t)
	u := e.user("Chatty")
	board := e.board()
	for i := 0; i < 10; i++ {
		if c, _ := e.newThread(u, board, fmt.Sprintf("Thread number %d", i), "Some content here"); c != 201 {
			t.Fatalf("thread %d: %d", i, c)
		}
	}
	if c, _ := e.newThread(u, board, "One thread too many", "Some content here"); c != 429 {
		t.Errorf("eleventh thread: %d", c)
	}
}
