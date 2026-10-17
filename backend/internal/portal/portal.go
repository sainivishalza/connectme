// Package portal serves the partner portal: a dependency-free single-page app
// for venue managers, embedded in the binary. Like the moderation dashboard it
// is only a UI: every action goes through the authenticated /partners/manage
// API, which proves the caller manages the partner on each request. The page
// shows member-supplied text (booking notes, names of Open-mode members), so
// it gets the same strict CSP: no inline script or style, no third-party
// origins, no framing.
package portal

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed web
var assets embed.FS

const csp = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; " +
	"connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// Handler serves the portal under /partner/.
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
		switch strings.TrimPrefix(r.URL.Path, "/partner") {
		case "", "/", "/index.html", "/app.js", "/style.css":
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/partner")
			if r2.URL.Path == "" {
				r2.URL.Path = "/"
			}
			files.ServeHTTP(w, r2)
		default:
			http.NotFound(w, r)
		}
	})
}
