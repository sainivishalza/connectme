package integration

import (
	"context"
	"strings"
	"testing"
)

// marked returns a valid clip whose bytes make the stand-in scanner decide.
func marked(marker string) []byte { return append(fakeMP4(6, 0), []byte(marker)...) }

type queueItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	TargetRef string `json:"target_ref"`
	Summary   string `json:"summary"`
	Source    string `json:"source"`
}

func (e *env) queue(role, uid string) []queueItem {
	e.t.Helper()
	c, b := e.callAs("GET", "/moderation/queue", nil, uid, role)
	if c != 200 {
		e.t.Fatalf("queue: %d %s", c, b)
	}
	var r struct {
		Items []queueItem `json:"items"`
	}
	decodeInto(e.t, b, &r)
	return r.Items
}

func hasQueued(items []queueItem, id string) bool {
	for _, i := range items {
		if i.ID == id {
			return true
		}
	}
	return false
}

func TestFlaggedVideosAreHeldUntilAModeratorApproves(t *testing.T) {
	e := setup(t)
	author, viewer := e.user("Clipper"), e.user("Watcher")
	mod := e.newUser("moderator", true, "Mod")

	c, held := e.uploadVideo(author, "short", "late night", marked("FLAGME"))
	if c != 201 || held.Status != "held" {
		t.Fatalf("flagged upload: %d %+v", c, held)
	}
	// Held clips are invisible to everyone but the owner.
	for _, v := range e.videoFeed(viewer) {
		if v.ID == held.ID {
			t.Fatal("held clip reached the feed")
		}
	}
	if c, _ := e.ticket(viewer, held.ID); c != 404 {
		t.Errorf("viewer ticket for a held clip: %d", c)
	}
	if c, _ := e.call("POST", "/videos/"+held.ID+"/like", nil, viewer); c != 404 {
		t.Errorf("liked a held clip: %d", c)
	}
	if c, _ := e.ticket(author, held.ID); c != 200 {
		t.Errorf("owner cannot watch their own held clip: %d", c)
	}
	_, mine := e.call("GET", "/videos/mine", nil, author)
	if !strings.Contains(string(mine), `"status":"held"`) {
		t.Errorf("owner is not told it is held: %s", mine)
	}

	// It is in the staff queue with the scanner's reason.
	items := e.queue("moderator", mod)
	var item queueItem
	for _, i := range items {
		if i.ID == held.ID {
			item = i
		}
	}
	if item.Type != "video" || item.Source != "scanner" || !strings.Contains(item.Summary, "possible nudity") || item.TargetRef != e.refOf(author) {
		t.Fatalf("queue item: %+v", item)
	}
	_, st := e.callAs("GET", "/moderation/stats", nil, mod, "moderator")
	if !strings.Contains(string(st), `"held_videos":`) {
		t.Errorf("stats lack held videos: %s", st)
	}

	// A moderator may watch it (audited), ordinary members may not.
	if c, _ := e.call("GET", "/moderation/videos/"+held.ID+"/ticket", nil, viewer); c != 403 {
		t.Errorf("member used the staff ticket: %d", c)
	}
	c, b := e.callAs("GET", "/moderation/videos/"+held.ID+"/ticket", nil, mod, "moderator")
	if c != 200 || !strings.Contains(string(b), "/videos/file/"+held.ID) {
		t.Fatalf("staff ticket: %d %s", c, b)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM staff_audit_log WHERE action='view_video' AND target_user_id=$1`, author).Scan(&n); err != nil || n != 1 {
		t.Errorf("view not audited: %d %v", n, err)
	}

	// Approve: live, in the feed, off the queue, audited with the reviewer.
	if c, b := e.callAs("POST", "/moderation/videos/"+held.ID+"/approve", nil, mod, "moderator"); c != 204 {
		t.Fatalf("approve: %d %s", c, b)
	}
	if c, _ := e.callAs("POST", "/moderation/videos/"+held.ID+"/approve", nil, mod, "moderator"); c != 404 {
		t.Errorf("approved twice: %d", c)
	}
	if hasQueued(e.queue("moderator", mod), held.ID) {
		t.Error("approved clip is still queued")
	}
	found := false
	for _, v := range e.videoFeed(viewer) {
		found = found || v.ID == held.ID
	}
	if !found {
		t.Error("approved clip not in the feed")
	}
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM videos WHERE id=$1::uuid AND reviewed_by IS NOT NULL AND status='live'`, held.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("reviewer not recorded: %d %v", n, err)
	}
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM staff_audit_log WHERE action='approve_video' AND target_user_id=$1`, author).Scan(&n); err != nil || n != 1 {
		t.Errorf("approval not audited: %d %v", n, err)
	}
}

func TestBlockedVideosAreRefusedAndQueuedAsAutomaticFlags(t *testing.T) {
	e := setup(t)
	author := e.user("Blocked Clipper")
	mod := e.newUser("moderator", true, "Mod2")

	if c, _ := e.uploadVideo(author, "short", "", marked("BLOCKME")); c != 422 {
		t.Fatalf("blocked upload: %d", c)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM videos WHERE owner_id=$1`, author).Scan(&n); err != nil || n != 0 {
		t.Errorf("blocked clip stored: %d %v", n, err)
	}
	var report queueItem
	for _, i := range e.queue("moderator", mod) {
		if i.TargetRef == e.refOf(author) && i.Type == "report" {
			report = i
		}
	}
	if report.Source != "automatic" || !strings.Contains(report.Summary, "known abuse hash") {
		t.Errorf("no automatic flag for the blocked upload: %+v", report)
	}
}

