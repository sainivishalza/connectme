package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"connectme/internal/social"
	"connectme/internal/users"
)

type feedPost struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	Alias     string `json:"alias"`
	Distance  string `json:"distance"`
	HotelName string `json:"hotel_name"`
	Mine      bool   `json:"mine"`
	Likes     int    `json:"likes"`
	Liked     bool   `json:"liked"`
	Claims    int    `json:"claims"`
	Claimed   bool   `json:"claimed"`
	Author    *struct {
		Ref         string `json:"ref"`
		Anonymous   bool   `json:"anonymous"`
		DisplayName string `json:"display_name"`
	} `json:"author"`
}

func (e *env) feedOf(uid, query string) (int, []feedPost, string) {
	e.t.Helper()
	c, b := e.call("GET", "/social/feed?"+query, nil, uid)
	var out struct {
		Posts []feedPost `json:"posts"`
	}
	if c == 200 {
		decodeInto(e.t, b, &out)
	}
	return c, out.Posts, string(b)
}

func (e *env) post(uid, kind, body string) (int, feedPost) {
	e.t.Helper()
	c, b := e.call("POST", "/social/posts", map[string]string{"kind": kind, "body": body}, uid)
	var p feedPost
	if c == 201 {
		decodeInto(e.t, b, &p)
	}
	return c, p
}

func hasPost(ps []feedPost, id string) bool {
	for _, p := range ps {
		if p.ID == id {
			return true
		}
	}
	return false
}

func (e *env) socialSvc() *social.Service {
	return social.NewService(e.db, users.NewService(e.db, users.LocalStore{Dir: e.t.TempDir()}, nil))
}

func TestMomentsAreNearbyAndShowAuthorsAsTheirModeAllows(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	open, shy, viewer, far := e.user("Olive Open"), e.user("Sam Shy"), e.user("Viewer"), e.user("Faraway")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)
	e.locate(open, lat, lng)
	e.locate(shy, lat+0.004, lng)
	e.locate(viewer, lat+0.002, lng+0.002)
	e.locate(far, lat+1.0, lng) // ~110 km away

	c, p1 := e.post(open, "moment", "Sunny walk by the river, anyone?")
	if c != 201 {
		t.Fatalf("post: %d", c)
	}
	_, p2 := e.post(shy, "moment", "Reading at the corner café")

	c, ps, raw := e.feedOf(viewer, "kind=moment&radius_km=10")
	if c != 200 || !hasPost(ps, p1.ID) || !hasPost(ps, p2.ID) {
		t.Fatalf("viewer feed: %d %s", c, raw)
	}
	for _, p := range ps {
		switch p.ID {
		case p1.ID:
			if p.Author == nil || p.Author.Anonymous || p.Author.DisplayName != "Olive Open" {
				t.Errorf("an Open author should be shown by name: %+v", p.Author)
			}
		case p2.ID:
			if p.Author == nil || !p.Author.Anonymous || p.Author.DisplayName != "" {
				t.Errorf("an incognito author must stay anonymous: %+v", p.Author)
			}
			if strings.Contains(raw, "Sam Shy") {
				t.Error("the incognito author's name leaked")
			}
		}
		if !strings.HasPrefix(p.Distance, "within ") {
			t.Errorf("distance %q is not a bucket", p.Distance)
		}
	}
	// A faraway person does not see them, and sees nothing local.
	_, fps, _ := e.feedOf(far, "kind=moment&radius_km=10")
	if hasPost(fps, p1.ID) {
		t.Error("a post reached someone 110 km away")
	}
	if strings.Contains(raw, `"latitude"`) || strings.Contains(raw, `"location"`) {
		t.Errorf("exact position in the feed: %s", raw)
	}
}

