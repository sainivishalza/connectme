package integration

import (
	"fmt"
	"math/rand"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"connectme/pkg/middleware"
)

// ownershipRules say, for each group of routes that take an identifier, what
// proves the caller may touch THAT object. They were written from reading the
// service code, not from comments. A route with an identifier and no matching
// rule is printed as UNREVIEWED so it cannot hide.
var ownershipRules = []struct {
	re   *regexp.Regexp
	text string
}{
	{regexp.MustCompile(`^/chat/(matches|groups)/\{id\}`), "reviewed: chat.participant(): party to an unexpired matched match, or member of a matched group pair; neither user banned; else 404"},
	{regexp.MustCompile(`^/chat/keys/\{matchID\}`), "reviewed: chat.participant(), then only the OTHER party's key bundle; consumes one one-time prekey per call (see audit: prekey drain)"},
	{regexp.MustCompile(`^/(investors/admin|sponsors)/`), "staff permission enforced in middleware (admin role / manage-hotels); not an object owner check"},
	{regexp.MustCompile(`^/chat/media/\{id\}`), "reviewed: media document's match id, then chat.participant()"},
	{regexp.MustCompile(`^/calls/\{id\}`), "reviewed: caller_id or callee_id of the call (accept/decline: callee only)"},
	{regexp.MustCompile(`^/matching/matches/\{id\}`), "reviewed: caller is user1/user2 of the match, status matched"},
	{regexp.MustCompile(`^/media/photos/\{id\}`), "reviewed: owner verified, not banned, not blocked, mode visibility must be full; else 404"},
	{regexp.MustCompile(`^/users/me/photos/\{id\}`), "reviewed: photo id must be in the caller's own profile photos"},
	{regexp.MustCompile(`^/geo/hotels/\{id\}`), "reviewed: caller has an open check-in at that hotel (activeCheckin)"},
	{regexp.MustCompile(`^/safety/(contacts|plans)/\{id\}`), "reviewed: SQL filters id AND user_id = caller"},
	{regexp.MustCompile(`^/scamcheck/\{conv\}`), "reviewed: caller is party to the match or a matched group"},
	{regexp.MustCompile(`^/games/(td|party|quiz|group)/`), "reviewed: games.member(): caller in the match or matched group"},
	{regexp.MustCompile(`^/games/masked/`), "spot-checked: masked_members membership"},
	{regexp.MustCompile(`^/social/posts/\{id\}`), "reviewed: social.canSee(): not blocked/banned and within radius, same hotel stay, or community member"},
	{regexp.MustCompile(`^/social/claims/\{id\}`), "reviewed: owned(): caller is the post's author"},
	{regexp.MustCompile(`^/social/comments/\{id\}`), "spot-checked: author of the comment or of the post"},
	{regexp.MustCompile(`^/follows/\{(ref|direction)\}`), "reviewed: follows.target(): visible-ref resolution (blocked/hidden look like not found)"},
	{regexp.MustCompile(`^/investors/pledges/\{id\}`), "reviewed: user_id = caller"},
	{regexp.MustCompile(`^/forums/(threads|replies)/\{id\}$`), "reviewed for DELETE: author_id = caller; GET is public board content"},
	{regexp.MustCompile(`^/forums/`), "public board content; reports are per caller"},
	{regexp.MustCompile(`^/communities/\{id\}`), "spot-checked: community_members membership / owner role"},
	{regexp.MustCompile(`^/groups/`), "reviewed for GET /groups/{id}: isMember(); other group routes spot-checked"},
	{regexp.MustCompile(`^/videos/`), "spot-checked: open-mode, verified, not banned, not blocked; delete: owner; file: short-lived HMAC ticket"},
	{regexp.MustCompile(`^/random/\{id\}/keep`), "reviewed: caller is party to a matched random-origin match"},
	{regexp.MustCompile(`^/signal/`), "reviewed: signal.Allowed(): per-kind membership"},
	{regexp.MustCompile(`^/users/\{ref\}`), "spot-checked: users.GetPublic(): ResolveVisibleRef + projection by relation and mode"},
	{regexp.MustCompile(`^/media/voice/\{ref\}`), "spot-checked: users.OpenVoiceIntro visibility"},
	{regexp.MustCompile(`^/chapters/\{ref\}`), "spot-checked: visible-ref resolution"},
	{regexp.MustCompile(`^/levels/board/`), "aggregate public boards; opt-in per member; blocks applied per viewer"},
	{regexp.MustCompile(`^/(gifs|sounds)/\{id\}`), "public generated assets"},
	{regexp.MustCompile(`^/sponsors/\{id\}/click`), "any signed-in user; records a click"},
	{regexp.MustCompile(`^/moderation/blocks/\{ref\}`), "caller's own block list"},
	{regexp.MustCompile(`^/moderation/`), "staff role enforced in middleware; per-object staff actions are audited"},
	{regexp.MustCompile(`^/idcheck/`), "member: own check; staff: role-gated queue"},
	{regexp.MustCompile(`^/payments/`), "own account; Stripe objects are matched through signed metadata"},
}

