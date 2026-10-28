package gifs

import (
	"bytes"
	"context"
	"image/gif"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEveryBuiltinStickerIsASmallValidAnimatedGIF(t *testing.T) {
	b := NewBuiltin()
	all, _ := b.Search(context.Background(), "", 100)
	if len(all) < 12 {
		t.Fatalf("only %d stickers", len(all))
	}
	seen := map[string]bool{}
	for _, it := range all {
		if seen[it.ID] || it.Title == "" {
			t.Errorf("bad item %+v", it)
		}
		seen[it.ID] = true
		data, err := b.Media(context.Background(), it.ID)
		if err != nil {
			t.Fatalf("%s: %v", it.ID, err)
		}
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil || len(g.Image) != frames {
			t.Errorf("%s: %v frames=%d", it.ID, err, len(g.Image))
		}
		if len(data) > 120<<10 {
			t.Errorf("%s is %d bytes", it.ID, len(data))
		}
		// An animation should actually change between frames.
		moves := false
		for _, fr := range g.Image[1:] {
			moves = moves || !bytes.Equal(g.Image[0].Pix, fr.Pix)
		}
		if !moves {
			t.Errorf("%s does not animate", it.ID)
		}
	}
}

func TestBuiltinSearchMatchesWordsAndRejectsUnknownIDs(t *testing.T) {
	b := NewBuiltin()
	ctx := context.Background()
	got := func(q string) string {
		items, _ := b.Search(ctx, q, 30)
		var ids []string
		for _, i := range items {
			ids = append(ids, i.ID)
		}
		return strings.Join(ids, ",")
	}
	if g := got("lol"); g != "laugh" {
		t.Errorf("lol: %s", g)
	}
	if g := got("  LOVE  "); g != "heart" {
		t.Errorf("love: %s", g)
	}
	if g := got("party"); !strings.Contains(g, "party") || !strings.Contains(g, "dance") {
		t.Errorf("party: %s", g)
	}
	if g := got("zzzz"); g != "" {
		t.Errorf("nonsense: %s", g)
	}
	if items, _ := b.Search(ctx, "", 3); len(items) != 3 {
		t.Errorf("limit not applied: %d", len(items))
	}
	for _, bad := range []string{"../etc/passwd", "nope", ""} {
		if _, err := b.Media(ctx, bad); err != ErrNotFound {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if CleanQuery("  a   b  "+strings.Repeat("x", 80)) == "" || len([]rune(CleanQuery(strings.Repeat("y", 90)))) != 40 {
		t.Error("query cleaning")
	}
}

// fakeTenor stands in for Tenor's API and its media host.
func fakeTenor(t *testing.T, mediaURL func(base string) string, gifBody []byte) (*Tenor, *httptest.Server, *[]string) {
	var queries []string
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/v2/search", func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"results":[{"id":"111","content_description":"cat dance","media_formats":{"tinygif":{"url":"` + mediaURL(srv.URL) + `"}}},{"id":"","media_formats":{}}]}`))
	})
	mux.HandleFunc("/v2/featured", func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"results":[]}`))
	})
	mux.HandleFunc("/v2/posts", func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"results":[{"id":"111","media_formats":{"tinygif":{"url":"` + mediaURL(srv.URL) + `"}}}]}`))
	})
	mux.HandleFunc("/media.gif", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(gifBody) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	tn := NewTenor("secret-key")
	tn.APIBase = srv.URL
	tn.AllowInsecure = true
	tn.AllowHost = func(h string) bool { return h == "127.0.0.1" }
	return tn, srv, &queries
}

func TestTenorSendsOnlyTheQueryAndAFixedClientAndOnlyFetchesAllowedMediaHosts(t *testing.T) {
	g, _ := NewBuiltin().Media(context.Background(), "heart")
	tn, _, queries := fakeTenor(t, func(base string) string { return base + "/media.gif" }, g)
	ctx := context.Background()
	items, err := tn.Search(ctx, "  cat   dance ", 5)
	if err != nil || len(items) != 1 || items[0].ID != "111" || items[0].Title != "cat dance" {
		t.Fatalf("search: %v %+v", err, items)
	}
	q := (*queries)[0]
	for _, want := range []string{"q=cat+dance", "contentfilter=high", "client_key=connectme", "key=secret-key", "limit=5"} {
		if !strings.Contains(q, want) {
			t.Errorf("request lacks %s: %s", want, q)
		}
	}
	data, err := tn.Media(ctx, "111")
	if err != nil || string(data[:3]) != "GIF" {
		t.Fatalf("media: %v", err)
	}
	if items, _ := tn.Search(ctx, "", 5); len(items) != 0 || !strings.Contains((*queries)[len(*queries)-1], "limit=5") {
		t.Errorf("trending: %v", items)
	}
	for _, bad := range []string{"1/../2", "a b", "x?y=1", strings.Repeat("9", 60)} {
		if _, err := tn.Media(ctx, bad); err != ErrNotFound {
			t.Errorf("id %q: %v", bad, err)
		}
	}

	// A media address outside the allowed hosts is never fetched (no server-side request forgery).
	evil, _, _ := fakeTenor(t, func(string) string { return "http://169.254.169.254/latest/meta-data" }, g)
	if _, err := evil.Media(ctx, "111"); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("metadata address was not refused: %v", err)
	}
	// Plain http is refused outside tests; something that is not a GIF, or is too big, is refused.
	strict, _, _ := fakeTenor(t, func(base string) string { return base + "/media.gif" }, g)
	strict.AllowInsecure = false
	if _, err := strict.Media(ctx, "111"); err == nil {
		t.Error("http media accepted")
	}
	notGIF, _, _ := fakeTenor(t, func(base string) string { return base + "/media.gif" }, []byte("<html>hi</html>"))
	if _, err := notGIF.Media(ctx, "111"); err == nil {
		t.Error("an HTML page was accepted as a GIF")
	}
	huge, _, _ := fakeTenor(t, func(base string) string { return base + "/media.gif" }, append([]byte("GIF89a"), make([]byte, maxGIFBytes+10)...))
	if _, err := huge.Media(ctx, "111"); err == nil {
		t.Error("an oversize file was accepted")
	}
}
