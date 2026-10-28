// Package levels turns lifetime gift activity into friendly levels.
//
// Two ladders, both derived (never stored, so they can never drift from the
// ledger): Charm, from coins received as gifts, and Supporter, from coins sent.
// Charm is shown on a member's visible profile by default; Supporter is
// private unless the member chooses to show it, because how much someone has
// spent is theirs to share.
package levels

import "fmt"

// MaxLevel is the top of both ladders.
const MaxLevel = 30

// threshold is the total coins needed to reach a level (level 1 needs none).
func threshold(level int) int64 {
	n := int64(level - 1)
	return 30*n*n + 70*n
}

// Level is one position on a ladder.
type Level struct {
	Level    int    `json:"level"`
	Title    string `json:"title"`
	Total    int64  `json:"total"`
	NextAt   int64  `json:"next_at,omitempty"`  // coins needed for the next level (0 at the top)
	Progress int    `json:"progress,omitempty"` // percent of the way to the next level
	Maxed    bool   `json:"maxed,omitempty"`
}

// For returns the level for a lifetime total of coins.
func For(total int64) Level {
	if total < 0 {
		total = 0
	}
	l := 1
	for l < MaxLevel && total >= threshold(l+1) {
		l++
	}
	out := Level{Level: l, Title: Title(l), Total: total}
	if l >= MaxLevel {
		out.Maxed = true
		return out
	}
	lo, hi := threshold(l), threshold(l+1)
	out.NextAt = hi
	out.Progress = int((total - lo) * 100 / (hi - lo))
	return out
}

// Title is the name of the band a level sits in.
func Title(level int) string {
	switch {
	case level >= 25:
		return "Legend"
	case level >= 20:
		return "Radiant"
	case level >= 15:
		return "Shining"
	case level >= 10:
		return "Bright"
	case level >= 5:
		return "Rising"
	default:
		return "Newcomer"
	}
}

// Badge is all that other people ever see of a level: its number and its
// name, never the coins behind it.
type Badge struct {
	Level int    `json:"level"`
	Title string `json:"title"`
}

// Badge returns the public face of the level.
func (l Level) Badge() Badge { return Badge{Level: l.Level, Title: l.Title} }

// Label is how a level reads on a badge, for example "Lv 12 Bright".
func (l Level) Label() string { return fmt.Sprintf("Lv %d %s", l.Level, l.Title) }
