// Package utils holds input validation helpers shared across modules.
package utils

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MinPasswordRunes = 12
	// bcrypt silently truncates input beyond 72 bytes, so longer is rejected.
	MaxPasswordBytes = 72
	MinAge           = 18
	maxAge           = 120
)

var (
	phoneRE = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
	tagRE   = regexp.MustCompile(`<[^>]*>?`)
)

// NormalizeEmail trims, lowercases and validates an address (no display names).
func NormalizeEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > 254 {
		return "", errors.New("invalid email address")
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") {
		return "", errors.New("invalid email address")
	}
	return s, nil
}

// ValidatePassword enforces: >=12 characters, <=72 bytes, upper, lower, digit, symbol.
func ValidatePassword(p string) error {
	if len(p) > MaxPasswordBytes {
		return errors.New("password must be at most 72 bytes")
	}
	if utf8.RuneCountInString(p) < MinPasswordRunes {
		return errors.New("password must be at least 12 characters")
	}
	var up, low, digit, sym bool
	for _, r := range p {
		switch {
		case unicode.IsUpper(r):
			up = true
		case unicode.IsLower(r):
			low = true
		case unicode.IsDigit(r):
			digit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			sym = true
		}
	}
	if !(up && low && digit && sym) {
		return errors.New("password must include an uppercase letter, a lowercase letter, a number and a symbol")
	}
	return nil
}

// NormalizePhone accepts E.164 (+14155550123), tolerating spaces and dashes.
func NormalizePhone(s string) (string, error) {
	s = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(s))
	if !phoneRE.MatchString(s) {
		return "", errors.New("phone must be in international format, e.g. +14155550123")
	}
	return s, nil
}

// AgeGateReference is the "now" to use for the 18+ gate. It is the clock in
// UTC-12, the last place on Earth to reach a calendar date, so a person is
// only admitted once their 18th birthday has begun *everywhere*. This also
// removes any dependence on the server's local timezone.
func AgeGateReference(now time.Time) time.Time { return now.UTC().Add(-12 * time.Hour) }

// ParseAdultDOB parses YYYY-MM-DD and enforces the 18+ age gate.
func ParseAdultDOB(s string, now time.Time) (time.Time, error) {
	dob, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, errors.New("date_of_birth must be YYYY-MM-DD")
	}
	age := AgeAt(dob, now)
	if age < MinAge {
		return time.Time{}, errors.New("you must be at least 18 years old")
	}
	if age > maxAge || dob.After(now) {
		return time.Time{}, errors.New("invalid date_of_birth")
	}
	return dob, nil
}

// AgeAt returns whole years between dob and now.
func AgeAt(dob, now time.Time) int {
	y := now.Year() - dob.Year()
	if now.Month() < dob.Month() || (now.Month() == dob.Month() && now.Day() < dob.Day()) {
		y--
	}
	return y
}

// StripTags removes HTML tags from free text (names, bios). Output encoding
// must still be applied by whatever renders the text.
func StripTags(s string) string {
	for prev := ""; prev != s; {
		prev = s
		s = tagRE.ReplaceAllString(s, "")
	}
	return strings.TrimSpace(s)
}

// HasControl reports whether s contains a control character (line breaks, NUL,
// escape and the like) or a Unicode line or paragraph separator. Single-line
// fields such as display names must not.
func HasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || r == 0x2028 || r == 0x2029 {
			return true
		}
	}
	return false
}

// MaskEmail keeps the first letter and the domain: "a***@example.com". For logs.
func MaskEmail(s string) string {
	at := strings.LastIndexByte(s, '@')
	if at <= 0 {
		return "***"
	}
	r := []rune(s[:at])
	return string(r[0]) + "***" + s[at:]
}

// MaskPhone keeps only the last two digits: "***42". For logs.
func MaskPhone(s string) string {
	d := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			d = append(d, r)
		}
	}
	if len(d) < 2 {
		return "***"
	}
	return "***" + string(d[len(d)-2:])
}
