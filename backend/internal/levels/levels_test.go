package levels

import "testing"

func TestLevelsRiseWithTotalsAndStopAtTheTop(t *testing.T) {
	cases := []struct {
		total int64
		level int
		title string
	}{
		{-5, 1, "Newcomer"}, {0, 1, "Newcomer"}, {99, 1, "Newcomer"}, {100, 2, "Newcomer"}, {259, 2, "Newcomer"}, {260, 3, "Newcomer"},
		{760, 5, "Rising"}, {2_000, 8, "Rising"}, {10_000_000, 30, "Legend"},
	}
	for _, c := range cases {
		l := For(c.total)
		if l.Level != c.level || l.Title != c.title {
			t.Errorf("For(%d) = %d %s, want %d %s", c.total, l.Level, l.Title, c.level, c.title)
		}
	}
	prev := int64(-1)
	for lv := 1; lv <= MaxLevel; lv++ {
		th := threshold(lv)
		if th <= prev {
			t.Fatalf("thresholds must increase: level %d = %d after %d", lv, th, prev)
		}
		prev = th
		if got := For(th).Level; got != lv {
			t.Errorf("exactly the threshold of level %d gives level %d", lv, got)
		}
		if lv > 1 && For(th-1).Level != lv-1 {
			t.Errorf("one coin short of level %d should be level %d", lv, lv-1)
		}
	}
}

func TestProgressTowardsTheNextLevelAndTheTop(t *testing.T) {
	l := For(180) // between 100 (L2) and 260 (L3): 80 of 160
	if l.Progress != 50 || l.NextAt != 260 || l.Maxed {
		t.Errorf("midway: %+v", l)
	}
	top := For(1 << 40)
	if !top.Maxed || top.NextAt != 0 || top.Progress != 0 {
		t.Errorf("top: %+v", top)
	}
}

func TestBadgeCarriesNoMoney(t *testing.T) {
	b := For(5_000).Badge()
	if b.Level < 2 || b.Title == "" {
		t.Errorf("badge: %+v", b)
	}
	if l := For(1234).Label(); l == "" {
		t.Error("label")
	}
}
