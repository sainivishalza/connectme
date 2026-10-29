package csam

import (
	"context"
	"errors"
	"testing"
)

func TestStubIsRefusedInProduction(t *testing.T) {
	for _, name := range []string{"", "stub", "STUB"} {
		if _, err := New(name, true); err == nil {
			t.Errorf("provider %q must not build in production", name)
		}
		if sc, err := New(name, false); err != nil || sc == nil {
			t.Errorf("provider %q should build in development: %v", name, err)
		}
	}
}

func TestUnknownProviderIsAnError(t *testing.T) {
	if _, err := New("nope", false); err == nil {
		t.Fatal("an unknown provider must be rejected")
	}
}

func TestSkeletonAdapterNeverGivesACleanAnswer(t *testing.T) {
	sc, err := New("photodna", true)
	if err != nil {
		t.Fatal(err)
	}
	v, err := sc.Scan(context.Background(), []byte("image"), ScanMeta{UserID: "u", Source: "profile_photo"})
	if !errors.Is(err, ErrUnavailable) || v.Match {
		t.Fatalf("the unimplemented adapter must fail closed, got %+v %v", v, err)
	}
}