// probeRoute calls a route once with an empty JSON body and reports the status
// and the rate limit it advertises. Each call uses a fresh verified user.
func (e *env) probeRoute(method, path string, signedIn bool, role string) (int, string) {
	req := httptest.NewRequest(method, concrete(path), strings.NewReader("{}"))
	if signedIn {
		tok, _ := middleware.SignAccessToken([]byte(secret), e.user("Probe"), role, time.Minute)
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1", rand.Intn(256), rand.Intn(256), rand.Intn(256))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec.Code, rec.Header().Get("X-RateLimit-Limit")
}

func ownershipOf(path string) string {
	if !strings.Contains(path, "{") {
		return "n/a: acts on the caller's own data (user id comes from the signed token)"
	}
	for _, r := range ownershipRules {
		if r.re.MatchString(path) {
			return r.text
		}
	}
	return "**UNREVIEWED**"
}

// TestWriteSecurityRoutes regenerates docs/SECURITY_ROUTES.md from the live
// router: method, path, auth, lowest role that gets through, the rate limit the
// route reports, and how ownership is proved. Run with UPDATE_SECURITY_ROUTES=1.
func TestWriteSecurityRoutes(t *testing.T) {
	if os.Getenv("UPDATE_SECURITY_ROUTES") != "1" {
		t.Skip("set UPDATE_SECURITY_ROUTES=1 to regenerate docs/SECURITY_ROUTES.md")
	}
	e := setup(t)
	var b strings.Builder
	b.WriteString("# Security route map\n\nGenerated from the running router by `UPDATE_SECURITY_ROUTES=1 go test ./internal/integration -run TestWriteSecurityRoutes`.\n")
	b.WriteString("**Auth**: `public` = on the audited allowlist, `user` = signed access token, `staff` = moderator/admin role.\n")
	b.WriteString("**Role**: lowest role observed to get past the role check. **Rate limit**: the `X-RateLimit-Limit` the route reports to a signed-in caller; `none` is a finding.\n")
	b.WriteString("**Ownership**: how the code proves the caller may touch that object. `reviewed` = read in the service code, `spot-checked` = looked at, not proven for every method, `UNREVIEWED` = not looked at.\n\n")
	b.WriteString("| Method | Path | Auth | Role | Rate limit | Ownership / object check |\n|---|---|---|---|---|---|\n")

	var noLimit, unreviewed []string
	for _, r := range routesOf(t, e.h) {
		key := r.Method + " " + r.Path
		norm := r.Method + " " + strings.TrimSuffix(r.Path, "/") // the audit lists are written without the trailing slash
		if norm == r.Method+" " {
			norm = key
		}
		auth, role := "user", "user"
		switch {
		case publicRoutes[key] || publicRoutes[norm]:
			auth, role = "public", "-"
		case staffRoutes[key] || staffRoutes[norm]:
			auth = "staff"
		}
		limit := "none"
		hdr := ""
		switch auth {
		case "public":
			_, hdr = e.probeRoute(r.Method, r.Path, false, "")
		case "staff":
			for _, rl := range []string{"moderator", "admin"} {
				code, h := e.probeRoute(r.Method, r.Path, true, rl)
				if code != 401 && code != 403 {
					role, hdr = rl, h
					break
				}
			}
		default:
			_, hdr = e.probeRoute(r.Method, r.Path, true, "user")
		}
		if hdr != "" {
			limit = hdr
		} else if auth != "public" {
			noLimit = append(noLimit, key)
		}
		own := ownershipOf(r.Path)
		if strings.Contains(own, "UNREVIEWED") {
			unreviewed = append(unreviewed, key)
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s | %s | %s |\n", r.Method, r.Path, auth, role, limit, own)
	}

	fmt.Fprintf(&b, "\n## Gaps found by the generator\n\n- Routes reporting no rate limit: %d\n", len(noLimit))
	for _, k := range noLimit {
		fmt.Fprintf(&b, "  - `%s`\n", k)
	}
	fmt.Fprintf(&b, "- Routes with an identifier and no reviewed ownership rule: %d\n", len(unreviewed))
	for _, k := range unreviewed {
		fmt.Fprintf(&b, "  - `%s`\n", k)
	}

	b.WriteString(`
## WebSocket events (Node relay, realtime/src/server.js)

Authentication is the signed access token sent in the Socket.IO connect packet (not in the URL). Every event shares one per-user budget of 240/min; messages have their own 30/min. Both budgets live in the relay's memory (see the audit, finding on rate limits).

| Event | Auth | Object check | Limit |
|---|---|---|---|
| connect | access token verified (HS256, issuer, typ=access) | - | none per IP, no cap on sockets per user |
| authenticate | same subject as the connection | - | 240/min/user |
| join_match | token | API ` + "`GET /chat/matches/{id}/access`" + ` (chat.participant) | 240/min/user |
| leave_match | token | - | 240/min/user |
| watch / unwatch | token | API ` + "`GET /signal/{kind}/{id}/access`" + `; max 8 per socket | 240/min/user |
| message | token, room joined | API ` + "`POST /chat/matches/{id}/messages`" + ` re-checks participant | 30/min/user |
| typing | token, room joined | re-check via access endpoint every 20 s | 240/min/user |
| read_receipt | token, room joined | API ` + "`POST /chat/matches/{id}/read`" + ` | 240/min/user |

Server pushes: ` + "`message`, `typing`, `read_receipt`, `match_closed`, `auth_expired`, `signal`" + `.
`)
	if err := os.WriteFile("../../../docs/SECURITY_ROUTES.md", []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// isStaffRoute and isPublicRoute accept the path with or without a trailing
// slash: the audit lists are written without it and the router registers some
// routes with it (a mismatch once hid two staff routes from the 403 check).
func isStaffRoute(r route) bool {
	return staffRoutes[r.Method+" "+r.Path] || staffRoutes[r.Method+" "+strings.TrimSuffix(r.Path, "/")]
}

func isPublicRoute(r route) bool {
	return publicRoutes[r.Method+" "+r.Path] || publicRoutes[r.Method+" "+strings.TrimSuffix(r.Path, "/")]
}

// Public routes that may report no rate limit (they do no work worth limiting).
var noLimitOK = map[string]bool{"GET /healthz": true, "GET /readyz": true}

// A table row: | METHOD | `path` | auth | ... (the backticks are matched by "." so the pattern needs no escapes).
var docRow = regexp.MustCompile(`^[|] ([A-Z]+) [|] .(.+?). [|] (public|user|staff) [|]`)

// TestRouteRegistry makes a security decision unavoidable for every new route:
// it must be listed in docs/SECURITY_ROUTES.md (regenerate it), have a rate
// limit, and, when it takes an identifier, a reviewed ownership rule. Staff
// routes must refuse an ordinary member.
func TestRouteRegistry(t *testing.T) {
	e := setup(t)
	raw, err := os.ReadFile("../../../docs/SECURITY_ROUTES.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]string{}
	for _, l := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if m := docRow.FindStringSubmatch(l); m != nil {
			doc[m[1]+" "+m[2]] = l
		}
	}
	live := map[string]bool{}
	member := e.user("Registry Member")
	for _, r := range routesOf(t, e.h) {
		key := r.Method + " " + r.Path
		live[key] = true
		line, ok := doc[key]
		if !ok {
			t.Errorf("%s is not in docs/SECURITY_ROUTES.md: make the security decision (auth, ownership, rate limit, role) and regenerate it with UPDATE_SECURITY_ROUTES=1", key)
			continue
		}
		if strings.Contains(line, "UNREVIEWED") {
			t.Errorf("%s takes an identifier but has no reviewed ownership rule", key)
		}
		if isPublicRoute(r) {
			if !noLimitOK[key] {
				if _, h := e.probeRoute(r.Method, r.Path, false, ""); h == "" {
					t.Errorf("public route %s has no rate limit", key)
				}
			}
			continue
		}
		if isStaffRoute(r) {
			code, _ := e.probeRoute(r.Method, r.Path, true, "user")
			if code != 403 {
				t.Errorf("staff route %s answered an ordinary member with %d, want 403", key, code)
			}
			if _, h := e.probeRoute(r.Method, r.Path, true, "admin"); h == "" {
				t.Errorf("staff route %s has no rate limit", key)
			}
			continue
		}
		if _, h := e.probeRoute(r.Method, r.Path, true, "user"); h == "" {
			t.Errorf("route %s has no rate limit", key)
		}
	}
	for key := range doc {
		if !live[key] {
			t.Errorf("docs/SECURITY_ROUTES.md lists %s but the router does not: regenerate the file", key)
		}
	}
	_ = member
}
