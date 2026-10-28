package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeMP4 builds a minimal MP4: an ftyp box and a moov box holding an mvhd
// that declares the given length. The sniffer and duration reader accept it;
// a real player would not, which is fine for server tests.
func fakeMP4(seconds float64, pad int) []byte {
	box := func(typ string, payload []byte) []byte {
		b := make([]byte, 8, 8+len(payload))
		binary.BigEndian.PutUint32(b, uint32(8+len(payload)))
		copy(b[4:], typ)
		return append(b, payload...)
	}
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], 1000)
	binary.BigEndian.PutUint32(mvhd[16:], uint32(seconds*1000))
	out := box("ftyp", append([]byte("isom"), 0, 0, 2, 0))
	out = append(out, box("moov", box("mvhd", mvhd))...)
	return append(out, make([]byte, pad)...)
}

type vid struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Seconds int    `json:"seconds"`
	Mine    bool   `json:"mine"`
	Status  string `json:"status"`
	Likes   int    `json:"likes"`
	Liked   bool   `json:"liked"`
	Author  *struct {
		Ref  string `json:"ref"`
		Name string `json:"name"`
	} `json:"author"`
}

func (e *env) uploadVideo(uid, kind, caption string, data []byte) (int, vid) {
	e.t.Helper()
	c, b := e.raw("POST", "/videos/?kind="+kind+"&caption="+url.QueryEscape(caption), data, uid)
	var r struct {
		Video vid `json:"video"`
	}
	if c == 201 {
		decodeInto(e.t, b, &r)
	}
	return c, r.Video
}

func (e *env) videoFeed(uid string) []vid {
	e.t.Helper()
	c, b := e.call("GET", "/videos/feed", nil, uid)
	if c != 200 {
		e.t.Fatalf("feed: %d %s", c, b)
	}
	var r struct {
		Videos []vid `json:"videos"`
	}
	decodeInto(e.t, b, &r)
	return r.Videos
}

func (e *env) ticket(uid, id string) (int, string) {
	e.t.Helper()
	c, b := e.call("GET", "/videos/"+id+"/ticket", nil, uid)
	var r struct {
		URL string `json:"url"`
	}
	if c == 200 {
		decodeInto(e.t, b, &r)
	}
	return c, r.URL
}

func (e *env) fetchFile(path string, hdr map[string]string) *httptest.ResponseRecorder {
	return e.fire("GET", path, nil, "", hdr)
}

func TestVideoShortFeedAndPlayback(t *testing.T) {
	e := setup(t)
	author, fan := e.user("Dana"), e.user("Fan")
	clip := fakeMP4(12, 4096)

	c, v := e.uploadVideo(author, "short", "Sunset run", clip)
	if c != 201 || v.Kind != "short" || v.Seconds != 12 || !v.Mine || v.Author == nil {
		t.Fatalf("upload: %d %+v", c, v)
	}
	feed := e.videoFeed(fan)
	if len(feed) == 0 || feed[0].ID != v.ID || feed[0].Author == nil || feed[0].Author.Name != "Dana" || feed[0].Mine {
		t.Fatalf("feed: %+v", feed)
	}

	// A signed ticket plays the bytes, with range support, with no auth header.
	c, path := e.ticket(fan, v.ID)
	if c != 200 || !strings.HasPrefix(path, "/videos/file/"+v.ID+"?exp=") {
		t.Fatalf("ticket: %d %q", c, path)
	}
	rec := e.fetchFile(path, nil)
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), clip) || rec.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("file: %d %q len=%d", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	if rec := e.fetchFile(path, map[string]string{"Range": "bytes=0-99"}); rec.Code != 206 || rec.Body.Len() != 100 {
		t.Errorf("range: %d len=%d", rec.Code, rec.Body.Len())
	}
	// Tampered, expired and unsigned requests get nothing.
	if rec := e.fetchFile(strings.Replace(path, "sig=", "sig=00", 1), nil); rec.Code != 404 {
		t.Errorf("tampered sig: %d", rec.Code)
	}
	past := time.Now().Add(-time.Minute).Unix()
	m := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(m, "video|%s|%d", v.ID, past)
	if rec := e.fetchFile(fmt.Sprintf("/videos/file/%s?exp=%d&sig=%s", v.ID, past, hex.EncodeToString(m.Sum(nil))), nil); rec.Code != 404 {
		t.Errorf("expired ticket: %d", rec.Code)
	}
	if rec := e.fetchFile("/videos/file/"+v.ID, nil); rec.Code != 404 {
		t.Errorf("unsigned: %d", rec.Code)
	}

	// Likes toggle and count.
	_, b := e.call("POST", "/videos/"+v.ID+"/like", nil, fan)
	var l struct {
		Liked bool `json:"liked"`
		Likes int  `json:"likes"`
	}
	decodeInto(t, b, &l)
	if !l.Liked || l.Likes != 1 {
		t.Errorf("like: %+v", l)
	}
	if f := e.videoFeed(fan); !f[0].Liked || f[0].Likes != 1 {
		t.Errorf("feed after like: %+v", f[0])
	}
	_, b = e.call("POST", "/videos/"+v.ID+"/like", nil, fan)
	decodeInto(t, b, &l)
	if l.Liked || l.Likes != 0 {
		t.Errorf("unlike: %+v", l)
	}

	// Only the owner deletes; the file stops working.
	if c, _ := e.call("DELETE", "/videos/"+v.ID, nil, fan); c != 404 {
		t.Errorf("other deleted: %d", c)
	}
	if c, _ := e.call("DELETE", "/videos/"+v.ID, nil, author); c != 204 {
		t.Errorf("owner delete: %d", c)
	}
	if rec := e.fetchFile(path, nil); rec.Code != 404 {
		t.Errorf("deleted file still served: %d", rec.Code)
	}
}

