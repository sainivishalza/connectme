package integration

import (
	"strings"
	"testing"
)

type commView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	HotelName string `json:"hotel_name"`
	Members   int    `json:"members"`
	Joined    bool   `json:"joined"`
	Owner     bool   `json:"owner"`
	Distance  string `json:"distance"`
}

func (e *env) createCommunity(uid, name string) (int, commView) {
	e.t.Helper()
	c, b := e.call("POST", "/communities/", map[string]any{"name": name, "description": "A friendly group", "tag": "Hiking"}, uid)
	var v commView
	if c == 201 {
		decodeInto(e.t, b, &v)
	}
	return c, v
}

func (e *env) nearbyCommunities(uid string) []commView {
	e.t.Helper()
	c, b := e.call("GET", "/communities/nearby", nil, uid)
	if c != 200 {
		e.t.Fatalf("nearby communities: %d %s", c, b)
	}
	var out struct {
		Communities []commView `json:"communities"`
	}
	decodeInto(e.t, b, &out)
	return out.Communities
}

func findComm(cs []commView, id string) *commView {
	for i := range cs {
		if cs[i].ID == id {
			return &cs[i]
		}
	}
	return nil
}

func (e *env) communityPosts(uid, id string) (int, []feedPost) {
	e.t.Helper()
	c, b := e.call("GET", "/communities/"+id+"/posts", nil, uid)
	var out struct {
		Posts []feedPost `json:"posts"`
	}
	if c == 200 {
		decodeInto(e.t, b, &out)
	}
	return c, out.Posts
}

func TestInterestCommunitiesAreLocalAndMembersOnly(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	owner, near, far, nowhere := e.user("Owner Olive"), e.user("Neighbour"), e.user("Faraway"), e.user("NoPlace")
	e.locate(owner, lat, lng)
	e.locate(near, lat+0.05, lng) // ~5 km
	e.locate(far, lat+2, lng)     // ~220 km

	if c, _ := e.createCommunity(nowhere, "Weekend hikers"); c != 422 {
		t.Errorf("create without a location: %d", c)
	}
	for name, body := range map[string]map[string]any{
		"short name": {"name": "ab"}, "long description": {"name": "Good name", "description": strings.Repeat("d", 201)},
		"bad radius": {"name": "Good name", "radius_km": 500},
	} {
		if c, _ := e.call("POST", "/communities/", body, owner); c != 422 {
			t.Errorf("%s: %d", name, c)
		}
	}
	c, comm := e.createCommunity(owner, "Weekend hikers")
	if c != 201 || !comm.Joined || !comm.Owner || comm.Members != 1 {
		t.Fatalf("create: %d %+v", c, comm)
	}

	// Nearby people see it; faraway people do not and cannot join.
	if v := findComm(e.nearbyCommunities(near), comm.ID); v == nil || v.Joined || !strings.HasPrefix(v.Distance, "within ") {
		t.Errorf("neighbour's view: %+v", v)
	}
	if findComm(e.nearbyCommunities(far), comm.ID) != nil {
		t.Error("a community 220 km away shows up nearby")
	}
	if c, _ := e.call("POST", "/communities/"+comm.ID+"/join", nil, far); c != 422 {
		t.Errorf("faraway join: %d", c)
	}
	if c, _ := e.call("POST", "/communities/"+comm.ID+"/join", nil, nowhere); c != 422 {
		t.Errorf("join without location: %d", c)
	}

	// Members-only feed.
	if c, _ := e.communityPosts(near, comm.ID); c != 404 {
		t.Errorf("a non-member read the feed: %d", c)
	}
	if c, _ := e.call("POST", "/communities/"+comm.ID+"/posts", map[string]string{"body": "Can I post without joining?"}, near); c != 404 {
		t.Errorf("a non-member posted: %d", c)
	}
	for i := 0; i < 2; i++ { // joining twice is a no-op
		if c, _ := e.call("POST", "/communities/"+comm.ID+"/join", nil, near); c != 200 {
			t.Fatalf("join: %d", c)
		}
	}
	if v := findComm(e.nearbyCommunities(owner), comm.ID); v == nil || v.Members != 2 {
		t.Errorf("member count: %+v", v)
	}
	c, b := e.call("POST", "/communities/"+comm.ID+"/posts", map[string]string{"body": "Saturday trail at 9?"}, near)
	var post feedPost
	decodeInto(t, b, &post)
	if c != 201 || post.Author == nil || post.Author.DisplayName != "Neighbour" {
		t.Fatalf("member post: %d %s", c, b)
	}
	if _, ps := e.communityPosts(owner, comm.ID); !hasPost(ps, post.ID) {
		t.Error("the owner does not see the member's post")
	}
	// Posts in a community never leak into the public feeds.
	_, pub, _ := e.feedOf(owner, "kind=moment")
	if hasPost(pub, post.ID) {
		t.Error("a community post appeared in the nearby moments feed")
	}

	// Moderation by the owner: delete a member's post; members cannot delete each other's.
	other := e.user("Other")
	e.locate(other, lat+0.01, lng)
	e.call("POST", "/communities/"+comm.ID+"/join", nil, other)
	if c, _ := e.call("DELETE", "/social/posts/"+post.ID, nil, other); c != 404 {
		t.Errorf("a member deleted someone else's post: %d", c)
	}
	if c, _ := e.call("DELETE", "/social/posts/"+post.ID, nil, owner); c != 204 {
		t.Errorf("owner could not remove a post: %d", c)
	}
	// Only the owner deletes the community.
	if c, _ := e.call("DELETE", "/communities/"+comm.ID, nil, near); c != 404 {
		t.Errorf("a member deleted the community: %d", c)
	}
}

