package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// goFiles lists the non-test Go files under internal/ (relative to this package).
func goFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.Walk("..", func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var requestDecoder = regexp.MustCompile(`json\.NewDecoder\((?:http\.MaxBytesReader\([^,]+,\s*)?(?:r|req)\.Body`)

// A request body is decoded into an explicit struct and unknown fields are
// refused, so a client can never slip in a field such as role, tier or coins
// that the struct does not declare. Every decoder of a request body must say so
// within the next few lines.
func TestEveryRequestDecoderRefusesUnknownFields(t *testing.T) {
	for _, p := range goFiles(t) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		for i, l := range lines {
			if !requestDecoder.MatchString(l) {
				continue
			}
			end := i + 4
			if end > len(lines) {
				end = len(lines)
			}
			if !strings.Contains(strings.Join(lines[i:end], "\n"), "DisallowUnknownFields") {
				t.Errorf("%s:%d decodes a request body without DisallowUnknownFields", filepath.ToSlash(p), i+1)
			}
		}
	}
}