func TestVideoUploadValidation(t *testing.T) {
	e := setup(t)
	open := e.user("Opener")
	shy := e.user("Shy")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)
	unverified := e.newUser("user", false, "Unv")

	cases := []struct {
		name string
		who  string
		kind string
		data []byte
		want int
	}{
		{"too long", open, "short", fakeMP4(45, 10), 422},
		{"not a video", open, "short", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 600)...), 422},
		{"webm", open, "short", append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, 600)...), 422},
		{"no header", open, "short", append([]byte{0, 0, 0, 20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 2, 0, 'i', 's', 'o', 'm'}, make([]byte, 50)...), 422},
		{"too big", open, "short", fakeMP4(5, 13<<20), 422},
		{"empty", open, "short", nil, 422},
		{"bad kind", open, "story", fakeMP4(5, 10), 422},
		{"incognito short", shy, "short", fakeMP4(5, 10), 403},
		{"unverified", unverified, "short", fakeMP4(5, 10), 403},
	}
	for _, tc := range cases {
		if c, _ := e.uploadVideo(tc.who, tc.kind, "", tc.data); c != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, c, tc.want)
		}
	}
	// Incognito members may keep a reel (it follows photo visibility).
	if c, _ := e.uploadVideo(shy, "reel", "", fakeMP4(5, 10)); c != 201 {
		t.Errorf("incognito reel: %d", c)
	}
	if c, _ := e.uploadVideo(open, "short", strings.Repeat("x", 151), fakeMP4(5, 10)); c != 422 {
		t.Errorf("long caption: %d", c)
	}
	// Five shorts a day.
	for i := 0; i < 5; i++ {
		if c, _ := e.uploadVideo(open, "short", "", fakeMP4(3, i)); c != 201 {
			t.Fatalf("short %d: %d", i, c)
		}
	}
	if c, _ := e.uploadVideo(open, "short", "", fakeMP4(3, 9)); c != 422 {
		t.Errorf("sixth short: %d", c)
	}
}

