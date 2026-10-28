package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func (e *env) videoComment(uid, id, body string) (int, commentJSON) {
	e.t.Helper()
	c, b := e.call("POST", "/videos/"+id+"/comments", map[string]string{"body": body}, uid)
	var out commentJSON
	if c == 201 {
		if err := json.Unmarshal(b, &out); err != nil {
			e.t.Fatal(err)
		}
	}
	return c, out
}

func TestVideoCommentsAndFollowingFeed(t *testing.T) {
	e := setup(t)
	owner, fan, shy, stranger := e.user("Clip Owner"), e.user("Clip Fan"), e.user("Clip Shy"), e.user("Clip Stranger")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)
	e.register(owner, "owner-device-token-aaaaaaaaaaaaaa", "android")
	c, v := e.uploadVideo(owner, "short", "Sunset walk", fakeMP4(5, 500))
	if c != 201 {
		t.Fatalf("upload: %d", c)
	}
	// Other clips in the feed come from people the fan does not follow.
	_, other := e.uploadVideo(stranger, "short", "Somebody else", fakeMP4(4, 500))

	// Comments: named only when shown in full; the owner is marked.
	if c, cm := e.videoComment(fan, v.ID, "Beautiful light"); c != 201 || cm.Name != "Clip Fan" || cm.Ref == "" {
		t.Fatalf("fan: %d %+v", c, cm)
	}
	if got := e.waitPush(1); len(got) != 1 || got[0].Msg.Data["kind"] != "comment" || strings.Contains(got[0].Msg.Body, "Fan") {
		t.Errorf("owner notification: %+v", got)
	}
	if c, cm := e.videoComment(shy, v.ID, "Hidden fan"); c != 201 || cm.Name != "Someone" || cm.Ref != "" {
		t.Fatalf("anonymous: %d %+v", c, cm)
	}
	if c, cm := e.videoComment(owner, v.ID, "Thank you both"); c != 201 || !cm.Author {
		t.Fatalf("owner: %d %+v", c, cm)
	}
	if c, _ := e.videoComment(fan, v.ID, "x"); c != 422 {
		t.Errorf("too short: %d", c)
	}
	_, b := e.call("GET", "/videos/"+v.ID+"/comments", nil, stranger)
	var list struct {
		Comments []commentJSON `json:"comments"`
	}
	if err := json.Unmarshal(b, &list); err != nil || len(list.Comments) != 3 || list.Comments[1].Name != "Someone" {
		t.Errorf("list: %s", b)
	}
	if !strings.Contains(string(mustFeed(e, fan, "")), `"comments":3`) {
		t.Errorf("feed lacks the comment count")
	}

	// Following filter.
	e.call("POST", "/follows/"+e.refOf(owner), nil, fan)
	f := string(mustFeed(e, fan, "?following=1"))
	if !strings.Contains(f, v.ID) || strings.Contains(f, other.ID) || !strings.Contains(f, `"following":true`) {
		t.Errorf("following feed: %s", f)
	}

	// Rights and reports.
	var fanComment string
	for _, cm := range list.Comments {
		if cm.Body == "Beautiful light" {
			fanComment = cm.ID
		}
	}
	if c, _ := e.call("DELETE", "/videos/comments/"+fanComment, nil, stranger); c != 404 {
		t.Errorf("stranger deleting: %d", c)
	}
	if c, _ := e.call("POST", "/videos/comments/"+fanComment+"/report", map[string]string{"reason": "spam"}, stranger); c != 204 {
		t.Errorf("report: %d", c)
	}
	if c, _ := e.call("DELETE", "/videos/comments/"+fanComment, nil, owner); c != 204 {
		t.Errorf("owner deleting: %d", c)
	}

	// A block hides the video and its comments.
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(owner)}, stranger)
	if c, _ := e.call("GET", "/videos/"+v.ID+"/comments", nil, stranger); c != 404 {
		t.Errorf("comments across a block: %d", c)
	}
}

func mustFeed(e *env, uid, query string) []byte {
	e.t.Helper()
	c, b := e.call("GET", "/videos/feed"+query, nil, uid)
	if c != 200 {
		e.t.Fatalf("feed: %d", c)
	}
	return b
}