func TestScannerOutageHoldsClipsInsteadOfPublishingThem(t *testing.T) {
	e := setup(t)
	author, viewer := e.user("Unlucky"), e.user("Other Watcher")
	c, v := e.uploadVideo(author, "short", "", marked("BROKEN"))
	if c != 201 || v.Status != "held" {
		t.Fatalf("upload during outage: %d %+v", c, v)
	}
	for _, x := range e.videoFeed(viewer) {
		if x.ID == v.ID {
			t.Fatal("unscanned clip published")
		}
	}
	var note string
	if err := e.db.QueryRow(context.Background(), `SELECT scan_note FROM videos WHERE id=$1::uuid`, v.ID).Scan(&note); err != nil || note != "scanner unavailable" {
		t.Errorf("note: %q %v", note, err)
	}
}

func TestHeldReelsAreHiddenAndNotAdvertised(t *testing.T) {
	e := setup(t)
	owner, viewer := e.user("Reeler"), e.user("Reel Viewer")
	mod := e.newUser("moderator", true, "Mod3")
	c, reel := e.uploadVideo(owner, "reel", "", marked("FLAGME"))
	if c != 201 || reel.Status != "held" {
		t.Fatalf("reel: %d %+v", c, reel)
	}
	if c, _ := e.call("GET", "/videos/reel/"+e.refOf(owner), nil, viewer); c != 404 {
		t.Errorf("held reel visible: %d", c)
	}
	if c, _ := e.call("GET", "/videos/reel/"+e.refOf(owner), nil, owner); c != 200 {
		t.Errorf("owner cannot see their held reel: %d", c)
	}
	_, body := e.call("GET", "/users/"+e.refOf(owner), nil, viewer)
	if strings.Contains(string(body), "reel_s") {
		t.Errorf("held reel advertised: %s", body)
	}
	e.callAs("POST", "/moderation/videos/"+reel.ID+"/approve", nil, mod, "moderator")
	if c, _ := e.call("GET", "/videos/reel/"+e.refOf(owner), nil, viewer); c != 200 {
		t.Errorf("approved reel hidden: %d", c)
	}
	// Removing is the other outcome.
	_, other := e.uploadVideo(owner, "short", "again", marked("FLAGME"))
	if c, b := e.callAs("POST", "/moderation/videos/"+other.ID+"/remove", map[string]string{"reason": "does not meet the rules"}, mod, "moderator"); c != 204 && c != 403 {
		t.Errorf("remove: %d %s", c, b)
	}
}
