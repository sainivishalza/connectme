package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync/atomic"
)

var trusted atomic.Pointer[[]*net.IPNet]

// SetTrustedProxies declares which peers are our own load balancers/ingress.
// Only for those peers is X-Forwarded-For honoured; from anyone else it is
// ignored, because a client can put anything in it. Pass nil to trust none
// (the default).
func SetTrustedProxies(cidrs []*net.IPNet) { trusted.Store(&cidrs) }

// ParseCIDRs parses a list of CIDRs or bare IPs.
func ParseCIDRs(items []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			if ip := net.ParseIP(s); ip != nil {
				if ip.To4() != nil {
					s += "/32"
				} else {
					s += "/128"
				}
			}
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func isTrusted(ip net.IP) bool {
	p := trusted.Load()
	if p == nil {
		return false
	}
	for _, n := range *p {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP returns the caller's address: the TCP peer, unless the peer is a
// trusted proxy, in which case the right-most X-Forwarded-For entry that is
// not itself a trusted proxy (the right-most untrusted hop cannot be forged
// by the client, whereas the left-most can).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !isTrusted(peer) {
		return host
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(hops[i]))
		if ip == nil {
			continue
		}
		if !isTrusted(ip) {
			return ip.String()
		}
	}
	return host
}
