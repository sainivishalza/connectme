package users

import (
	"connectme/internal/csam"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"strings"
	"testing"
)

func TestDualModeVisibilityEndToEnd(t *testing.T) {
	e := setup(t, allowAll{})
	alice := e.user(true, "Alicia Zanzibar", "my-secret-bio", "hiking")
	bob := e.user(true, "Bobby", "bio")
	lat, lng := randomBase()
	e.locate(alice, lat, lng)
	e.locate(bob, lat+0.01, lng+0.005)

	code, body := e.upload(alice, jpegBytes(300, 200))
	if code != 201 {
		t.Fatalf("upload: %d %s", code, body)
	}
	var meta PhotoMeta
	_ = json.Unmarshal(body, &meta)

	_, me, _ := e.do("GET", "/users/me", nil, alice)
	oldRef := refOf(t, me)

	// 1. Open: a stranger sees the full profile and can fetch the photo.
	code, body, _ = e.do("GET", "/users/"+oldRef, nil, bob)
	if code != 200 || !strings.Contains(string(body), "Alicia Zanzibar") || !strings.Contains(string(body), meta.ID) {
		t.Fatalf("open profile: %d %s", code, body)
	}
	if c, _, h := e.do("GET", "/media/photos/"+meta.ID, nil, bob); c != 200 || h.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("open photo: %d", c)
	}

	// 2. Going incognito rotates the ref; the old one no longer resolves.
	code, body, _ = e.do("POST", "/users/me/mode", SetModeRequest{Mode: ModeIncognito}, alice)
	if code != 200 {
		t.Fatalf("set mode: %d %s", code, body)
	}
	newRef := refOf(t, body)
	if newRef == oldRef {
		t.Fatal("ref must rotate on mode change")
	}
	if c, _, _ := e.do("GET", "/users/"+oldRef, nil, bob); c != 404 {
		t.Fatalf("old ref should be 404, got %d", c)
	}

	// 3. Nearby shows only an anonymous card: no name, bio, age, photo, coordinates.
	code, body, _ = e.do("GET", "/users/nearby?radius_km=25", nil, bob)
	if code != 200 {
		t.Fatalf("nearby: %d %s", code, body)
	}
	s := string(body)
	for _, leak := range []string{"Alicia", "Zanzibar", "my-secret-bio", meta.ID, "/media/", `"age"`,
		fmt.Sprintf("%.2f", lat), fmt.Sprintf("%.2f", lng), `"lat"`, `"lng"`, "location"} {
		if strings.Contains(s, leak) {
			t.Fatalf("nearby leaks %q: %s", leak, s)
		}
	}
	if !strings.Contains(s, `"anonymous":true`) || !strings.Contains(s, `"distance":"within`) || !strings.Contains(s, newRef) {
		t.Fatalf("nearby card malformed: %s", s)
	}
	if c, _, _ := e.do("GET", "/media/photos/"+meta.ID, nil, bob); c != 404 {
		t.Fatalf("incognito photo must be 404, got %d", c)
	}

	// 4. Matched in incognito; Alice then switches to open — Bob still sees nothing.
	u1, u2 := alice, bob
	if u2 < u1 {
		u1, u2 = u2, u1
	}
	var matchID string
	if err := e.db.QueryRow(context.Background(),
		`INSERT INTO matches (user1_id, user2_id, status, mode) VALUES ($1,$2,'matched','incognito') RETURNING id::text`,
		u1, u2).Scan(&matchID); err != nil {
		t.Fatal(err)
	}
	_, body, _ = e.do("POST", "/users/me/mode", SetModeRequest{Mode: ModeOpen}, alice)
	openRef := refOf(t, body)
	code, body, _ = e.do("GET", "/users/"+openRef, nil, bob)
	if code != 200 || strings.Contains(string(body), "Alicia") {
		t.Fatalf("mid-chat switch must not unmask: %d %s", code, body)
	}
	if c, _, _ := e.do("GET", "/media/photos/"+meta.ID, nil, bob); c != 404 {
		t.Fatalf("photo must stay hidden mid-chat, got %d", c)
	}

	// 5. One-sided reveal is not enough; mutual reveal unlocks.
	e.db.Exec(context.Background(), `INSERT INTO reveal_consents (match_id, user_id) VALUES ($1,$2)`, matchID, bob)
	if _, body, _ = e.do("GET", "/users/"+openRef, nil, bob); strings.Contains(string(body), "Alicia") {
		t.Fatal("one-sided reveal must not unmask")
	}
	e.db.Exec(context.Background(), `INSERT INTO reveal_consents (match_id, user_id) VALUES ($1,$2)`, matchID, alice)
	if _, body, _ = e.do("GET", "/users/"+openRef, nil, bob); !strings.Contains(string(body), "Alicia Zanzibar") {
		t.Fatalf("mutual reveal must unmask: %s", body)
	}
	if c, _, _ := e.do("GET", "/media/photos/"+meta.ID, nil, bob); c != 200 {
		t.Fatalf("photo after mutual reveal: %d", c)
	}

	// 6. Blocking hides the profile and the photo in both directions.
	e.db.Exec(context.Background(), `INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1,$2)`, bob, alice)
	if c, _, _ := e.do("GET", "/users/"+openRef, nil, bob); c != 404 {
		t.Fatalf("blocked profile: %d", c)
	}
	if c, _, _ := e.do("GET", "/media/photos/"+meta.ID, nil, alice); c != 200 {
		t.Fatalf("owner must always see own photo: %d", c)
	}
}

