package push

import (
	"testing"
	"time"
)

func TestQuietWindow(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.UTC) }
	night := Quiet{On: true, Start: 22 * 60, End: 8 * 60}
	for name, c := range map[string]struct {
		q    Quiet
		now  time.Time
		want bool
	}{
		"off":                    {Quiet{Start: 0, End: 1000}, at(5, 0), false},
		"overnight inside late":  {night, at(23, 30), true},
		"overnight inside early": {night, at(3, 0), true},
		"overnight edge start":   {night, at(22, 0), true},
		"overnight edge end":     {night, at(8, 0), false},
		"daytime outside":        {night, at(12, 0), false},
		"same day window":        {Quiet{On: true, Start: 13 * 60, End: 15 * 60}, at(14, 0), true},
		"same day outside":       {Quiet{On: true, Start: 13 * 60, End: 15 * 60}, at(16, 0), false},
		"time zone shifts it":    {Quiet{On: true, Start: 22 * 60, End: 8 * 60, TZMin: 120}, at(21, 0), true},  // 23:00 local
		"negative zone":          {Quiet{On: true, Start: 22 * 60, End: 8 * 60, TZMin: -300}, at(2, 0), false}, // 21:00 local
	} {
		if got := c.q.Active(c.now); got != c.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}
