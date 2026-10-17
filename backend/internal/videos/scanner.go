package videos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Verdict is a scanner's decision about one clip.
type Verdict struct {
	Action string `json:"action"` // allow | flag | block
	Reason string `json:"reason"`
}

// Scanner checks video bytes before they are stored or served. A real
// deployment plugs in a provider that hashes against known-abuse databases
// and classifies content; the contract here is deliberately small.
type Scanner interface {
	Name() string
	Scan(ctx context.Context, data []byte, mime string) (Verdict, error)
}

// StubScanner allows everything. It exists for development and is refused in
// production by the configuration check.
type StubScanner struct{}

func (StubScanner) Name() string { return "stub" }
func (StubScanner) Scan(context.Context, []byte, string) (Verdict, error) {
	return Verdict{Action: "allow"}, nil
}

// HTTPScanner posts the raw clip to a provider endpoint and expects
// {"action":"allow|flag|block","reason":"..."} back.
type HTTPScanner struct {
	URL, Key string
	Client   *http.Client
}

func (HTTPScanner) Name() string { return "http" }

func (h HTTPScanner) Scan(ctx context.Context, data []byte, mime string) (Verdict, error) {
	c := h.Client
	if c == nil {
		c = &http.Client{Timeout: 60 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(data))
	if err != nil {
		return Verdict{}, err
	}
	req.Header.Set("Content-Type", mime)
	if h.Key != "" {
		req.Header.Set("Authorization", "Bearer "+h.Key)
	}
	resp, err := c.Do(req)
	if err != nil {
		return Verdict{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Verdict{}, fmt.Errorf("scanner answered %s", resp.Status)
	}
	var v Verdict
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&v); err != nil {
		return Verdict{}, err
	}
	switch v.Action {
	case "allow", "flag", "block":
		if len(v.Reason) > 200 {
			v.Reason = v.Reason[:200]
		}
		return v, nil
	}
	return Verdict{}, errors.New("scanner returned an unknown action")
}
