package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

type commentJSON struct {
	ID     string `json:"id"`
	Body   string `json:"body"`
	Name   string `json:"name"`
	Ref    string `json:"ref"`
	Author bool   `json:"author"`
	Mine   bool   `json:"mine"`
}

func (e *env) comments(uid, postID string) (int, []commentJSON) {
	e.t.Helper()
	c, b := e.call("GET", "/social/posts/"+postID+"/comments", nil, uid)
	var out struct {
		Comments []commentJSON `json:"comments"`
	}
	if c == 200 {
		if err := json.Unmarshal(b, &out); err != nil {
			e.t.Fatal(err)
		}
	}
	return c, out.Comments
}

func (e *env) comment(uid, postID, body string) (int, commentJSON) {
	e.t.Helper()
	c, b := e.call("POST", "/social/posts/"+postID+"/comments", map[string]string{"body": body}, uid)
	var out commentJSON
	if c == 201 {
		if err := json.Unmarshal(b, &out); err != nil {
			e.t.Fatal(err)
		}
	}
	return c, out
}

func TestCommentsFollowThePostsVisibilityAndNeverRevealAnonymousPeople(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	author, open, shy, blocked, far := e.user("Post Author"), e.user("Open Commenter"), e.user("Shy Commenter"), e.user("Blocked One"), e.user("Far Away")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)
	for _, u := range []string{author, open, shy, blocked} {
		e.locate(u, lat, lng)
	}
	e.locate(far, lat+3, lng)

	_, moment := e.post(author, "moment", "Anyone up for coffee near the park?")
	_, secret := e.post(author, "universe", "Tell me something nobody knows")

	// A moment: named commenters are shown in full, anonymous ones are only "Someone".
	if c, cm := e.comment(open, moment.ID, "Count me in"); c != 201 || cm.Name != "Open Commenter" || cm.Ref == "" || !cm.Mine {
		t.Fatalf("open comment: %d %+v", c, cm)
	}
	if c, cm := e.comment(shy, moment.ID, "Me too"); c != 201 || cm.Name != "Someone" || cm.Ref != "" {
		t.Fatalf("anonymous comment must not be named: %d %+v", c, cm)
	}
	if c, cm := e.comment(author, moment.ID, "Great, 10am"); c != 201 || !cm.Author {
		t.Fatalf("author reply: %d %+v", c, cm)
	}
	_, list := e.comments(blocked, moment.ID)
	if len(list) != 3 || list[0].Body != "Count me in" || list[1].Name != "Someone" {
		t.Fatalf("list: %+v", list)
	}

	// The feed carries the count.
	c, b := e.call("GET", "/social/feed?kind=moment&radius_km=10", nil, open)
	if c != 200 || !strings.Contains(string(b), `"comments":3`) {
		t.Errorf("feed lacks the comment count: %d %s", c, b)
	}

	// Universe: everyone, the author too, is a nickname; nicknames are stable per post and differ across posts.
	_, a1 := e.comment(open, secret.ID, "Honestly, I love rainy days")
	_, a2 := e.comment(open, secret.ID, "and bookshops")
	_, ao := e.comment(author, secret.ID, "That is nice")
	if a1.Ref != "" || strings.Contains(a1.Name, "Open") || a1.Name == "" || a1.Name != a2.Name || ao.Name != secret.Alias || !ao.Author {
		t.Errorf("universe names: %+v %+v %+v (alias %q)", a1, a2, ao, secret.Alias)
	}
	_, secret2 := e.post(author, "universe", "A second anonymous thought")
	_, b1 := e.comment(open, secret2.ID, "Another one")
	if b1.Name == a1.Name {
		t.Logf("nickname collision across posts is possible but unlikely: %q", b1.Name)
	}
	for _, uid := range []string{open, shy, author} {
		_, l := e.comments(uid, secret.ID)
		for _, cm := range l {
			if cm.Ref != "" || strings.Contains(cm.Name, "Commenter") || strings.Contains(cm.Name, "Post Author") {
				t.Errorf("universe comment reveals a person: %+v", cm)
			}
		}
	}

	// Out of range, missing or malformed.
	if c, _ := e.comment(far, moment.ID, "hello from far away"); c != 404 {
		t.Errorf("comment from out of range: %d", c)
	}
	if c, _ := e.comments(far, moment.ID); c != 404 {
		t.Errorf("list from out of range: %d", c)
	}
	if c, _ := e.comment(open, moment.ID, "x"); c != 422 {
		t.Errorf("too short: %d", c)
	}
	if c, _ := e.comment(open, "nope", "hello there"); c != 404 {
		t.Errorf("bad id: %d", c)
	}

	// A block hides each other's comments and stops new ones.
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(open)}, blocked)
	_, list = e.comments(blocked, moment.ID)
	for _, cm := range list {
		if cm.Name == "Open Commenter" {
			t.Errorf("blocked commenter still visible: %+v", cm)
		}
	}
	if c, _ := e.comment(open, moment.ID, "can you see this?"); c != 201 {
		t.Errorf("a block must not stop commenting for others: %d", c)
	}

	// Delete: your own, or anything on your post; not other people's elsewhere.
	_, mine := e.comment(shy, moment.ID, "I will delete this")
	if c, _ := e.call("DELETE", "/social/comments/"+mine.ID, nil, open); c != 404 {
		t.Errorf("deleting someone else's comment: %d", c)
	}
	if c, _ := e.call("DELETE", "/social/comments/"+mine.ID, nil, shy); c != 204 {
		t.Errorf("delete own: %d", c)
	}
	_, other := e.comment(shy, moment.ID, "post author may remove this")
	if c, _ := e.call("DELETE", "/social/comments/"+other.ID, nil, author); c != 204 {
		t.Errorf("author removes: %d", c)
	}

	// Report puts the text in front of staff; you cannot report yourself.
	if c, _ := e.call("POST", "/social/comments/"+a1.ID+"/report", map[string]string{"reason": "spam"}, author); c != 204 {
		t.Errorf("report: %d", c)
	}
	if c, _ := e.call("POST", "/social/comments/"+a1.ID+"/report", map[string]string{"reason": "bogus"}, author); c != 422 {
		t.Errorf("bad reason: %d", c)
	}

	// Deleting the post removes its comments.
	e.call("DELETE", "/social/posts/"+moment.ID, nil, author)
	if c, _ := e.comments(open, moment.ID); c != 404 {
		t.Errorf("comments of a deleted post: %d", c)
	}
}