func TestVideoReelFollowsPhotoVisibility(t *testing.T) {
	e := setup(t)
	open, shy, viewer, blocked := e.user("Ona"), e.user("Shy"), e.user("Viewer"), e.user("Blocked")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, shy)

	_, openReel := e.uploadVideo(open, "reel", "", fakeMP4(8, 100))
	_, shyReel := e.uploadVideo(shy, "reel", "", fakeMP4(8, 100))

	reel := func(viewerID, owner string) int {
		c, _ := e.call("GET", "/videos/reel/"+e.refOf(owner), nil, viewerID)
		return c
	}
	if c := reel(viewer, open); c != 200 {
		t.Errorf("open reel: %d", c)
	}
	// Anonymous profiles: not even confirmable.
	if c := reel(viewer, shy); c != 404 {
		t.Errorf("incognito reel: %d", c)
	}
	if c, _ := e.ticket(viewer, shyReel.ID); c != 404 {
		t.Errorf("incognito reel ticket: %d", c)
	}
	if c := reel(shy, shy); c != 200 {
		t.Errorf("own reel: %d", c)
	}
	// The profile advertises the reel's length only where the reel is visible.
	_, body := e.call("GET", "/users/"+e.refOf(open), nil, viewer)
	if !strings.Contains(string(body), `"reel_s":8`) {
		t.Errorf("open profile does not advertise its reel: %s", body)
	}
	_, body = e.call("GET", "/users/"+e.refOf(shy), nil, viewer)
	if strings.Contains(string(body), "reel_s") {
		t.Errorf("anonymous profile leaks a reel: %s", body)
	}
	// Reels are not in the public feed.
	for _, v := range e.videoFeed(viewer) {
		if v.Kind == "reel" {
			t.Errorf("reel leaked into the feed: %+v", v)
		}
	}
	// Blocks hide everything.
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(blocked)}, open)
	if c := reel(blocked, open); c != 404 {
		t.Errorf("blocked reel: %d", c)
	}
	if c, _ := e.ticket(blocked, openReel.ID); c != 404 {
		t.Errorf("blocked ticket: %d", c)
	}
	// A new reel replaces the old one and the old file stops working.
	_, oldPath := e.ticket(open, openReel.ID)
	if c, v := e.uploadVideo(open, "reel", "", fakeMP4(6, 200)); c != 201 || v.ID == openReel.ID {
		t.Fatalf("replace reel: %d %+v", c, v)
	}
	if rec := e.fetchFile(oldPath, nil); rec.Code != 404 {
		t.Errorf("replaced reel still served: %d", rec.Code)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM videos WHERE owner_id=$1 AND kind='reel'`, open).Scan(&n); err != nil || n != 1 {
		t.Errorf("reels after replace: %d %v", n, err)
	}
}

func TestVideoReportsAndStaffRemoval(t *testing.T) {
	e := setup(t)
	author, viewer := e.user("Author"), e.user("Viewer")
	_, v := e.uploadVideo(author, "short", "caption here", fakeMP4(6, 100))

	if c, _ := e.call("POST", "/videos/"+v.ID+"/report", map[string]string{"reason": "nonsense"}, viewer); c != 422 {
		t.Errorf("bad reason: %d", c)
	}
	if c, _ := e.call("POST", "/videos/"+v.ID+"/report", map[string]string{"reason": "spam"}, author); c != 404 {
		t.Errorf("self report: %d", c)
	}
	if c, _ := e.call("POST", "/videos/"+v.ID+"/report", map[string]string{"reason": "spam"}, viewer); c != 204 {
		t.Fatalf("report: %d", c)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM reports WHERE reported_id=$1 AND details='video short'`, author).Scan(&n); err != nil || n != 1 {
		t.Errorf("report row: %d %v", n, err)
	}

	mod := e.newUser("moderator", true, "Mod")
	if c, _ := e.callAs("POST", "/moderation/videos/"+v.ID+"/remove", map[string]string{"reason": "x"}, mod, "moderator"); c != 403 && c != 422 {
		t.Errorf("short reason: %d", c)
	}
	// Banning permission is needed; a moderator lacks it, an admin has it.
	admin := e.newUser("admin", true, "Admin")
	if c, b := e.callAs("POST", "/moderation/videos/"+v.ID+"/remove", map[string]string{"reason": "violates the rules"}, admin, "admin"); c != 204 {
		t.Fatalf("staff remove: %d %s", c, b)
	}
	if c, _ := e.ticket(viewer, v.ID); c != 404 {
		t.Errorf("removed video ticket: %d", c)
	}
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM staff_audit_log WHERE action='remove_video' AND target_user_id=$1`, author).Scan(&n); err != nil || n != 1 {
		t.Errorf("audit row: %d %v", n, err)
	}
}

func TestShortVideosCanCarryABuiltInSound(t *testing.T) {
	e := setup(t)
	owner, viewer := e.user("Sound Owner"), e.user("Sound Viewer")
	post := func(uid, kind, sound string) (int, vid) {
		c, b := e.raw("POST", "/videos/?kind="+kind+"&caption=hi&sound="+sound, fakeMP4(4, 400), uid)
		var r struct {
			Video vid `json:"video"`
		}
		if c == 201 {
			decodeInto(e.t, b, &r)
		}
		return c, r.Video
	}
	if c, _ := post(owner, "short", "not-a-sound"); c != 422 {
		t.Errorf("unknown sound: %d", c)
	}
	if c, _ := post(owner, "reel", "sunrise"); c != 422 {
		t.Errorf("a reel cannot carry a sound: %d", c)
	}
	c, v := post(owner, "short", "sunrise")
	if c != 201 {
		t.Fatalf("upload: %d", c)
	}
	_, feed := e.call("GET", "/videos/feed", nil, viewer)
	if !strings.Contains(string(feed), `"sound":"sunrise"`) || !strings.Contains(string(feed), `"sound_name":"Sunrise"`) || !strings.Contains(string(feed), v.ID) {
		t.Errorf("feed lacks the sound: %s", feed)
	}
	// The library: a list and a real WAV file, members only.
	if c, _ := e.call("GET", "/sounds/", nil, ""); c != 401 {
		t.Errorf("signed out: %d", c)
	}
	_, list := e.call("GET", "/sounds/", nil, viewer)
	if !strings.Contains(string(list), `"id":"sunrise"`) || !strings.Contains(string(list), `"seconds"`) {
		t.Errorf("list: %s", list)
	}
	c2, wav := e.call("GET", "/sounds/sunrise", nil, viewer)
	if c2 != 200 || len(wav) < 10000 || string(wav[:4]) != "RIFF" {
		t.Errorf("wav: %d %d bytes", c2, len(wav))
	}
	if c, _ := e.call("GET", "/sounds/none", nil, viewer); c != 404 {
		t.Errorf("unknown file: %d", c)
	}
}
