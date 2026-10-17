package payments

import (
	"strings"
	"testing"
	"time"
)

func TestVerifySignature(t *testing.T) {
	const secret = "whsec_test_secret"
	now := time.Unix(1_800_000_000, 0)
	payload := []byte(`{"id":"evt_1","type":"checkout.session.completed"}`)
	good := SignPayload(payload, secret, now)

	cases := []struct {
		name    string
		payload []byte
		header  string
		secret  string
		at      time.Time
		ok      bool
	}{
		{"valid", payload, good, secret, now, true},
		{"valid within tolerance", payload, good, secret, now.Add(4 * time.Minute), true},
		{"wrong secret", payload, good, "whsec_other", now, false},
		{"tampered payload", []byte(`{"id":"evt_1","type":"x"}`), good, secret, now, false},
		{"stale timestamp", payload, good, secret, now.Add(6 * time.Minute), false},
		{"future timestamp", payload, good, secret, now.Add(-6 * time.Minute), false},
		{"empty secret never verifies", payload, SignPayload(payload, "", now), "", now, false},
		{"empty header", payload, "", secret, now, false},
		{"garbage header", payload, "hello", secret, now, false},
		{"no v1", payload, "t=1800000000", secret, now, false},
		{"bad hex", payload, "t=1800000000,v1=zzzz", secret, now, false},
		{"non-numeric t", payload, "t=abc," + good[strings.Index(good, "v1="):], secret, now, false},
		{"one good among several v1", payload, good + ",v1=deadbeef", secret, now, true},
	}
	for _, c := range cases {
		err := VerifySignature(c.payload, c.header, c.secret, c.at)
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestCatalogRespectsSpecRange(t *testing.T) {
	min, max := int64(1<<62), int64(0)
	for _, p := range Packs() {
		if p.Cents < min {
			min = p.Cents
		}
		if p.Cents > max {
			max = p.Cents
		}
		if p.Coins <= 0 {
			t.Errorf("pack %s grants nothing", p.ID)
		}
	}
	if min != 99 || max != 9999 {
		t.Fatalf("coin packs must span $0.99-$99.99, got %d-%d cents", min, max)
	}
}