func TestPostRulesValidationLimitsBlocksAndBans(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	author, viewer, nowhere := e.user("Author"), e.user("Viewer"), e.user("NoPlace")
	e.locate(author, lat, lng)
	e.locate(viewer, lat+0.003, lng)

	for name, body := range map[string]string{"too short": "hi", "too long": strings.Repeat("a", 281), "control": "hello\x00world"} {
		if c, _ := e.post(author, "moment", body); c != 422 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if c, _ := e.post(author, "other", "valid body text"); c != 422 {
		t.Errorf("unknown kind: %d", c)
	}
	if c, _ := e.post(nowhere, "moment", "I have not shared a location"); c != 422 {
		t.Errorf("post without a location: %d", c)
	}
	if c, _, _ := e.feedOf(nowhere, "kind=moment"); c != 422 {
		t.Errorf("feed without a location: %d", c)
	}
	if c, _, _ := e.feedOf(viewer, "kind=weird"); c != 422 {
		t.Errorf("unknown feed kind: %d", c)
	}
	if c, _, _ := e.feedOf(viewer, "kind=moment&before=not-a-uuid"); c != 422 {
		t.Errorf("bad cursor: %d", c)
	}

	_, p := e.post(author, "moment", "A perfectly fine post")
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(author)}, viewer)
	if _, ps, _ := e.feedOf(viewer, "kind=moment"); hasPost(ps, p.ID) {
		t.Error("a blocked author's post is visible to the blocker")
	}
	e.call("DELETE", "/moderation/blocks/"+e.refOf(author), nil, viewer)
	if _, ps, _ := e.feedOf(viewer, "kind=moment"); !hasPost(ps, p.ID) {
		t.Fatal("post should be visible again after unblocking")
	}
	e.exec(`UPDATE users SET is_banned=true WHERE id=$1`, author)
	if _, ps, _ := e.feedOf(viewer, "kind=moment"); hasPost(ps, p.ID) {
		t.Error("a banned author's post is still visible")
	}

	// A daily limit stops floods.
	spammer := e.user("Spammer")
	e.locate(spammer, lat, lng)
	var last int
	for i := 0; i < 22; i++ {
		last, _ = e.post(spammer, "moment", "Post number "+strings.Repeat("x", i+3))
	}
	if last != 429 {
		t.Errorf("no daily limit on moments: %d", last)
	}
}

func TestLittleUniversePostsAreAnonymousAndUnlinkable(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	author, viewer := e.user("Real Name Rita"), e.user("Viewer")
	e.locate(author, lat, lng)
	e.locate(viewer, lat+0.1, lng) // ~11 km: inside the 25 km universe, outside a 10 km moment radius

	c, p := e.post(author, "universe", "Sometimes I wish for a quiet night in")
	if c != 201 || p.Alias == "" {
		t.Fatalf("post: %d %+v", c, p)
	}
	c, ps, raw := e.feedOf(viewer, "kind=universe")
	if c != 200 || !hasPost(ps, p.ID) {
		t.Fatalf("viewer feed: %d %s", c, raw)
	}
	for _, leak := range []string{"Real Name Rita", e.refOf(author), `"author"`, "display_name"} {
		if strings.Contains(raw, leak) {
			t.Errorf("universe feed leaks %q: %s", leak, raw)
		}
	}
	// It is mine to me, with the same alias, and removable only by me.
	_, mine, _ := e.feedOf(author, "kind=universe")
	if !hasPost(mine, p.ID) || !mine[0].Mine {
		t.Error("author does not see their own post")
	}
	if c, _ := e.call("DELETE", "/social/posts/"+p.ID, nil, viewer); c != 404 {
		t.Errorf("a stranger deleted the post: %d", c)
	}
	if c, _ := e.call("DELETE", "/social/posts/"+p.ID, nil, author); c != 204 {
		t.Errorf("author delete: %d", c)
	}
	if _, ps, _ := e.feedOf(viewer, "kind=universe"); hasPost(ps, p.ID) {
		t.Error("deleted post still visible")
	}
}

