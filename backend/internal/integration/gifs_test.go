package integration

import (
	"strings"
	"testing"
)

func TestGIFRoutesNeedLoginSearchAndServeRealGIFs(t *testing.T) {
	e := setup(t)
	u := e.user("Gif Sender")

	if rec := e.fire("GET", "/gifs/search?q=love", nil, "", nil); rec.Code != 401 {
		t.Errorf("anonymous search: %d", rec.Code)
	}
	c, b := e.call("GET", "/gifs/search?q=love", nil, u)
	if c != 200 || !strings.Contains(string(b), `"id":"heart"`) || !strings.Contains(string(b), `"provider":"builtin"`) {
		t.Fatalf("search: %d %s", c, b)
	}
	if c, b := e.call("GET", "/gifs/search", nil, u); c != 200 || strings.Count(string(b), `"id"`) < 12 {
		t.Errorf("popular list: %d %s", c, b)
	}
	if c, b := e.call("GET", "/gifs/search?q=zzzzqq", nil, u); c != 200 || !strings.Contains(string(b), `"items":[]`) {
		t.Errorf("no results should be an empty list: %d %s", c, b)
	}
	if c, _ := e.call("GET", "/gifs/search?limit=2", nil, u); c != 200 {
		t.Errorf("limit: %d", c)
	}

	rec := e.fire("GET", "/gifs/heart", nil, e.tokenFor(u, "user"), nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/gif" || !strings.HasPrefix(rec.Body.String(), "GIF89a") {
		t.Errorf("media: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(rec.Header().Get("Cache-Control"), "private") {
		t.Errorf("headers: %v", rec.Header())
	}
	for _, bad := range []string{"/gifs/nope", "/gifs/..%2Fsecrets"} {
		if rec := e.fire("GET", bad, nil, e.tokenFor(u, "user"), nil); rec.Code != 404 {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
}
