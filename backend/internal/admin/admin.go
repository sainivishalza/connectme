// Package admin serves the moderator dashboard: a dependency-free single-page
// app embedded in the binary. It is only a UI; every action it performs goes
// through the normal authenticated /moderation API, so server-side role checks
// and the staff audit log apply exactly as for any other client.
package admin

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed web
var assets embed.FS

// csp is much stricter than a typical web app because this page handles
// untrusted user text: no inline script or style, no third-party origins,
// no framing, and it can only talk to its own origin.
const csp = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' blob:; media-src 'self'; " +
	"connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// Handler serves the dashboard under /admin/.
func Handler() http.Handler {
	sub, err := fs.Sub(assets, "web")
	if err != nil {
		panic(err) // the embed directive guarantees "web" exists
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		// Only the three known assets; anything else (including traversal
		// attempts and directory listings) is a plain 404.
		switch strings.TrimPrefix(r.URL.Path, "/admin") {
		case "", "/", "/index.html", "/app.js", "/style.css":
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/admin")
			if r2.URL.Path == "" {
				r2.URL.Path = "/"
			}
			files.ServeHTTP(w, r2)
		default:
			http.NotFound(w, r)
		}
	})
}
