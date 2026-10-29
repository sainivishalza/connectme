package utils

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	good := map[string]string{" A@Example.com ": "a@example.com", "x.y+z@mail.co": "x.y+z@mail.co"}
	for in, want := range good {
		if got, err := NormalizeEmail(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "nope", "a@b", "Bob <b@x.com>", "a@@b.com", strings.Repeat("a", 250) + "@x.com"} {
		if _, err := NormalizeEmail(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("Correct-Horse-9"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
	for name, p := range map[string]string{
		"short":     "Aa1!aaaa",
		"no upper":  "correct-horse-9",
		"no lower":  "CORRECT-HORSE-9",
		"no digit":  "Correct-Horse-x",
		"no symbol": "CorrectHorse99",
		"too long":  "Aa1!" + strings.Repeat("x", 80),
	} {
		if err := ValidatePassword(p); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
}

func TestParseAdultDOB(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	cases := map[string]bool{
		"2008-10-15": true,  // exactly 18 today
		"2008-10-16": false, // 17 until tomorrow
		"2010-01-01": false,
		"1990-05-05": true,
		"1890-01-01": false,
		"2030-01-01": false,
		"15/10/1990": false,
	}
	for in, ok := range cases {
		_, err := ParseAdultDOB(in, now)
		if (err == nil) != ok {
			t.Errorf("%s: ok=%v err=%v", in, ok, err)
		}
	}
}

func TestPhoneAndStripTags(t *testing.T) {
	if p, err := NormalizePhone("+1 415-555-0123"); err != nil || p != "+14155550123" {
		t.Fatalf("got %q %v", p, err)
	}
	if _, err := NormalizePhone("4155550123"); err == nil {
		t.Fatal("missing + must be rejected")
	}
	if got := StripTags("hi <script>alert(1)</script> <b>you</b><img src=x onerror="); got != "hi alert(1) you" {
		t.Fatalf("got %q", got)
	}
}

func TestAgeGateReferenceIsConservative(t *testing.T) {
	// 2026-10-16 02:00 in UTC+8 is still 2026-10-15 18:00 UTC and 2026-10-15 06:00 in UTC-12.
	local := time.Date(2026, 10, 16, 2, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	ref := AgeGateReference(local)
	if ref.Year() != 2026 || ref.Month() != 10 || ref.Day() != 15 {
		t.Fatalf("reference date %v", ref)
	}
	// Born 2008-10-16: 18 in UTC+8 already, but not yet on the UTC-12 calendar.
	if _, err := ParseAdultDOB("2008-10-16", ref); err == nil {
		t.Fatal("must not admit someone whose 18th birthday has not begun everywhere")
	}
	if _, err := ParseAdultDOB("2008-10-15", ref); err != nil {
		t.Fatalf("born 2008-10-15 is 18 everywhere: %v", err)
	}
}

func TestHasControl(t *testing.T) {
	for in, want := range map[string]bool{
		"Anna": false, "Zoë": false, "李雷": false, "a b": false,
		"a\nb": true, "a\r\nb": true, "a\x00": true, "a b": true, "a b": true, "a\x1bb": true, "a\x7f": true, "a\u0085b": true,
	} {
		if got := HasControl(in); got != want {
			t.Errorf("HasControl(%q) = %v, want %v", in, got, want)
		}
	}
}
