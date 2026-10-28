// Package investors is the ownership ledger behind the "Invest" page.
//
// The rule is simple and the same everywhere: the founder holds 51%; the other
// 49% belongs to confirmed investors in proportion to the money each put in.
// With $1,000 per 1% as the starting price, the first $49,000 fills the pool;
// when more money is confirmed, everyone's share of the 49% shrinks in
// proportion, so percentages are always derived from the confirmed amounts and
// never stored.
package investors

import (
	"fmt"
	"strings"
)

const (
	// FounderPercent is the founder's fixed holding.
	FounderPercent = 51
	// PoolPercent is what all investors share between them.
	PoolPercent = 49
	// microPerPercent: percentages are kept to six decimals (0.000001%).
	microPerPercent = 1_000_000
)

// PoolMicro is the investor pool in millionths of a percent.
const PoolMicro = int64(PoolPercent) * microPerPercent

// ShareMicro returns an investor's holding in millionths of a percent:
// 49% x amount / total, rounded down. total must be > 0.
func ShareMicro(amountCents, totalCents int64) int64 {
	if totalCents <= 0 || amountCents <= 0 {
		return 0
	}
	// 49,000,000 x amount fits in int64 for any amount up to ~1.8e11 cents.
	return PoolMicro * amountCents / totalCents
}

// Two ways to turn money into a share. Both give the founder 51% and share the
// other 49% between investors; they differ in what happens as more money
// arrives.
const (
	// ModeProRata (the default): the 49% is shared in proportion to the money
	// confirmed, so a new investment lowers everyone else's percentage.
	ModeProRata = "prorata"
	// ModeFixed: every investor buys a fixed percentage at a fixed price per
	// 1% (for example $1,000), nobody is diluted, and the 49% sells out once
	// $49,000 (at that price) has been confirmed.
	ModeFixed = "fixed"
	// DefaultPricePerPercentCents is $1,000 for 1%.
	DefaultPricePerPercentCents = 100_000
)

// FixedShareMicro is the holding bought at a fixed price: amount / price per
// 1%, rounded down, in millionths of a percent.
func FixedShareMicro(amountCents, pricePerPercentCents int64) int64 {
	if amountCents <= 0 || pricePerPercentCents <= 0 {
		return 0
	}
	return amountCents * microPerPercent / pricePerPercentCents
}

// FormatPercent renders millionths of a percent with up to six decimals and no
// trailing zeros: 500000 -> "0.5", 10 -> "0.00001", 49000000 -> "49".
func FormatPercent(micro int64) string {
	whole, frac := micro/microPerPercent, micro%microPerPercent
	if frac == 0 {
		return fmt.Sprintf("%d", whole)
	}
	f := strings.TrimRight(fmt.Sprintf("%06d", frac), "0")
	return fmt.Sprintf("%d.%s", whole, f)
}

// ParseUSD turns "12", "12.5" or "12.50" into cents. It refuses more than two
// decimals, signs, exponents and anything that is not plain digits.
func ParseUSD(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" || len(whole) > 9 || (hasDot && (frac == "" || len(frac) > 2)) {
		return 0, false
	}
	var cents int64
	for _, c := range whole {
		if c < '0' || c > '9' {
			return 0, false
		}
		cents = cents*10 + int64(c-'0')
	}
	cents *= 100
	for i, c := range frac {
		if c < '0' || c > '9' {
			return 0, false
		}
		cents += int64(c-'0') * map[int]int64{0: 10, 1: 1}[i]
	}
	return cents, true
}
