package users

import (
	"encoding/json"
	"strings"
	"testing"
)

var secretRow = ProfileRow{
	Ref:              "11111111-1111-1111-1111-111111111111",
	DisplayName:      "Alicia Zanzibar",
	Bio:              "private-bio-text",
	Intro:            "coffee and hikes",
	Mood:             "chill",
	Age:              29,
	Tags:             []string{"hiking", "coffee"},
	Photos:           []PhotoMeta{{ID: "photo-id-abc"}},
	SnappedDistanceM: 3400,
}

func TestResolveVisibilityMatrix(t *testing.T) {
	cases := []struct {
		name string
		mode Mode
		rel  Relation
		want Visibility
	}{
		{"self incognito", ModeIncognito, Relation{Self: true}, VisFull},
		{"stranger open", ModeOpen, Relation{}, VisFull},
		{"stranger incognito", ModeIncognito, Relation{}, VisAnonymous},
		{"stranger hotel", ModeHotel, Relation{}, VisAnonymous},
		{"open match, target now incognito", ModeIncognito, Relation{Matched: true, MatchMode: ModeOpen}, VisFull},
		{"incognito match, target switched to open", ModeOpen, Relation{Matched: true, MatchMode: ModeIncognito}, VisAnonymous},
		{"incognito match, only viewer revealed", ModeIncognito, Relation{Matched: true, MatchMode: ModeIncognito, ViewerRevealed: true}, VisAnonymous},
		{"incognito match, only target revealed", ModeIncognito, Relation{Matched: true, MatchMode: ModeIncognito, TargetRevealed: true}, VisAnonymous},
		{"incognito match, both revealed", ModeIncognito, Relation{Matched: true, MatchMode: ModeIncognito, ViewerRevealed: true, TargetRevealed: true}, VisFull},
		{"hotel match, both revealed", ModeHotel, Relation{Matched: true, MatchMode: ModeHotel, ViewerRevealed: true, TargetRevealed: true}, VisFull},
		{"hotel match, none revealed", ModeHotel, Relation{Matched: true, MatchMode: ModeHotel}, VisAnonymous},
	}
	for _, c := range cases {
		if got := ResolveVisibility(c.mode, c.rel); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// The anonymous projection must never contain identifying data, whatever the row holds.
func TestAnonymousProjectionLeaksNothing(t *testing.T) {
	for _, mode := range []Mode{ModeIncognito, ModeHotel, ModeOpen} {
		row := secretRow
		row.Mode = mode
		raw, err := json.Marshal(Project(row, VisAnonymous))
		if err != nil {
			t.Fatal(err)
		}
		s := string(raw)
		for _, secret := range []string{"Alicia", "Zanzibar", "private-bio-text", "photo-id-abc", "/media/", `"age"`, "29"} {
			if strings.Contains(s, secret) {
				t.Errorf("mode %s: anonymous JSON leaks %q: %s", mode, secret, s)
			}
		}
		for _, want := range []string{"hiking", "coffee and hikes", "within 5 km", `"anonymous":true`} {
			if !strings.Contains(s, want) {
				t.Errorf("mode %s: anonymous JSON missing %q: %s", mode, want, s)
			}
		}
	}
}

func TestFullProjection(t *testing.T) {
	row := secretRow
	row.Mode = ModeOpen
	p := Project(row, VisFull)
	if p.DisplayName != "Alicia Zanzibar" || p.Age != 29 || p.Anonymous {
		t.Fatalf("unexpected: %+v", p)
	}
	if len(p.Photos) != 1 || p.Photos[0] != "/media/photos/photo-id-abc" {
		t.Fatalf("photos: %+v", p.Photos)
	}
}

func TestHotelNameOnlyInHotelMode(t *testing.T) {
	row := secretRow
	row.HotelName = "Grand Hotel"
	row.Mode = ModeIncognito
	if p := Project(row, VisAnonymous); p.HotelName != "" {
		t.Fatal("hotel name must not appear outside hotel mode")
	}
	row.Mode = ModeHotel
	if p := Project(row, VisAnonymous); p.HotelName != "Grand Hotel" {
		t.Fatal("hotel name missing in hotel mode")
	}
}

func TestDistanceLabelBuckets(t *testing.T) {
	cases := map[float64]string{
		0: "within 1 km", 999: "within 1 km", 1001: "within 2 km", 4999: "within 5 km",
		5001: "within 10 km", 99999: "within 100 km", 100001: "100+ km", -1: "",
	}
	for m, want := range cases {
		if got := DistanceLabel(m); got != want {
			t.Errorf("%v m: got %q want %q", m, got, want)
		}
	}
}