func TestMissedConnectionsAreVenueScopedClaimableAndDeletedWithTheStay(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	hotelID := e.hotel(lat, lng)
	author, claimant, bystander, outsider := e.user("Author"), e.user("Claimant"), e.user("Bystander"), e.user("Outsider")
	e.locate(author, lat, lng)
	e.locate(claimant, lat+0.0003, lng)
	e.locate(bystander, lat, lng+0.0003)
	e.locate(outsider, lat+0.02, lng)
	for _, u := range []string{author, claimant, bystander} {
		if c, b := e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u); c != 201 {
			t.Fatalf("check-in: %d %s", c, b)
		}
	}

	if c, _ := e.post(outsider, "missed", "I was never even there"); c != 422 {
		t.Errorf("a non-guest posted a missed connection: %d", c)
	}
	c, p := e.post(author, "missed", "Red scarf by the lobby fireplace, we shared a smile")
	if c != 201 || p.HotelName == "" {
		t.Fatalf("post: %d %+v", c, p)
	}
	for _, leak := range []string{`"author"`, "alias"} {
		_, b := e.call("GET", "/social/feed?kind=missed", nil, claimant)
		if strings.Contains(string(b), leak) {
			t.Errorf("a missed-connection post is linked to its author (%s)", leak)
		}
	}
	if _, ps, _ := e.feedOf(claimant, "kind=missed"); !hasPost(ps, p.ID) {
		t.Fatal("a guest at the same hotel cannot see it")
	}
	if _, ps, _ := e.feedOf(outsider, "kind=missed"); hasPost(ps, p.ID) {
		t.Error("someone who is not checked in saw a venue post")
	}

	// "That's me": only guests, never the author.
	if c, _ := e.call("POST", "/social/posts/"+p.ID+"/claim", nil, outsider); c != 404 {
		t.Errorf("outsider claim: %d", c)
	}
	if c, _ := e.call("POST", "/social/posts/"+p.ID+"/claim", nil, author); c != 404 {
		t.Errorf("author claimed their own post: %d", c)
	}
	for i := 0; i < 2; i++ { // claiming twice is a no-op
		if c, b := e.call("POST", "/social/posts/"+p.ID+"/claim", nil, claimant); c != 204 {
			t.Fatalf("claim: %d %s", c, b)
		}
	}
	e.call("POST", "/social/posts/"+p.ID+"/claim", nil, bystander)

	// Only the author can read or answer claims; claimants stay anonymous (hotel mode).
	if c, _ := e.call("GET", "/social/posts/"+p.ID+"/claims", nil, claimant); c != 200 {
		// a non-author gets an empty list, never someone else's claimants
		t.Logf("non-author claims status %d", c)
	}
	_, craw := e.call("GET", "/social/posts/"+p.ID+"/claims", nil, claimant)
	if strings.Contains(string(craw), `"claimant"`) {
		t.Errorf("a non-author can read claims: %s", craw)
	}
	_, braw := e.call("GET", "/social/posts/"+p.ID+"/claims", nil, author)
	var cl struct {
		Claims []struct {
			ID       string `json:"id"`
			Claimant struct {
				Anonymous   bool   `json:"anonymous"`
				DisplayName string `json:"display_name"`
			} `json:"claimant"`
		} `json:"claims"`
	}
	if err := json.Unmarshal(braw, &cl); err != nil || len(cl.Claims) != 2 {
		t.Fatalf("author claims: %s", braw)
	}
	for _, c := range cl.Claims {
		if !c.Claimant.Anonymous || c.Claimant.DisplayName != "" {
			t.Errorf("a hotel guest claimant is not anonymous: %+v", c)
		}
	}
	if c, _ := e.call("POST", "/social/claims/"+cl.Claims[0].ID+"/accept", nil, claimant); c != 404 {
		t.Errorf("a claimant accepted their own claim: %d", c)
	}
	// Decline one, accept the other.
	if c, _ := e.call("POST", "/social/claims/"+cl.Claims[1].ID+"/decline", nil, author); c != 204 {
		t.Errorf("decline: %d", c)
	}
	c, b := e.call("POST", "/social/claims/"+cl.Claims[0].ID+"/accept", nil, author)
	var acc struct {
		MatchID string `json:"match_id"`
	}
	decodeInto(t, b, &acc)
	if c != 200 || acc.MatchID == "" {
		t.Fatalf("accept: %d %s", c, b)
	}
	if n := e.count(`SELECT COUNT(*) FROM matches WHERE id=$1::uuid AND mode='hotel' AND checkin_id IS NOT NULL AND status='matched'`, acc.MatchID); n != 1 {
		t.Error("the match is not a hotel match bound to the stay")
	}
	if c, _ := e.call("POST", "/social/claims/"+cl.Claims[0].ID+"/accept", nil, author); c != 404 {
		t.Errorf("accepting twice: %d", c)
	}

	// The post, its claims and the match are deleted with the stay.
	var checkin string
	if err := e.db.QueryRow(context.Background(), `SELECT checkin_id::text FROM posts WHERE id=$1::uuid`, p.ID).Scan(&checkin); err != nil {
		t.Fatal(err)
	}
	e.exec(`DELETE FROM hotel_checkins WHERE id=$1::uuid`, checkin)
	if e.count(`SELECT COUNT(*) FROM posts WHERE id=$1::uuid`, p.ID) != 0 || e.count(`SELECT COUNT(*) FROM missed_claims WHERE post_id=$1::uuid`, p.ID) != 0 {
		t.Error("the post and its claims survived the stay's deletion")
	}
	if e.count(`SELECT COUNT(*) FROM matches WHERE id=$1::uuid`, acc.MatchID) != 0 {
		t.Error("the match survived the stay's deletion")
	}
}