func TestPrivacyOfOwnProfileAndLocation(t *testing.T) {
	e := setup(t, allowAll{})
	u := e.user(true, "Cara", "bio")
	if c, _, _ := e.do("PUT", "/users/me/location", LocationRequest{Lat: 91, Lng: 0}, u); c != 422 {
		t.Fatalf("bad lat: %d", c)
	}
	lat, lng := randomBase()
	if c, _, _ := e.do("PUT", "/users/me/location", LocationRequest{Lat: lat, Lng: lng}, u); c != 204 {
		t.Fatalf("set location: %d", c)
	}
	_, body, _ := e.do("GET", "/users/me", nil, u)
	s := string(body)
	if !strings.Contains(s, `"has_location":true`) || strings.Contains(s, fmt.Sprintf("%.3f", lat)) ||
		strings.Contains(s, fmt.Sprintf("%.3f", lng)) {
		t.Fatalf("own profile must not expose coordinates: %s", s)
	}
}

func TestNearbyRules(t *testing.T) {
	e := setup(t, allowAll{})
	viewer := e.user(true, "Viewer", "b")
	near := e.user(true, "Near", "b", "chess")
	far := e.user(true, "Far", "b")
	hotel := e.user(true, "InHotel", "b")
	swiped := e.user(true, "Swiped", "b")
	unverified := e.user(false, "Nope", "b")
	lat, lng := randomBase()
	e.locate(viewer, lat, lng)
	e.locate(near, lat+0.004, lng+0.008)
	e.locate(far, lat+3, lng) // ~333 km away
	e.locate(hotel, lat+0.001, lng+0.001)
	e.locate(swiped, lat+0.002, lng+0.002)
	e.locate(unverified, lat+0.003, lng+0.003)
	e.db.Exec(context.Background(), `UPDATE profiles SET mode='hotel' WHERE user_id=$1`, hotel)
	e.db.Exec(context.Background(), `INSERT INTO swipes (actor_id, target_id, action) VALUES ($1,$2,'pass')`, viewer, swiped)

	_, body, _ := e.do("GET", "/users/nearby?radius_km=25", nil, viewer)
	var res struct {
		Results []PublicProfile `json:"results"`
	}
	_ = json.Unmarshal(body, &res)
	if len(res.Results) != 1 || res.Results[0].DisplayName != "Near" {
		t.Fatalf("want only 'Near' (open mode), got %s", body)
	}

	if _, body, _ = e.do("GET", "/users/nearby?radius_km=25&tags=chess", nil, viewer); !strings.Contains(string(body), "Near") {
		t.Fatalf("tag filter: %s", body)
	}
	if _, body, _ = e.do("GET", "/users/nearby?radius_km=25&tags=golf", nil, viewer); strings.Contains(string(body), "Near") {
		t.Fatalf("tag filter should exclude: %s", body)
	}
	if c, _, _ := e.do("GET", "/users/nearby?radius_km=3", nil, viewer); c != 422 {
		t.Fatalf("arbitrary radius must be rejected: %d", c)
	}
	if c, _, _ := e.do("GET", "/users/nearby?radius_km=25", nil, unverified); c != 403 {
		t.Fatalf("unverified viewer: %d", c)
	}
	nolocation := e.user(true, "NoLoc", "b")
	if c, _, _ := e.do("GET", "/users/nearby?radius_km=25", nil, nolocation); c != 403 {
		t.Fatalf("no location: %d", c)
	}
}

