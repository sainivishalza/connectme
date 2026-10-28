package groups

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPSuggesterKeepsTheLLMSourceAndPlanButNothingElse(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Internal-Key")
		_, _ = w.Write([]byte(`{"venues":["a","b"],"icebreakers":["q"],"plan":["1","2","3","4","5","6","7"],"source":"llm"}`))
	}))
	defer srv.Close()
	out, err := NewHTTPSuggester(srv.URL, "k").Suggest(context.Background(), SuggestInput{Activity: "dinner", GroupSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "k" || out.Source != "llm" || len(out.Plan) != 5 || len(out.Venues) != 2 {
		t.Errorf("got %+v key=%q", out, gotKey)
	}
}

func TestHTTPSuggesterDoesNotLetTheServiceInventASource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"venues":["a"],"icebreakers":["q"],"source":"trusted-by-admins"}`))
	}))
	defer srv.Close()
	out, _ := NewHTTPSuggester(srv.URL, "").Suggest(context.Background(), SuggestInput{})
	if out.Source != "ai" {
		t.Errorf("source %q", out.Source)
	}
}

func TestHTTPSuggesterFallsBackWhenTheServiceFails(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"500":   func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) },
		"junk":  func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) },
		"empty": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) },
	} {
		srv := httptest.NewServer(h)
		out, err := NewHTTPSuggester(srv.URL, "").Suggest(context.Background(), SuggestInput{Activity: "dinner", GroupSize: 2})
		srv.Close()
		if err != nil || out.Source != "fallback" || len(out.Plan) != 0 || len(out.Venues) == 0 {
			t.Errorf("%s: %+v %v", name, out, err)
		}
	}
}
