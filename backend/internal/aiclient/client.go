// Package aiclient talks to the Python AI service (scoring, moderation,
// wingman). The service is an enhancement, never a dependency: every call has
// a short timeout, and after repeated failures a circuit breaker stops
// calling it for a while so user requests are not slowed down.
package aiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"connectme/pkg/metrics"
)

var ErrUnavailable = errors.New("ai service unavailable")

const (
	failureThreshold = 5
	openFor          = 30 * time.Second
	defaultTextTimeout  = 2 * time.Second
	defaultImageTimeout = 6 * time.Second
	maxResponse      = 1 << 20
)

type Client struct {
	base string
	key  string
	http *http.Client
	now  func() time.Time

	textTimeout  time.Duration
	imageTimeout time.Duration

	mu        sync.Mutex
	fails     int
	openUntil time.Time
}

func New(baseURL, key string) *Client {
	return &Client{base: baseURL, key: key, http: &http.Client{}, now: time.Now,
		textTimeout: defaultTextTimeout, imageTimeout: defaultImageTimeout}
}

// SetTimeouts overrides how long a text or image moderation call may take.
// Zero keeps the current value.
func (c *Client) SetTimeouts(text, image time.Duration) {
	if text > 0 {
		c.textTimeout = text
	}
	if image > 0 {
		c.imageTimeout = image
	}
}

type Person struct {
	Tags []string `json:"tags"`
	// Lifestyle: goal, kids, drinking, smoking, exercise (only what was answered).
	Lifestyle   map[string]string `json:"lifestyle,omitempty"`
	Age         *int              `json:"age,omitempty"`
	ActiveHours []int             `json:"active_hours"`
}

type History struct {
	AToB int `json:"a_to_b"`
	BToA int `json:"b_to_a"`
}

type ScoreRequest struct {
	A       Person  `json:"a"`
	B       Person  `json:"b"`
	History History `json:"history"`
}

type Reason struct {
	Code string  `json:"code"`
	Text string  `json:"text"`
	W    float64 `json:"weight"`
}

type ScoreResponse struct {
	Score   int      `json:"score"`
	Reasons []Reason `json:"reasons"`
}

type Verdict struct {
	Safe            bool     `json:"safe"`
	Action          string   `json:"action"` // allow | review | block
	Reason          string   `json:"reason"`
	Categories      []string `json:"categories"`
	ScreenedByModel bool     `json:"screened_by_model"`
}

func (c *Client) Score(ctx context.Context, r ScoreRequest) (*ScoreResponse, error) {
	var out ScoreResponse
	if err := c.postJSON(ctx, "/score", r, &out, c.textTimeout); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ModerateText(ctx context.Context, kind, text string) (*Verdict, error) {
	var out Verdict
	err := c.postJSON(ctx, "/moderate", map[string]string{"text": text, "kind": kind}, &out, c.textTimeout)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ModerateImage(ctx context.Context, data []byte) (*Verdict, error) {
	var out Verdict
	err := c.do(ctx, "/moderate/image", "application/octet-stream", data, &out, c.imageTimeout)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) postJSON(ctx context.Context, path string, in, out any, timeout time.Duration) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return c.do(ctx, path, "application/json", b, out, timeout)
}

// do performs one call, tracking failures for the circuit breaker.
func (c *Client) do(ctx context.Context, path, contentType string, body []byte, out any, timeout time.Duration) (err error) {
	kind := path[1:]
	if c.base == "" {
		return ErrUnavailable
	}
	if !c.allow() {
		metrics.AI(kind, "breaker_open")
		return ErrUnavailable
	}
	defer func() {
		if err != nil {
			metrics.AI(kind, "error")
		} else {
			metrics.AI(kind, "ok")
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	if c.key != "" {
		req.Header.Set("X-Internal-Key", c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.record(false)
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if resp.StatusCode != http.StatusOK {
		// 4xx means we sent something wrong (a bug), 5xx means the service is unhealthy.
		c.record(resp.StatusCode < 500)
		return fmt.Errorf("ai service: status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(data, out); err != nil {
		c.record(false)
		return fmt.Errorf("ai service: bad response: %w", err)
	}
	c.record(true)
	return nil
}

func (c *Client) allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.now().Before(c.openUntil)
}

func (c *Client) record(ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ok {
		c.fails = 0
		metrics.AIBreaker(false)
		return
	}
	c.fails++
	if c.fails >= failureThreshold {
		c.openUntil = c.now().Add(openFor)
		c.fails = 0
		metrics.AIBreaker(true)
	}
}