func TestCommentsAreNotAllowedOnMissedConnections(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	hotelID := e.hotel(lat, lng)
	author, viewer := e.user("Missed Author"), e.user("Missed Viewer")
	e.locate(author, lat, lng)
	e.locate(viewer, lat+0.0003, lng)
	for _, u := range []string{author, viewer} {
		if c, b := e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u); c != 201 {
			t.Fatalf("check-in: %d %s", c, b)
		}
	}
	c, p := e.post(author, "missed", "Red scarf by the lobby fireplace, we shared a smile")
	if c != 201 {
		t.Fatalf("post: %d", c)
	}
	if c, _ := e.comment(viewer, p.ID, "that was me!"); c != 422 {
		t.Errorf("comment on a missed connection: %d", c)
	}
}

func TestCommentNotifiesThePostAuthorGenericallyAndRespectsThePreference(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	author, other := e.user("Notified Author"), e.user("Commenter Person")
	e.locate(author, lat, lng)
	e.locate(other, lat, lng)
	e.register(author, "author-device-token-aaaaaaaaaaaaaa", "android")
	_, p := e.post(author, "moment", "Anyone want to walk the dog?")

	e.comment(author, p.ID, "I will go first") // your own comment never notifies you
	if got := e.quiet(); len(got) != 0 {
		t.Errorf("notified of own comment: %+v", got)
	}
	e.comment(other, p.ID, "I would love to")
	got := e.waitPush(1)
	if len(got) != 1 || got[0].Msg.Data["kind"] != "comment" || got[0].Msg.Data["ref"] != p.ID ||
		strings.Contains(got[0].Msg.Body, "Commenter") || strings.Contains(got[0].Msg.Body, "love") {
		t.Fatalf("notification: %+v", got)
	}
	e.call("PUT", "/push/prefs", map[string]bool{"comments": false}, author)
	e.clearPushDedupe()
	e.comment(other, p.ID, "another one")
	if got := e.quiet(); len(got) != 0 {
		t.Errorf("sent despite opt-out: %+v", got)
	}
}
