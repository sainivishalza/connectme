package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectme/internal/admin"
)

// The staff view of a user must never carry contact or location data.
func TestStaffViewUserOmitsPrivateFields(t *testing.T) {
	e := setup(t)
	mod := e.newUser("moderator", true, "Mod")
	target := e.user("Target")
	code, body := e.callAs("GET", "/moderation/users/"+e.refOf(target), nil, mod, "moderator")
	if code != 200 {
		t.Fatalf("view user: %d %s", code, body)
	}
	s := strings.ToLower(string(body))
	for _, banned := range []string{`"email"`, "phone", "date_of_birth", "latitude", "longitude", "password"} {
		if strings.Contains(s, banned) {
			t.Errorf("staff view leaks %q: %s", banned, body)
		}
	}
	if n := e.count(`SELECT count(*) FROM staff_audit_log WHERE action='view_user' AND actor_id=$1`, mod); n != 1 {
		t.Errorf("view not audited, got %d entries", n)
	}
}

func TestBanRequiresReasonAndPermission(t *testing.T) {
	e := setup(t)
	mod := e.newUser("moderator", true, "Mod")
	user := e.user("Plain")
	target := e.user("Target")
	path := "/moderation/users/" + e.refOf(target) + "/ban"

	if code, _ := e.callAs("POST", path, map[string]string{"reason": "bad actor"}, user, "user"); code != 403 {
		t.Errorf("ordinary user ban: %d", code)
	}
	if code, _ := e.callAs("POST", path, map[string]string{"reason": "no"}, mod, "moderator"); code != 422 {
		t.Errorf("short reason accepted: %d", code)
	}
	if code, b := e.callAs("POST", path, map[string]string{"reason": "Repeated scam attempts"}, mod, "moderator"); code != 204 {
		t.Fatalf("ban: %d %s", code, b)
	}
	if n := e.count(`SELECT count(*) FROM users WHERE id=$1 AND is_banned`, target); n != 1 {
		t.Error("target not banned")
	}
	if n := e.count(`SELECT count(*) FROM staff_audit_log WHERE action='ban_user'`); n < 1 {
		t.Error("ban not audited")
	}
}

func TestStaffCannotBanHigherOrEqualRole(t *testing.T) {
	e := setup(t)
	mod := e.newUser("moderator", true, "Mod")
	admin := e.newUser("admin", true, "Boss")
	code, _ := e.callAs("POST", "/moderation/users/"+e.refOf(admin)+"/ban",
		map[string]string{"reason": "attempted overreach"}, mod, "moderator")
	if code == 200 {
		t.Fatal("moderator banned an admin")
	}
	if n := e.count(`SELECT count(*) FROM users WHERE id=$1 AND is_banned`, admin); n != 0 {
		t.Error("admin was banned")
	}
}

func TestAuditLogIsAdminOnlyAndAppendOnly(t *testing.T) {
	e := setup(t)
	mod := e.newUser("moderator", true, "Mod")
	adm := e.newUser("admin", true, "Boss")
	if code, _ := e.callAs("GET", "/moderation/audit", nil, mod, "moderator"); code != 403 {
		t.Errorf("moderator read audit log: %d", code)
	}
	if code, _ := e.callAs("GET", "/moderation/audit", nil, adm, "admin"); code != 200 {
		t.Errorf("admin audit read: %d", code)
	}
	e.callAs("GET", "/moderation/stats", nil, mod, "moderator")
	e.exec(`INSERT INTO staff_audit_log (actor_id, actor_role, action) VALUES ($1,'moderator','view_user')`, mod)
	if err := tryExec(e, `DELETE FROM staff_audit_log`); err == nil {
		t.Error("audit log rows can be deleted")
	}
	if err := tryExec(e, `UPDATE staff_audit_log SET action='x'`); err == nil {
		t.Error("audit log rows can be rewritten")
	}
}

func TestAdminDashboardServesOnlyKnownAssetsWithStrictCSP(t *testing.T) {
	h := admin.Handler()
	for path, want := range map[string]int{
		"/admin/": 200, "/admin": 200, "/admin/app.js": 200, "/admin/style.css": 200,
		"/admin/../go.mod": 404, "/admin/secret": 404, "/admin/web/": 404,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("GET %s: %d, want %d", path, rec.Code, want)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/admin/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("weak CSP: %q", csp)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST allowed: %d", rec.Code)
	}
}

