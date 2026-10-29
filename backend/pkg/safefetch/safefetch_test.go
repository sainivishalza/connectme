package safefetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeResolver map[string][]string

func (f fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	var out []net.IPAddr
	for _, s := range f[host] {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	if out == nil {
		return nil, errors.New("no such host")
	}
	return out, nil
}

func TestBlockedRanges(t *testing.T) {
	for ip, want := range map[string]bool{
		"127.0.0.1": true, "127.5.5.5": true, "10.0.0.1": true, "172.16.0.1": true, "172.31.255.255": true, "192.168.1.1": true,
		"169.254.169.254": true, "169.254.0.1": true, "100.64.0.1": true, "0.0.0.0": true, "224.0.0.1": true, "255.255.255.255": true,
		"::1": true, "::": true, "fc00::1": true, "fd12:3456::1": true, "fe80::1": true, "::ffff:127.0.0.1": true, "::ffff:10.1.2.3": true,
		"8.8.8.8": false, "1.1.1.1": false, "93.184.216.34": false, "172.32.0.1": false, "2606:4700:4700::1111": false,
	} {
		if got := IsBlockedIP(net.ParseIP(ip)); got != want {
			t.Errorf("IsBlockedIP(%s) = %v, want %v", ip, got, want)
		}
	}
	if !IsBlockedIP(nil) {
		t.Error("a nil address must be blocked")
	}
}

func TestLoopbackAndMetadataAreRefusedByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("secret")) }))
	defer srv.Close()
	c := New(Options{AllowHTTP: true})
	for _, u := range []string{srv.URL, "http://169.254.169.254/latest/meta-data/", "http://[::1]/", "http://10.0.0.5/"} {
		if _, err := c.Get(context.Background(), u); err == nil || !strings.Contains(err.Error(), "safefetch") {
			t.Errorf("%s was not refused by safefetch: %v", u, err)
		}
	}
}

func TestANameThatResolvesToAPrivateAddressIsRefused(t *testing.T) {
	r := fakeResolver{
		"evil.example":  {"10.1.2.3"},
		"mixed.example": {"8.8.8.8", "127.0.0.1"}, // one good answer must not hide a bad one
		"v6.example":    {"fd00::1"},
	}
	c := New(Options{AllowHTTP: true, Resolver: r})
	for _, host := range []string{"evil.example", "mixed.example", "v6.example"} {
		if _, err := c.Get(context.Background(), "http://"+host+"/x"); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: want ErrBlocked, got %v", host, err)
		}
	}
}

func TestSchemeAndHostRules(t *testing.T) {
	c := New(Options{
		AllowHost: func(h string) bool { return h == "good.example" },
		Resolver:  fakeResolver{"good.example": {"8.8.8.8"}, "bad.example": {"8.8.4.4"}},
	})
	for _, u := range []string{"http://good.example/", "ftp://good.example/", "file:///etc/passwd", "https://bad.example/", "https:///nohost"} {
		if _, err := c.Get(context.Background(), u); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: want ErrBlocked, got %v", u, err)
		}
	}
}

func TestEveryRedirectHopIsChecked(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("fine")) }))
	defer final.Close()
	one := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, final.URL+"/x", http.StatusFound) }))
	defer one.Close()

	// A redirect to an allowed host works.
	ok := New(Options{AllowHTTP: true, AllowPrivateNetworks: true, AllowHost: func(h string) bool { return h == "127.0.0.1" }})
	if body, err := ok.Get(context.Background(), one.URL); err != nil || string(body) != "fine" {
		t.Fatalf("an allowed redirect failed: %q %v", body, err)
	}

	// A redirect to a host that is not on the list is refused, even though the first hop was fine.
	toOther := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:1/x", http.StatusFound)
	}))
	defer toOther.Close()
	if _, err := ok.Get(context.Background(), toOther.URL); !errors.Is(err, ErrBlocked) {
		t.Errorf("redirect to a host outside the allowlist: want ErrBlocked, got %v", err)
	}

	// A redirect to the metadata address is refused.
	toMeta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer toMeta.Close()
	if _, err := ok.Get(context.Background(), toMeta.URL); !errors.Is(err, ErrBlocked) {
		t.Errorf("redirect to the metadata address: want ErrBlocked, got %v", err)
	}

	// A downgrade to plain http is refused when only https is allowed.
	httpsOnly := New(Options{AllowPrivateNetworks: true})
	if _, err := httpsOnly.Get(context.Background(), one.URL); !errors.Is(err, ErrBlocked) {
		t.Errorf("plain http while only https is allowed: want ErrBlocked, got %v", err)
	}

	// A redirect loop is cut off.
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, r.URL.Path+"x", http.StatusFound) }))
	defer loop.Close()
	if _, err := New(Options{AllowHTTP: true, AllowPrivateNetworks: true, MaxRedirects: 3}).Get(context.Background(), loop.URL); !errors.Is(err, ErrBlocked) {
		t.Errorf("redirect loop: want ErrBlocked, got %v", err)
	}
}

func TestSizeAndTimeLimits(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(make([]byte, 5000)) }))
	defer big.Close()
	if _, err := New(Options{AllowHTTP: true, AllowPrivateNetworks: true, MaxBytes: 1000}).Get(context.Background(), big.URL); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversize body: want ErrTooLarge, got %v", err)
	}
	if body, err := New(Options{AllowHTTP: true, AllowPrivateNetworks: true, MaxBytes: 10000}).Get(context.Background(), big.URL); err != nil || len(body) != 5000 {
		t.Errorf("a body under the cap: %d %v", len(body), err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(600 * time.Millisecond) }))
	defer slow.Close()
	start := time.Now()
	if _, err := New(Options{AllowHTTP: true, AllowPrivateNetworks: true, Timeout: 150 * time.Millisecond}).Get(context.Background(), slow.URL); err == nil {
		t.Error("a slow server was waited for")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("the timeout was not enforced: %v", time.Since(start))
	}
}
