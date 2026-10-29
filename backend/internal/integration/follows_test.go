package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

type followCards struct {
	Profiles []struct {
		Ref       string `json:"ref"`
		Followers int    `json:"followers"`
		Following bool   `json:"following"`
	} `json:"profiles"`
}

func (e *env) followList(uid, direction string) followCards {
	e.t.Helper()
	c, b := e.call("GET", "/follows/"+direction, nil, uid)
	if c != 200 {
		e.t.Fatalf("list %s: %d %s", direction, c, b)
	}
	var out followCards
	if err := json.Unmarshal(b, &out); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func TestFollowsAreOneWayVisibleOnlyAndPrivateInTheList(t *testing.T) {
	e := setup(t)
	host, fan, other, shy := e.user("Follow Host"), e.user("Follow Fan"), e.user("Follow Other"), e.user("Shy One")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)

	// Cannot follow yourself, an anonymous member, or something that does not exist.
	if c, _ := e.call("POST", "/follows/"+e.refOf(fan), nil, fan); c != 422 {
		t.Errorf("self follow: %d", c)
	}
	if c, _ := e.call("POST", "/follows/"+e.refOf(shy), nil, fan); c != 404 {
		t.Errorf("follow anonymous: %d", c)
	}
	if c, _ := e.call("POST", "/follows/not-a-ref", nil, fan); c != 404 {
		t.Errorf("follow garbage: %d", c)
	}

	// Following is idempotent and shows up as a count plus a flag, for the follower only.
	for i := 0; i < 2; i++ {
		if c, b := e.call("POST", "/follows/"+e.refOf(host), nil, fan); c != 204 {
			t.Fatalf("follow: %d %s", c, b)
		}
	}
	e.call("POST", "/follows/"+e.refOf(host), nil, other)
	_, b := e.call("GET", "/users/"+e.refOf(host), nil, fan)
	if !strings.Contains(string(b), `"followers":2`) || !strings.Contains(string(b), `"following":true`) {
		t.Errorf("follower view: %s", b)
	}
	_, b = e.call("GET", "/users/"+e.refOf(host), nil, e.user("Stranger"))
	if !strings.Contains(string(b), `"followers":2`) || strings.Contains(string(b), `"following"`) {
		t.Errorf("stranger view: %s", b)
	}

	if l := e.followList(fan, "following"); len(l.Profiles) != 1 || l.Profiles[0].Ref != e.refOf(host) {
		t.Errorf("following list: %+v", l)
	}
	if l := e.followList(host, "followers"); len(l.Profiles) != 2 {
		t.Errorf("followers list: %+v", l)
	}
	if c, _ := e.call("GET", "/follows/everyone", nil, fan); c != 404 {
		t.Errorf("unknown list: %d", c)
	}

	// A member who goes Incognito drops off both lists and cannot be followed again.
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, other)
	if l := e.followList(host, "followers"); len(l.Profiles) != 1 {
		t.Errorf("followers after incognito: %+v", l)
	}

	// Unfollow is idempotent.
	for i := 0; i < 2; i++ {
		if c, _ := e.call("DELETE", "/follows/"+e.refOf(host), nil, fan); c != 204 {
			t.Fatalf("unfollow: %d", c)
		}
	}
	if l := e.followList(fan, "following"); len(l.Profiles) != 0 {
		t.Errorf("after unfollow: %+v", l)
	}
}

func TestBlockingRemovesFollowsBothWays(t *testing.T) {
	e := setup(t)
	a, b := e.user("Block A"), e.user("Block B")
	e.call("POST", "/follows/"+e.refOf(b), nil, a)
	e.call("POST", "/follows/"+e.refOf(a), nil, b)
	if c, _ := e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(b)}, a); c != 204 {
		t.Fatalf("block: %d", c)
	}
	if len(e.followList(a, "following").Profiles)+len(e.followList(b, "following").Profiles) != 0 {
		t.Error("follows survived a block")
	}
	if c, _ := e.call("POST", "/follows/"+e.refOf(a), nil, b); c != 404 {
		t.Errorf("follow across a block: %d", c)
	}
}
