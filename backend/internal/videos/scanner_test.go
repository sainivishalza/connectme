package videos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPScannerSendsTheClipAndReadsTheVerdict(t *testing.T) {
	var gotAuth, gotType string
	var gotLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotType = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		b := make([]byte, 64)
		n, _ := r.Body.Read(b)
		gotLen = n
		_, _ = w.Write([]byte(`{"action":"flag","reason":"` + strings.Repeat("x", 500) + `"}`))
	}))
	defer srv.Close()
	v, err := HTTPScanner{URL: srv.URL, Key: "k"}.Scan(context.Background(), []byte("clip-bytes"), "video/mp4")
	if err != nil || v.Action != "flag" || len(v.Reason) != 200 {
		t.Fatalf("verdict: %+v %v", v, err)
	}
	if gotAuth != "Bearer k" || gotType != "video/mp4" || gotLen == 0 {
		t.Errorf("request: auth=%q type=%q len=%d", gotAuth, gotType, gotLen)
	}
}

func TestHTTPScannerFailsClosedOnBadAnswers(t *testing.T) {
	cases := map[string]func(http.ResponseWriter, *http.Request){
		"server error":   func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 500) },
		"not json":       func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) },
		"unknown action": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"action":"maybe"}`)) },
		"empty action":   func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) },
	}
	for name, h := range cases {
		srv := httptest.NewServer(http.HandlerFunc(h))
		if _, err := (HTTPScanner{URL: srv.URL}).Scan(context.Background(), []byte("x"), "video/mp4"); err == nil {
			t.Errorf("%s: no error", name)
		}
		srv.Close()
	}
	if _, err := (HTTPScanner{URL: "http://127.0.0.1:1"}).Scan(context.Background(), []byte("x"), "video/mp4"); err == nil {
		t.Error("unreachable scanner: no error")
	}
}

func TestStubAllowsAndSaysSo(t *testing.T) {
	v, err := StubScanner{}.Scan(context.Background(), nil, "")
	if err != nil || v.Action != "allow" || (StubScanner{}).Name() != "stub" {
		t.Errorf("stub: %+v %v", v, err)
	}
}