func TestCommunityOwnershipLeavingAndLimits(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	owner, member := e.user("Owner"), e.user("Member")
	e.locate(owner, lat, lng)
	e.locate(member, lat+0.01, lng)

	_, c1 := e.createCommunity(owner, "Group one")
	e.createCommunity(owner, "Group two")
	e.createCommunity(owner, "Group three")
	if c, _ := e.createCommunity(owner, "Group four"); c != 429 {
		t.Errorf("more than three owned communities: %d", c)
	}

	e.call("POST", "/communities/"+c1.ID+"/join", nil, member)
	// The owner leaves: the longest-standing member inherits it.
	if c, _ := e.call("DELETE", "/communities/"+c1.ID+"/membership", nil, owner); c != 204 {
		t.Fatalf("owner leaves: %d", c)
	}
	if v := findComm(e.nearbyCommunities(member), c1.ID); v == nil || !v.Owner {
		t.Errorf("ownership not handed over: %+v", v)
	}
	// The last person leaving removes the community.
	if c, _ := e.call("DELETE", "/communities/"+c1.ID+"/membership", nil, member); c != 204 {
		t.Fatalf("last leaves: %d", c)
	}
	if e.count(`SELECT COUNT(*) FROM communities WHERE id=$1::uuid`, c1.ID) != 0 {
		t.Error("an empty community was kept")
	}
	if c, _ := e.call("DELETE", "/communities/"+c1.ID+"/membership", nil, member); c != 404 {
		t.Errorf("leaving twice: %d", c)
	}
}

