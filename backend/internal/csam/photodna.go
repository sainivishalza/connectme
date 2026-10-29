package csam

import "context"

// PhotoDNA is a placeholder for the real provider adapter. The provider has
// not been chosen yet (PhotoDNA, Google Content Safety API, Thorn Safer or
// Cloudflare), and each needs an application or contract. Until an adapter is
// written it answers ErrUnavailable, so every upload is refused rather than
// waved through. Replace Scan with the provider's call and add the provider
// name to config's implemented list.
type PhotoDNA struct {
	Endpoint string
	Key      string
}

func (*PhotoDNA) Scan(context.Context, []byte, ScanMeta) (Verdict, error) {
	return Verdict{}, ErrUnavailable
}
