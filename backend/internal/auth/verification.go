package auth

import "context"

// CSAMScanner checks an image against known-CSAM hash lists (PhotoDNA / NCMEC
// or equivalent). It MUST run before any image is stored or published.
type CSAMScanner interface {
	// Clean returns false if the image matches a known hash.
	Clean(ctx context.Context, image []byte) (bool, error)
}

// StubCSAMScanner accepts everything. Development only: config refuses to
// start in production with CSAM_PROVIDER=stub.
//
// ID verification is not here any more: the earlier stub "liveness" check
// verified anyone who uploaded two files. It is now the human-reviewed flow
// in internal/idcheck.
type StubCSAMScanner struct{}

func (StubCSAMScanner) Clean(context.Context, []byte) (bool, error) { return true, nil }
