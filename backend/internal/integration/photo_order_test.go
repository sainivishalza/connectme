package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPhotosCanBeReorderedCaptionedAndMadeMainAndCaptionsFollowTheSameVisibilityRules(t *testing.T) {
	e := setup(t)
	owner, viewer := e.user("Photo Owner"), e.user("Photo Viewer")
	for i := 0; i < 3; i++ {
		if c := uploadPhoto(e, owner, jpegBytesFor(t)); c != 201 {
			t.Fatalf("upload %d: %d", i, c)
		}
	}
	ids := func() []string {
		_, b := e.call("GET", "/users/me", nil, owner)
		var me struct {
			Photos []struct {
				ID      string `json:"id"`
				Caption string `json:"caption"`
			} `json:"photos"`
		}
		_ = json.Unmarshal(b, &me)
		out := make([]string, len(me.Photos))
		for i, p := range me.Photos {
			out[i] = p.ID + "|" + p.Caption
		}
		return out
	}
	start := ids()
	id := func(i int) string { return strings.Split(start[i], "|")[0] }
	if len(start) != 3 {
		t.Fatalf("photos: %v", start)
	}

	// Reordering needs every photo exactly once.
	for name, body := range map[string][]string{"missing one": {id(0), id(1)}, "repeat": {id(0), id(0), id(1)}, "stranger id": {id(0), id(1), "11111111-1111-1111-1111-111111111111"}} {
		if c, _ := e.call("PUT", "/users/me/photos/order", map[string]any{"ids": body}, owner); c != 422 {
			t.Errorf("%s: %d, want 422", name, c)
		}
	}
	if c, b := e.call("PUT", "/users/me/photos/order", map[string]any{"ids": []string{id(2), id(0), id(1)}}, owner); c != 204 {
		t.Fatalf("reorder: %d %s", c, b)
	}
	if got := ids(); !strings.HasPrefix(got[0], id(2)) || !strings.HasPrefix(got[1], id(0)) || !strings.HasPrefix(got[2], id(1)) {
		t.Errorf("order after reorder: %v", got)
	}
	// Make main moves one to the front and keeps the rest in order.
	if c, _ := e.call("PUT", "/users/me/photos/"+id(1)+"/main", nil, owner); c != 204 {
		t.Fatalf("main: %d", c)
	}
	if got := ids(); !strings.HasPrefix(got[0], id(1)) || !strings.HasPrefix(got[1], id(2)) || !strings.HasPrefix(got[2], id(0)) {
		t.Errorf("order after main: %v", got)
	}

	// Captions: trimmed, limited, screened, owner-only.
	if c, _ := e.call("PUT", "/users/me/photos/"+id(1)+"/caption", map[string]string{"caption": "  Sunday   hike  "}, owner); c != 204 {
		t.Fatalf("caption: %d", c)
	}
	if got := ids(); got[0] != id(1)+"|Sunday hike" {
		t.Errorf("caption stored as %q", got[0])
	}
	if c, _ := e.call("PUT", "/users/me/photos/"+id(1)+"/caption", map[string]string{"caption": strings.Repeat("x", 101)}, owner); c != 422 {
		t.Errorf("long caption: %d", c)
	}
	if c, _ := e.call("PUT", "/users/me/photos/"+id(1)+"/caption", map[string]string{"caption": "x"}, viewer); c != 404 {
		t.Errorf("someone else's photo: %d, want 404", c)
	}
	if c, _ := e.call("PUT", "/users/me/photos/"+id(1)+"/main", nil, viewer); c != 404 {
		t.Errorf("someone else's photo as main: %d, want 404", c)
	}

	// The public profile shows captions only when it shows the photos.
	pub := func() string { _, b := e.call("GET", "/users/"+e.refOf(owner), nil, viewer); return string(b) }
	if p := pub(); !strings.Contains(p, "Sunday hike") {
		t.Errorf("open profile hides the caption: %s", p)
	}
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1::uuid`, owner)
	if p := pub(); strings.Contains(p, "Sunday hike") || strings.Contains(p, "/media/photos/") {
		t.Errorf("incognito profile leaks: %s", p)
	}
	// Clearing works.
	if c, _ := e.call("PUT", "/users/me/photos/"+id(1)+"/caption", map[string]string{"caption": ""}, owner); c != 204 {
		t.Errorf("clear: %d", c)
	}
	if got := ids(); got[0] != id(1)+"|" {
		t.Errorf("not cleared: %q", got[0])
	}
}
