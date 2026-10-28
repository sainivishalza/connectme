package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectme/internal/scamcheck"
)

type fakeChecker struct {
	got  []scamcheck.Line
	fail bool
}

func (f *fakeChecker) Check(_ context.Context, lines []scamcheck.Line) (*scamcheck.Result, error) {
	f.got = lines
	if f.fail {
		return nil, scamcheck.ErrUnavailable
	}
	return &scamcheck.Result{Risk: "high", Score: 80, Signals: []scamcheck.Signal{{Code: "money", Text: "x"}}, Advice: "stop"}, nil
}

func TestScamCheckOnlyForPeopleInTheChatAndNeverKeepsTheLines(t *testing.T) {
	e := setup(t)
	a, _, m := e.matched()
	stranger := e.user("Scam Stranger")
	fc := &fakeChecker{}
	svc := scamcheck.NewService(e.db, fc)
	ctx := context.Background()
	lines := []scamcheck.Line{{Who: "other", Text: "  please   send me money  "}, {Who: "me", Text: "no"}}

	r, err := svc.Check(ctx, a, m, lines)
	if err != nil || r.Risk != "high" {
		t.Fatalf("check: %+v %v", r, err)
	}
	if fc.got[0].Text != "please send me money" {
		t.Errorf("text not tidied: %q", fc.got[0].Text)
	}
	if _, err := svc.Check(ctx, stranger, m, lines); !errors.Is(err, scamcheck.ErrNotFound) {
		t.Errorf("stranger: %v", err)
	}
	if _, err := svc.Check(ctx, a, "not-a-uuid", lines); !errors.Is(err, scamcheck.ErrNotFound) {
		t.Errorf("bad id: %v", err)
	}
	for name, bad := range map[string][]scamcheck.Line{
		"none":       {},
		"bad sender": {{Who: "them", Text: "x"}},
		"too long":   {{Who: "other", Text: strings.Repeat("x", 301)}},
		"control":    {{Who: "other", Text: "a\x00b"}},
		"eleven":     make([]scamcheck.Line, 11),
	} {
		if _, err := svc.Check(ctx, a, m, bad); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	fc.fail = true
	if _, err := svc.Check(ctx, a, m, lines); !errors.Is(err, scamcheck.ErrUnavailable) {
		t.Errorf("AI down: %v", err)
	}

	// Over HTTP: a stranger gets 404, and the lines are in no table.
	if c, _ := e.call("POST", "/scamcheck/"+m, map[string]any{"lines": lines}, stranger); c != 404 {
		t.Errorf("http stranger: %d", c)
	}
	if n := e.count(`SELECT COUNT(*) FROM reports WHERE evidence::text LIKE '%send me money%'`); n != 0 {
		t.Errorf("shared lines were stored: %d", n)
	}
}
