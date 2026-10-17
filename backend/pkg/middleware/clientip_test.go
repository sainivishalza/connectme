package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	nets, err := ParseCIDRs([]string{"10.0.0.0/8", "192.168.1.5"})
	if err != nil {
		t.Fatal(err)
	}
	defer SetTrustedProxies(nil)

	get := func(remote string, xff ...string) string {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		for _, x := range xff {
			r.Header.Add("X-Forwarded-For", x)
		}
		return ClientIP(r)
	}

	// Default: nothing is trusted, so the header is ignored and cannot be spoofed.
	SetTrustedProxies(nil)
	if got := get("203.0.113.9:5555", "1.2.3.4"); got != "203.0.113.9" {
		t.Fatalf("untrusted by default: %q", got)
	}

	SetTrustedProxies(nets)
	cases := []struct {
		name, remote string
		xff          []string
		want         string
	}{
		{"direct client, spoofed header ignored", "203.0.113.9:1", []string{"6.6.6.6"}, "203.0.113.9"},
		{"trusted proxy, real client", "10.1.2.3:1", []string{"198.51.100.7"}, "198.51.100.7"},
		{"client tries to prepend a fake hop", "10.1.2.3:1", []string{"6.6.6.6, 198.51.100.7"}, "198.51.100.7"},
		{"two trusted proxies", "10.1.2.3:1", []string{"198.51.100.7, 10.9.9.9"}, "198.51.100.7"},
		{"multiple header lines", "10.1.2.3:1", []string{"6.6.6.6", "198.51.100.7"}, "198.51.100.7"},
		{"single-IP trust entry", "192.168.1.5:1", []string{"198.51.100.8"}, "198.51.100.8"},
		{"garbage entries skipped", "10.1.2.3:1", []string{"198.51.100.7, not-an-ip, , 10.0.0.2"}, "198.51.100.7"},
		{"trusted proxy, no header", "10.1.2.3:1", nil, "10.1.2.3"},
		{"only proxies in chain", "10.1.2.3:1", []string{"10.0.0.2, 10.0.0.3"}, "10.1.2.3"},
		{"ipv6 client", "10.1.2.3:1", []string{"2001:db8::1"}, "2001:db8::1"},
	}
	for _, c := range cases {
		if got := get(c.remote, c.xff...); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	if _, err := ParseCIDRs([]string{"not-a-cidr"}); err == nil {
		t.Error("invalid CIDR must be rejected")
	}
}