// Metrics must never be exposed on the public API router.
func TestMetricsNotServedOnPublicRouter(t *testing.T) {
	e := setup(t)
	if rec := e.fire("GET", "/metrics", nil, "", nil); rec.Code != 404 {
		t.Errorf("/metrics on public router: %d", rec.Code)
	}
}

func tryExec(e *env, sql string) error {
	_, err := e.db.Exec(context.Background(), sql)
	return err
}

func TestStaffAnalyticsShowFunnelAndDailyActivityAsAggregatesOnly(t *testing.T) {
	e := setup(t)
	mod := e.newUser("moderator", true, "Mod")
	plain := e.user("Plain Member")
	if code, _ := e.callAs("GET", "/moderation/analytics", nil, plain, "user"); code != 403 {
		t.Errorf("ordinary member read analytics: %d", code)
	}
	a, b := e.user("Funnel A"), e.user("Funnel B")
	e.swipe(a, b, "like")
	code, body := e.callAs("GET", "/moderation/analytics?days=14", nil, mod, "moderator")
	if code != 200 {
		t.Fatalf("analytics: %d %s", code, body)
	}
	var out struct {
		Funnel struct {
			Days, Registered, Verified, Onboarded, Swiped, Matched int
			WithPhoto                                              int `json:"with_photo"`
		} `json:"funnel"`
		Daily []struct {
			Day              string
			Signups, Swipes  int
			Matches, Reports int
		} `json:"daily"`
	}
	decodeInto(t, body, &out)
	f := out.Funnel
	if f.Days != 14 || len(out.Daily) != 14 {
		t.Errorf("period: days=%d series=%d", f.Days, len(out.Daily))
	}
	if !(f.Registered >= f.Verified && f.Verified >= f.Onboarded && f.Onboarded >= f.WithPhoto && f.WithPhoto >= f.Swiped && f.Swiped >= f.Matched) || f.Registered < 3 {
		t.Errorf("funnel is not a funnel: %+v", f)
	}
	today := out.Daily[len(out.Daily)-1]
	if today.Signups < 3 || today.Swipes < 1 {
		t.Errorf("today: %+v", today)
	}
	// Out-of-range periods are clamped, and nothing personal is in the answer.
	if _, b2 := e.callAs("GET", "/moderation/analytics?days=100000", nil, mod, "moderator"); !strings.Contains(string(b2), `"days":90`) {
		t.Errorf("days not clamped: %s", b2)
	}
	for _, leak := range []string{"email", "ref", e.refOf(a)} {
		if strings.Contains(strings.ToLower(string(body)), leak) {
			t.Errorf("analytics leaks %q", leak)
		}
	}
}

func TestStaffMemberSearchFindsByNameOrRefTreatsWildcardsAsText_AndIsAudited(t *testing.T) {
	e := setup(t)
	mod := e.newUser("moderator", true, "Mod")
	plain := e.user("Plain Searcher")
	target := e.user("Zephyrine Quill")
	ref := e.refOf(target)
	if code, _ := e.callAs("GET", "/moderation/members?q=zeph", nil, plain, "user"); code != 403 {
		t.Errorf("ordinary member searched: %d", code)
	}
	if code, _ := e.callAs("GET", "/moderation/members?q=z", nil, mod, "moderator"); code != 422 {
		t.Errorf("one-letter search: %d", code)
	}
	_, byName := e.callAs("GET", "/moderation/members?q=zephyr", nil, mod, "moderator")
	_, byRef := e.callAs("GET", "/moderation/members?q="+ref, nil, mod, "moderator")
	for name, b := range map[string][]byte{"name": byName, "ref": byRef} {
		if !strings.Contains(string(b), ref) || !strings.Contains(string(b), "Zephyrine Quill") {
			t.Errorf("search by %s missed the member: %s", name, b)
		}
	}
	if _, all := e.callAs("GET", "/moderation/members?q=%25%25", nil, mod, "moderator"); strings.Contains(string(all), ref) {
		t.Errorf("a wildcard listed everyone: %s", all)
	}
	for _, leak := range []string{`"email"`, "date_of_birth", "latitude"} {
		if strings.Contains(string(byName), leak) {
			t.Errorf("search result leaks %s", leak)
		}
	}
	if n := e.count(`SELECT count(*) FROM staff_audit_log WHERE action='member_search' AND actor_id=$1`, mod); n < 3 {
		t.Errorf("searches not audited: %d", n)
	}
}