func TestProfileValidationAndModeRules(t *testing.T) {
	e := setup(t, allowAll{})
	u := e.user(true, "Dan", "b")
	bad := map[string]string{
		"newline intro": `{"intro":"a\nb"}`,
		"bad gender":    `{"gender":"x"}`,
		"11 tags":       `{"tags":["a","b","c","d","e","f","g","h","i","j","k"]}`,
		"bad tag":       `{"tags":["<script>"]}`,
		"unknown field": `{"role":"admin"}`,
	}
	for name, b := range bad {
		if c, _, _ := e.do("PATCH", "/users/me", []byte(b), u); c != 422 && c != 400 {
			t.Errorf("%s: got %d", name, c)
		}
	}
	code, body, _ := e.do("PATCH", "/users/me", []byte(`{"display_name":"<b>Dan</b><script>x()</script>","tags":["Hiking","hiking","Rock Climbing"]}`), u)
	if code != 200 || strings.Contains(string(body), "<") || !strings.Contains(string(body), `"hiking","rock climbing"`) {
		t.Fatalf("sanitise/normalise: %d %s", code, body)
	}
	if c, _, _ := e.do("POST", "/users/me/mode", SetModeRequest{Mode: ModeHotel}, u); c != 422 {
		t.Fatalf("clients must not set hotel mode: %d", c)
	}
	// In hotel mode, "incognito" is a no-op; "open" is an explicit opt-in.
	e.db.Exec(context.Background(), `UPDATE profiles SET mode='hotel' WHERE user_id=$1`, u)
	_, body, _ = e.do("POST", "/users/me/mode", SetModeRequest{Mode: ModeIncognito}, u)
	if !strings.Contains(string(body), `"mode":"hotel"`) {
		t.Fatalf("hotel must stay forced incognito: %s", body)
	}
	_, body, _ = e.do("POST", "/users/me/mode", SetModeRequest{Mode: ModeOpen}, u)
	if !strings.Contains(string(body), `"mode":"open"`) {
		t.Fatalf("opt-in to open: %s", body)
	}
}