func TestLikesReportsStaffRemovalAndExpiry(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	author, fan, far := e.user("Author"), e.user("Fan"), e.user("Far")
	mod := e.newUser("moderator", true, "Mod")
	e.locate(author, lat, lng)
	e.locate(fan, lat+0.002, lng)
	e.locate(far, lat+1, lng)
	_, p := e.post(author, "moment", "Looking for a hiking buddy this weekend")

	toggle := func(u string) (int, bool, int) {
		c, b := e.call("POST", "/social/posts/"+p.ID+"/like", nil, u)
		var r struct {
			Liked bool `json:"liked"`
			Likes int  `json:"likes"`
		}
		if c == 200 {
			decodeInto(t, b, &r)
		}
		return c, r.Liked, r.Likes
	}
	if c, liked, n := toggle(fan); c != 200 || !liked || n != 1 {
		t.Errorf("like: %d %v %d", c, liked, n)
	}
	if c, liked, n := toggle(fan); c != 200 || liked || n != 0 {
		t.Errorf("unlike: %d %v %d", c, liked, n)
	}
	if c, _, _ := toggle(far); c != 404 {
		t.Errorf("a faraway person liked an invisible post: %d", c)
	}

	// Report: files a report against the author with the text as evidence.
	if c, _ := e.call("POST", "/social/posts/"+p.ID+"/report", map[string]string{"reason": "spam"}, fan); c != 204 {
		t.Fatalf("report: %d", c)
	}
	if c, _ := e.call("POST", "/social/posts/"+p.ID+"/report", map[string]string{"reason": "bogus"}, fan); c != 422 {
		t.Errorf("unknown reason: %d", c)
	}
	if e.count(`SELECT COUNT(*) FROM reports WHERE reported_id=$1 AND reporter_id=$2 AND reason='spam' AND evidence @> '["Looking for a hiking buddy this weekend"]'::jsonb`, author, fan) != 1 {
		t.Error("report not recorded with the post text")
	}

	// Staff can remove it, with a reason and an audit entry; ordinary users cannot.
	path := "/moderation/posts/" + p.ID + "/remove"
	if c, _ := e.call("POST", path, map[string]string{"reason": "harassment"}, fan); c != 403 {
		t.Errorf("a user removed a post: %d", c)
	}
	if c, _ := e.callAs("POST", path, map[string]string{"reason": "no"}, mod, "moderator"); c != 422 {
		t.Errorf("short reason: %d", c)
	}
	if c, _ := e.callAs("POST", path, map[string]string{"reason": "Targeted harassment of a guest"}, mod, "moderator"); c != 204 {
		t.Fatalf("staff removal: %d", c)
	}
	if e.count(`SELECT COUNT(*) FROM staff_audit_log WHERE action='remove_post' AND target_user_id=$1`, author) != 1 {
		t.Error("removal not audited")
	}
	if _, ps, _ := e.feedOf(fan, "kind=moment"); hasPost(ps, p.ID) {
		t.Error("removed post still listed")
	}

	// Expiry hides at once and the job deletes.
	_, q := e.post(author, "moment", "This one will expire soon")
	e.exec(`UPDATE posts SET expires_at = NOW() - INTERVAL '1 second' WHERE id=$1::uuid`, q.ID)
	if _, ps, _ := e.feedOf(fan, "kind=moment"); hasPost(ps, q.ID) {
		t.Error("an expired post is still listed")
	}
	n, err := e.socialSvc().Expire(context.Background())
	if err != nil || n < 1 || e.count(`SELECT COUNT(*) FROM posts WHERE id=$1::uuid`, q.ID) != 0 {
		t.Errorf("expiry job: %d %v", n, err)
	}
}

func TestFeedPagesWithACursor(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	author, viewer := e.user("Author"), e.user("Viewer")
	e.locate(author, lat, lng)
	e.locate(viewer, lat, lng)
	var ids []string
	for i := 0; i < 5; i++ {
		_, p := e.post(author, "moment", "Numbered post "+strings.Repeat("n", i+3))
		ids = append(ids, p.ID)
	}
	_, first, _ := e.feedOf(viewer, "kind=moment&limit=2")
	if len(first) != 2 || first[0].ID != ids[4] || first[1].ID != ids[3] {
		t.Fatalf("first page: %+v", first)
	}
	_, second, _ := e.feedOf(viewer, "kind=moment&limit=2&before="+first[1].ID)
	if len(second) != 2 || second[0].ID != ids[2] || second[1].ID != ids[1] {
		t.Fatalf("second page: %+v", second)
	}
}