func TestHotelCommunitiesFollowTheStay(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	hotelID := e.hotel(lat, lng)
	g1, g2, outsider := e.user("Guest One"), e.user("Guest Two"), e.user("Outsider")
	e.locate(g1, lat, lng)
	e.locate(g2, lat+0.0003, lng)
	e.locate(outsider, lat+0.03, lng)
	for _, g := range []string{g1, g2} {
		if c, b := e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, g); c != 201 {
			t.Fatalf("check-in: %d %s", c, b)
		}
	}

	var hc *commView
	for _, v := range e.nearbyCommunities(g1) {
		if v.Kind == "hotel" {
			vv := v
			hc = &vv
		}
	}
	if hc == nil || !hc.Joined || !strings.HasPrefix(hc.Name, "Guests at") {
		t.Fatalf("a guest is not in their hotel's community: %+v", hc)
	}
	e.nearbyCommunities(g2)
	if v := findComm(e.nearbyCommunities(g1), hc.ID); v == nil || v.Members != 2 {
		t.Errorf("both guests should be members: %+v", v)
	}
	// Outsiders cannot join, and a guest cannot leave while checked in.
	if c, _ := e.call("POST", "/communities/"+hc.ID+"/join", nil, outsider); c != 422 {
		t.Errorf("outsider joined a hotel community: %d", c)
	}
	if c, _ := e.call("DELETE", "/communities/"+hc.ID+"/membership", nil, g1); c != 403 {
		t.Errorf("leaving a hotel community mid-stay: %d", c)
	}
	// Guests talk; the posts are anonymous (hotel mode) and bound to the stay.
	c, b := e.call("POST", "/communities/"+hc.ID+"/posts", map[string]string{"body": "Anyone up for dinner by the pool?"}, g1)
	var post feedPost
	decodeInto(t, b, &post)
	if c != 201 {
		t.Fatalf("hotel community post: %d %s", c, b)
	}
	// The other guest sees it, and sees the author only as an anonymous guest.
	_, theirs := e.communityPosts(g2, hc.ID)
	if !hasPost(theirs, post.ID) {
		t.Fatal("the other guest does not see it")
	}
	for _, p := range theirs {
		if p.ID == post.ID && (p.Author == nil || !p.Author.Anonymous || p.Author.DisplayName != "") {
			t.Errorf("a hotel guest's post is not anonymous to others: %+v", p.Author)
		}
	}
	if c, _ := e.communityPosts(outsider, hc.ID); c != 404 {
		t.Errorf("an outsider read a hotel community: %d", c)
	}
	if e.count(`SELECT COUNT(*) FROM posts WHERE id=$1::uuid AND checkin_id IS NOT NULL`, post.ID) != 1 {
		t.Fatal("a hotel community post is not bound to the stay")
	}

	// When the stay is purged, the guest's membership and posts go with it.
	e.exec(`DELETE FROM hotel_checkins WHERE user_id=$1`, g1)
	if e.count(`SELECT COUNT(*) FROM posts WHERE id=$1::uuid`, post.ID) != 0 {
		t.Error("the post survived the stay's deletion")
	}
	if e.count(`SELECT COUNT(*) FROM community_members WHERE community_id=$1::uuid AND user_id=$2`, hc.ID, g1) != 0 {
		t.Error("the membership survived the stay's deletion")
	}
	if e.count(`SELECT COUNT(*) FROM community_members WHERE community_id=$1::uuid AND user_id=$2`, hc.ID, g2) != 1 {
		t.Error("the other guest was removed too")
	}
}

func TestReportingACommunityPostNeedsMembership(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	owner, member, outsider := e.user("Owner"), e.user("Member"), e.user("Outsider")
	e.locate(owner, lat, lng)
	e.locate(member, lat+0.01, lng)
	e.locate(outsider, lat+0.02, lng)
	_, comm := e.createCommunity(owner, "Book club")
	e.call("POST", "/communities/"+comm.ID+"/join", nil, member)
	_, b := e.call("POST", "/communities/"+comm.ID+"/posts", map[string]string{"body": "Buy my miracle supplements now"}, owner)
	var post feedPost
	decodeInto(t, b, &post)

	if c, _ := e.call("POST", "/social/posts/"+post.ID+"/report", map[string]string{"reason": "spam"}, outsider); c != 404 {
		t.Errorf("a non-member reported a members-only post: %d", c)
	}
	if c, _ := e.call("POST", "/social/posts/"+post.ID+"/report", map[string]string{"reason": "spam"}, member); c != 204 {
		t.Errorf("member report: %d", c)
	}
	if c, _ := e.call("POST", "/social/posts/"+post.ID+"/like", nil, outsider); c != 404 {
		t.Errorf("a non-member liked a members-only post: %d", c)
	}
}
