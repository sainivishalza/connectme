package integration

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These checks read the source tree. They are not a substitute for review,
// but they make three classes of regression impossible to merge silently.

func backendRoot(t *testing.T) string {
	t.Helper()
	// the test runs in internal/integration
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func goSources(t *testing.T, includeTests bool) map[string]string {
	t.Helper()
	root := backendRoot(t)
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		if !includeTests && strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// API4 / API2: every route registration in a handler carries a rate limiter.
func TestAuditEveryRouteRegistrationIsRateLimited(t *testing.T) {
	reg := regexp.MustCompile(`\br\.(?:With\([^\n]*\)\.)?(Get|Post|Put|Patch|Delete)\("`)
	staffReg := regexp.MustCompile(`\bstaff\.(Get|Post)\("`)
	limiter := regexp.MustCompile(`\blim\(|\blimit\(|RateLimit\(|\bread\b|\bwrite\b`)
	// Route lines deliberately without a per-user limiter.
	exempt := []string{`"/webhook"`, `"/healthz"`, `"/readyz"`}

	checked := 0
	for file, src := range goSources(t, false) {
		if !strings.HasSuffix(file, "handler.go") {
			continue
		}
		for i, line := range strings.Split(src, "\n") {
			if !reg.MatchString(line) && !staffReg.MatchString(line) {
				continue
			}
			if staffReg.MatchString(line) { // `staff` is defined with a limiter in the same file
				if !strings.Contains(src, "staff := r.With(") || !strings.Contains(src, `lim("staff"`) {
					t.Errorf("%s:%d staff route without limiter", file, i+1)
				}
				checked++
				continue
			}
			skip := false
			for _, e := range exempt {
				skip = skip || strings.Contains(line, e)
			}
			if skip {
				continue
			}
			// A limiter applied for the whole router with r.Use covers its routes.
			routerWide := strings.Contains(src, "r.Use(middleware.Auth(secret), middleware.RateLimit(")
			if !limiter.MatchString(line) && !routerWide {
				t.Errorf("%s:%d route has no rate limiter: %s", file, i+1, strings.TrimSpace(line))
			}
			checked++
		}
	}
	if checked < 60 {
		t.Fatalf("only %d route registrations inspected; the pattern probably stopped matching", checked)
	}
}

// API8 / injection: SQL is parameterised. The only string concatenation into
// SQL is a fixed set of internal helpers with literal arguments.
func TestAuditNoStringBuiltSQL(t *testing.T) {
	srcs := goSources(t, false)

	for file, src := range srcs {
		for i, line := range strings.Split(src, "\n") {
			l := strings.ToLower(line)
			if strings.Contains(l, "sprintf") && (strings.Contains(l, "select ") || strings.Contains(l, "insert into") ||
				strings.Contains(l, "update ") || strings.Contains(l, "delete from")) {
				t.Errorf("%s:%d SQL built with Sprintf: %s", file, i+1, strings.TrimSpace(line))
			}
		}
	}

	// Only the *opening* fragment matters: "`+<identifier>". Each must be a
	// fixed internal helper or constant, never request data.
	allowed := map[string]bool{
		"snapDeg": true, "where": true, "candidateSQL": true, "userIDExpr": true, "a": true, "b": true,
		"users.NotBlockedSQL": true, "notBlocked": true, "entitlements.ActiveTierSQL": true, "ActiveTierSQL": true,
		"itoa": true, "videoCols": true,
	}
	concat := regexp.MustCompile("`" + `\s*\+\s*([A-Za-z_][A-Za-z0-9_.]*)`)
	seen := 0
	for file, src := range srcs {
		for _, m := range concat.FindAllStringSubmatch(src, -1) {
			seen++
			if !allowed[m[1]] {
				t.Errorf("%s: unexpected concatenation into SQL: %q", file, m[0])
			}
		}
	}
	if seen < 15 {
		t.Fatalf("only %d SQL splices inspected; the pattern probably stopped matching", seen)
	}

	// The helpers that splice SQL must only ever receive string literals.
	call := regexp.MustCompile(`(?:NotBlockedSQL|notBlocked|ActiveTierSQL)\(([^()]*)\)`)
	lit := regexp.MustCompile(`^\s*"[^"]*"(\s*,\s*"[^"]*")*\s*$`)
	for file, src := range srcs {
		for _, m := range call.FindAllStringSubmatch(src, -1) {
			args := m[1]
			if strings.TrimSpace(args) == "a, b" || strings.Contains(args, " string") {
				continue // the helper's own definition / thin wrapper (its callers are checked)
			}
			if !lit.MatchString(args) {
				t.Errorf("%s: SQL helper called with a non-literal: (%s)", file, args)
			}
		}
	}

	// Dynamic UPDATE columns come only from literal keys.
	u := srcs["internal/users/service.go"]
	for _, m := range regexp.MustCompile(`set\[([^\]]+)\]\s*=`).FindAllStringSubmatch(u, -1) {
		if !regexp.MustCompile(`^"[a-z_]+"$`).MatchString(m[1]) {
			t.Errorf("profile column key is not a literal: %s", m[1])
		}
	}
}

// API8 / A02: no committed secrets, no weak primitives.
func TestAuditNoSecretsOrWeakCrypto(t *testing.T) {
	patterns := map[string]*regexp.Regexp{
		"live Stripe key":    regexp.MustCompile(`sk_live_[0-9a-zA-Z]{10,}`),
		"AWS access key":     regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		"private key":        regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
		"hardcoded password": regexp.MustCompile(`(?i)(password|passwd|secret)\s*[:=]\s*"[^"$]{8,}"`),
		"webhook secret":     regexp.MustCompile(`whsec_[0-9a-zA-Z]{16,}`),
	}
	for file, src := range goSources(t, false) {
		for name, re := range patterns {
			if loc := re.FindStringIndex(src); loc != nil {
				t.Errorf("%s: possible %s: %q", file, name, src[loc[0]:min(loc[1], loc[0]+40)])
			}
		}
		for _, bad := range []string{`"math/rand"`, `"crypto/md5"`, `"crypto/sha1"`, `"crypto/des"`, `"crypto/rc4"`} {
			if strings.Contains(src, bad) {
				t.Errorf("%s imports %s; security code must use crypto/rand and modern hashes", file, bad)
			}
		}
		if strings.Contains(src, "InsecureSkipVerify") {
			t.Errorf("%s disables TLS verification", file)
		}
	}
	// .env must never be committed.
	if b, err := os.ReadFile(filepath.Join(backendRoot(t), "..", ".gitignore")); err != nil || !strings.Contains(string(b), ".env") {
		t.Error(".env is not gitignored")
	}
}

// Passwords are bcrypt at cost >= 12 (spec) via a single constant.
func TestAuditPasswordHashingCost(t *testing.T) {
	src := goSources(t, false)["internal/auth/service.go"]
	if !regexp.MustCompile(`bcryptCost\s*=\s*12\b`).MatchString(src) {
		t.Fatal("bcryptCost must be 12")
	}
	if strings.Count(src, "bcrypt.GenerateFromPassword") < 2 {
		t.Fatal("expected registration and reset to hash with bcrypt")
	}
	if strings.Contains(src, "GenerateFromPassword([]byte(req.Password), 10)") {
		t.Fatal("hard-coded low cost")
	}
}

// Dependencies come from the module proxy with checksums (go.sum committed).
func TestAuditModuleChecksumsPresent(t *testing.T) {
	for _, f := range []string{"go.mod", "go.sum"} {
		if _, err := os.Stat(filepath.Join(backendRoot(t), f)); err != nil {
			t.Errorf("%s missing: %v", f, err)
		}
	}
}
