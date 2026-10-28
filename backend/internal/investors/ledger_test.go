package investors

import "testing"

func TestSharesDiluteAsMoneyArrives(t *testing.T) {
	const thousand = 100_000 // cents
	// First $49,000 fills the pool: $1,000 is 1%.
	if got := FormatPercent(ShareMicro(thousand, 49*thousand)); got != "1" {
		t.Errorf("first round: %s", got)
	}
	// Another $49,000 arrives: the same $1,000 is now 0.5%.
	if got := FormatPercent(ShareMicro(thousand, 98*thousand)); got != "0.5" {
		t.Errorf("after second round: %s", got)
	}
	// $1 into a pool priced at $1,000 per 1% is 0.001%.
	if got := FormatPercent(ShareMicro(100, 49*thousand)); got != "0.001" {
		t.Errorf("one dollar: %s", got)
	}
	// $10 is 0.01%.
	if got := FormatPercent(ShareMicro(1000, 49*thousand)); got != "0.01" {
		t.Errorf("ten dollars: %s", got)
	}
	// Everyone together never exceeds the pool; the sole investor owns all 49%.
	if got := FormatPercent(ShareMicro(500, 500)); got != "49" {
		t.Errorf("sole investor: %s", got)
	}
	var sum int64
	for _, a := range []int64{12345, 99999, 7, 100, 555555} {
		sum += ShareMicro(a, 12345+99999+7+100+555555)
	}
	if sum > PoolMicro || PoolMicro-sum > 5 {
		t.Errorf("rounding lost too much: %d of %d", sum, PoolMicro)
	}
	if ShareMicro(100, 0) != 0 || ShareMicro(0, 100) != 0 {
		t.Error("empty pool or amount must be zero")
	}
}

func TestFormatPercent(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 500000: "0.5", 10: "0.00001", 1: "0.000001", 49000000: "49", 12345678: "12.345678", 1000000: "1"} {
		if got := FormatPercent(in); got != want {
			t.Errorf("%d: got %s want %s", in, got, want)
		}
	}
}

func TestParseUSD(t *testing.T) {
	for in, want := range map[string]int64{"1": 100, "12.5": 1250, "12.50": 1250, "0.99": 99, "1000": 100000, " 7 ": 700} {
		if got, ok := ParseUSD(in); !ok || got != want {
			t.Errorf("%q: got %d %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "-5", "1e3", "1.234", "1.", ".5", "abc", "1,000", "9999999999", "+3"} {
		if _, ok := ParseUSD(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestFixedPriceSharesNeverDilute(t *testing.T) {
	const thousand = 100_000 // cents: $1,000 for 1%
	for cents, want := range map[int64]string{thousand: "1", 100: "0.001", 1000: "0.01", 49 * thousand: "49", 50: "0.0005", 1: "0.00001"} {
		if got := FormatPercent(FixedShareMicro(cents, thousand)); got != want {
			t.Errorf("%d cents: got %s want %s", cents, got, want)
		}
	}
	// The same $1,000 is 1% however much money arrives afterwards.
	if FixedShareMicro(thousand, thousand) != FixedShareMicro(thousand, thousand) || FixedShareMicro(0, thousand) != 0 || FixedShareMicro(100, 0) != 0 {
		t.Error("fixed share must not depend on anyone else or accept a zero price")
	}
	// A different price changes the rate: $500 for 1% doubles every holding.
	if got := FormatPercent(FixedShareMicro(thousand, 50_000)); got != "2" {
		t.Errorf("$1,000 at $500 per percent: %s", got)
	}
}