func TestPhotoProcessing(t *testing.T) {
	e := setup(t, allowAll{})
	u := e.user(true, "Eve", "b")

	// EXIF stripped, large image downsized.
	code, body := e.upload(u, withEXIF(jpegBytes(2400, 1600), "SECRET-GPS-48.85N"))
	if code != 201 {
		t.Fatalf("upload: %d %s", code, body)
	}
	var meta PhotoMeta
	_ = json.Unmarshal(body, &meta)
	stored, err := e.svc.media.Get(context.Background(), meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("SECRET-GPS")) || bytes.Contains(stored, []byte("Exif")) {
		t.Fatal("EXIF metadata survived processing")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(stored))
	if err != nil || cfg.Width != 1080 || cfg.Height != 720 {
		t.Fatalf("resize: %+v %v", cfg, err)
	}

	// Non-images and disguised files are refused.
	if c, _ := e.upload(u, []byte("GIF89a not really")); c != 422 {
		t.Fatalf("fake image: %d", c)
	}
	if c, _ := e.upload(u, []byte("<?php system($_GET['c']); ?>")); c != 422 {
		t.Fatalf("script upload: %d", c)
	}

	// Max six photos.
	for i := 0; i < MaxPhotos-1; i++ {
		if c, b := e.upload(u, jpegBytes(64, 64)); c != 201 {
			t.Fatalf("photo %d: %d %s", i+2, c, b)
		}
	}
	if c, _ := e.upload(u, jpegBytes(64, 64)); c != 422 {
		t.Fatalf("7th photo: %d", c)
	}

	// Delete removes metadata and file; other users' photos are not deletable.
	other := e.user(true, "Mallory", "b")
	if c, _, _ := e.do("DELETE", "/users/me/photos/"+meta.ID, nil, other); c != 404 {
		t.Fatalf("delete foreign photo: %d", c)
	}
	if c, _, _ := e.do("DELETE", "/users/me/photos/"+meta.ID, nil, u); c != 204 {
		t.Fatalf("delete: %d", c)
	}
	if _, err := e.svc.media.Get(context.Background(), meta.ID); err == nil {
		t.Fatal("file still exists after delete")
	}
}

func TestCSAMMatchBlocksStorage(t *testing.T) {
	e := setup(t, rejectAll{})
	u := e.user(true, "Frank", "b")
	if c, b := e.upload(u, jpegBytes(64, 64)); c != 422 || !strings.Contains(string(b), "upload rejected") {
		t.Fatalf("scan match: %d %s", c, b)
	}
	me, _ := e.svc.GetMe(context.Background(), u)
	if len(me.Photos) != 0 {
		t.Fatal("rejected media must not be recorded")
	}
	entries, _ := os.ReadDir(e.svc.media.(LocalStore).Dir)
	if len(entries) != 0 {
		t.Fatal("rejected media must not be written to disk")
	}
	// The hit is recorded without the image, the account is suspended and staff are alerted.
	var n int
	_ = e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM csam_hits WHERE user_id=$1::uuid AND source='profile_photo' AND provider_ref='test-hit' AND length(content_sha256)=64`, u).Scan(&n)
	if n != 1 {
		t.Fatalf("csam_hits rows: %d, want 1", n)
	}
	var banned bool
	_ = e.db.QueryRow(context.Background(), `SELECT is_banned FROM users WHERE id=$1::uuid`, u).Scan(&banned)
	if !banned {
		t.Fatal("the account must be suspended after a match")
	}
	_ = e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM reports WHERE reported_id=$1::uuid AND reason='csam_hit' AND status='open'`, u).Scan(&n)
	if n != 1 {
		t.Fatalf("staff alert (open csam_hit report): %d, want 1", n)
	}
}

type brokenScanner struct{}

func (brokenScanner) Scan(context.Context, []byte, csam.ScanMeta) (csam.Verdict, error) {
	return csam.Verdict{}, csam.ErrUnavailable
}

// An unscanned image is never stored: a scanner that cannot answer refuses the upload.
func TestScannerOutageRefusesTheUploadAndStoresNothing(t *testing.T) {
	e := setup(t, brokenScanner{})
	u := e.user(true, "Grace", "b")
	c, b := e.upload(u, jpegBytes(64, 64))
	if c != 503 || !strings.Contains(string(b), "moderation_unavailable") {
		t.Fatalf("scanner outage: %d %s", c, b)
	}
	me, _ := e.svc.GetMe(context.Background(), u)
	if len(me.Photos) != 0 {
		t.Fatal("an unscanned photo was recorded")
	}
	entries, _ := os.ReadDir(e.svc.media.(LocalStore).Dir)
	if len(entries) != 0 {
		t.Fatal("an unscanned photo was written to disk")
	}
	var n int
	_ = e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM csam_hits WHERE user_id=$1::uuid`, u).Scan(&n)
	if n != 0 {
		t.Fatal("an outage is not a hit")
	}
}
