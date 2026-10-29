// Package safefetch is the one way the server fetches a URL that anything other
// than our own configuration can influence (an address in a third-party reply,
// a host that comes from a user). It closes the usual server-side request
// forgery holes:
//
//   - the address is checked at dial time, after DNS resolution, and the
//     connection is made to the address that was checked, so a name that
//     resolves to a private address (or changes its answer between checks)
//     cannot reach one;
//   - loopback, private, link-local (including cloud metadata at 169.254.169.254),
//     shared, multicast and unspecified ranges, IPv4 and IPv6, are refused;
//   - only https unless the caller opts in, only hosts the caller allows, and
//     every redirect hop is checked again and the number of hops is limited;
//   - environment proxies are ignored, because a proxy would make the
//     connection on our behalf and skip the address check;
//   - time and size are capped.
//
// Services that are ours and fixed in configuration (the AI service, Stripe,
// push) do not need this and may sit on private networks.
package safefetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

var (
	ErrBlocked  = errors.New("safefetch: destination not allowed")
	ErrTooLarge = errors.New("safefetch: response too large")
)

// Resolver looks a host name up. net.DefaultResolver satisfies it.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Options configure a Client. The zero value is the strictest useful setting.
type Options struct {
	Timeout      time.Duration          // whole request including redirects; default 10s
	MaxBytes     int64                  // largest body; default 1 MiB
	MaxRedirects int                    // default 3
	AllowHTTP    bool                   // allow http as well as https
	AllowHost    func(host string) bool // nil allows any public host
	// AllowPrivateNetworks turns the address check off. For tests and for
	// trusted internal services only; production code that fetches anything a
	// user or a third party can influence must never set it.
	AllowPrivateNetworks bool
	Resolver             Resolver // default net.DefaultResolver
}

// Client fetches URLs under the rules in Options.
type Client struct {
	o    Options
	http *http.Client
}

var blockedNets = mustCIDRs(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
	"192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24",
	"203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "255.255.255.255/32",
	"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "2001:db8::/32", "64:ff9b::/96", "100::/64",
)

func mustCIDRs(list ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(list))
	for _, c := range list {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}

// IsBlockedIP reports whether ip is an address the server must never fetch from.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil { // also unwraps ::ffff:a.b.c.d
		ip = v4
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// New builds a Client.
func New(o Options) *Client {
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = 1 << 20
	}
	if o.MaxRedirects <= 0 {
		o.MaxRedirects = 3
	}
	if o.Resolver == nil {
		o.Resolver = net.DefaultResolver
	}
	c := &Client{o: o}
	tr := &http.Transport{
		Proxy:                 nil, // never through an environment proxy: it would skip the address check
		DialContext:           c.dial,
		TLSHandshakeTimeout:   o.Timeout,
		ResponseHeaderTimeout: o.Timeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
	}
	c.http = &http.Client{
		Transport: tr,
		Timeout:   o.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > o.MaxRedirects {
				return fmt.Errorf("%w: too many redirects", ErrBlocked)
			}
			return c.checkURL(req.URL)
		},
	}
	return c
}

// checkURL applies the scheme and host rules to one URL (the first request and each redirect).
func (c *Client) checkURL(u *url.URL) error {
	switch {
	case u.Scheme == "https", u.Scheme == "http" && c.o.AllowHTTP:
	default:
		return fmt.Errorf("%w: scheme %q", ErrBlocked, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: no host", ErrBlocked)
	}
	if c.o.AllowHost != nil && !c.o.AllowHost(host) {
		return fmt.Errorf("%w: host %q is not on the allowlist", ErrBlocked, host)
	}
	return nil
}

// dial resolves the host, refuses the connection unless EVERY address it
// resolves to is allowed, and then connects to one of those exact addresses.
func (c *Client) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	if lit := net.ParseIP(host); lit != nil {
		ips = []net.IP{lit}
	} else {
		as, err := c.o.Resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, a := range as {
			ips = append(ips, a.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%w: %q did not resolve", ErrBlocked, host)
	}
	if !c.o.AllowPrivateNetworks {
		for _, ip := range ips {
			if IsBlockedIP(ip) {
				return nil, fmt.Errorf("%w: %q resolves to a private or reserved address", ErrBlocked, host)
			}
		}
	}
	d := net.Dialer{Timeout: c.o.Timeout}
	var last error
	for _, ip := range ips {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

// HTTP returns an *http.Client with the same rules, for code that wants one.
// Response bodies are capped at MaxBytes.
func (c *Client) HTTP() *http.Client {
	hc := *c.http
	hc.Transport = capTransport{next: c.http.Transport, max: c.o.MaxBytes, check: c.checkURL}
	return &hc
}

// Do sends req under the same rules.
func (c *Client) Do(req *http.Request) (*http.Response, error) { return c.HTTP().Do(req) }

// Get fetches rawURL and returns its body, refusing anything larger than MaxBytes.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("safefetch: server answered %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// capTransport checks the first URL and limits the response body.
type capTransport struct {
	next  http.RoundTripper
	max   int64
	check func(*url.URL) error
}

func (t capTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.check(req.URL); err != nil {
		return nil, err
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &cappedBody{rc: resp.Body, left: t.max}
	return resp, nil
}

type cappedBody struct {
	rc   io.ReadCloser
	left int64
}

func (b *cappedBody) Read(p []byte) (int, error) {
	if b.left < 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > b.left+1 {
		p = p[:b.left+1]
	}
	n, err := b.rc.Read(p)
	b.left -= int64(n)
	if b.left < 0 {
		return n, ErrTooLarge
	}
	return n, err
}

func (b *cappedBody) Close() error { return b.rc.Close() }
