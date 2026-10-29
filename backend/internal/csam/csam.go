// Package csam is the gate every uploaded image passes before it is stored or
// shown. A scanner checks the image against known child-abuse material; when it
// cannot give an answer the upload is refused (fail closed), and a match blocks
// the upload, records the hit and suspends the account.
//
// End-to-end encrypted chat media is ciphertext and cannot be scanned here;
// see docs/CSAM_RUNBOOK.md.
package csam

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ScanMeta says where an image came from. It is recorded with a hit.
type ScanMeta struct {
	UserID string
	Source string // profile_photo | id_check
}

// Verdict is a scanner's answer. Reference is the provider's own id for the
// match (a case or request id), kept with the hit record.
type Verdict struct {
	Match     bool
	Reference string
}

// Scanner checks one image. It MUST return an error, never a clean verdict,
// when it could not run: callers refuse the upload on any error.
type Scanner interface {
	Scan(ctx context.Context, image []byte, meta ScanMeta) (Verdict, error)
}

// ErrUnavailable means the scanner could not give an answer (not configured,
// not implemented, timed out, provider outage). Uploads are refused.
var ErrUnavailable = errors.New("csam scanner unavailable")

// Stub accepts everything. Development and tests only: New refuses it in
// production and config refuses to start with it.
type Stub struct{}

func (Stub) Scan(context.Context, []byte, ScanMeta) (Verdict, error) { return Verdict{}, nil }

// New builds the scanner named by CSAM_PROVIDER. "stub" is only allowed when
// production is false. Any other name selects a provider adapter; none is
// implemented yet (see docs/CSAM_RUNBOOK.md), so those refuse every upload.
func New(provider string, production bool) (Scanner, error) {
	switch p := strings.ToLower(strings.TrimSpace(provider)); p {
	case "", "stub":
		if production {
			return nil, errors.New("CSAM_PROVIDER=stub is not allowed in production")
		}
		return Stub{}, nil
	case "photodna":
		return &PhotoDNA{}, nil
	default:
		return nil, fmt.Errorf("unknown CSAM_PROVIDER %q", provider)
	}
}
