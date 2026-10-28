// Package errtrack reports unexpected server errors to a Sentry-compatible
// endpoint (Sentry itself, GlitchTip, or anything speaking the envelope API).
//
// Privacy first: an event carries the error text, a stack trace of our own
// code, the route pattern and an environment/release tag, and nothing about
// the request or the person: no URL, query string, headers, body, IP, user id
// or cookies. Error text is scrubbed of emails, tokens and long digit runs
// before it leaves the process (database errors can echo values). With no DSN
// configured nothing is sent and nothing is queued.
//
// It is also built so it cannot hurt the API: sending is asynchronous with a
// small bounded queue (a full queue drops events), identical errors are sent
// at most once a minute, and the whole process sends at most 30 a minute.
package errtrack

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Reporter struct {
	endpoint, key, env, release string
	client                      *http.Client
	queue                       chan []byte
	wg                          sync.WaitGroup

	mu       sync.Mutex
	closed   bool
	seen     map[string]time.Time
	sentAt   []time.Time
	now      func() time.Time
	maxPerMn int
}

var def *Reporter

// Init configures the process-wide reporter. An empty DSN disables reporting.
// A malformed DSN is an error so a typo is noticed at startup, not at 3am.
func Init(dsn, env, release string) (*Reporter, error) {
	if dsn == "" {
		def = nil
		return nil, nil
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User == nil || u.User.Username() == "" || u.Host == "" {
		return nil, errors.New("errtrack: SENTRY_DSN must look like https://<public-key>@<host>/<project-id>")
	}
	project := strings.Trim(u.Path, "/")
	if project == "" || strings.Contains(project, "/") {
		return nil, errors.New("errtrack: SENTRY_DSN is missing its project id")
	}
	r := &Reporter{
		endpoint: fmt.Sprintf("%s://%s/api/%s/envelope/", u.Scheme, u.Host, project),
		key:      u.User.Username(), env: env, release: release,
		client:   &http.Client{Timeout: 5 * time.Second},
		queue:    make(chan []byte, 50),
		seen:     map[string]time.Time{},
		now:      time.Now,
		maxPerMn: 30,
	}
	r.wg.Add(1)
	go r.loop()
	def = r
	return r, nil
}

func (r *Reporter) loop() {
	defer r.wg.Done()
	for body := range r.queue {
		req, err := http.NewRequest(http.MethodPost, r.endpoint, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/x-sentry-envelope")
		req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_client=connectme-go/1, sentry_key="+r.key)
		if resp, err := r.client.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}
}

// Flush stops accepting events and waits (up to d) for the queue to drain.
func (r *Reporter) Flush(d time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	ch := make(chan struct{})
	go func() { r.wg.Wait(); close(ch) }()
	select {
	case <-ch:
	case <-time.After(d):
	}
}

var (
	reEmail  = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	reSecret = regexp.MustCompile(`(?i)(bearer|token|secret|password|authorization)[=: ]+\S+`)
	reDigits = regexp.MustCompile(`\d{6,}`)
	reQuery  = regexp.MustCompile(`\?[^\s"']+`)
)

// Scrub removes the kinds of value an error message can accidentally carry.
func Scrub(s string) string {
	s = reEmail.ReplaceAllString(s, "[email]")
	s = reSecret.ReplaceAllString(s, "$1=[redacted]")
	s = reQuery.ReplaceAllString(s, "?[query]")
	s = reDigits.ReplaceAllString(s, "[number]")
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}

type frame struct {
	Function string `json:"function"`
	Filename string `json:"filename"`
	Lineno   int    `json:"lineno"`
}

// stack returns our own frames only (no third-party code, no build-machine
// paths), oldest first as Sentry expects.
func stack(skip int) []frame {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(skip, pcs)
	fr := runtime.CallersFrames(pcs[:n])
	var out []frame
	for {
		f, more := fr.Next()
		file := strings.ReplaceAll(f.File, `\`, "/")
		if i := strings.Index(file, "/backend/"); i >= 0 {
			out = append(out, frame{f.Function, file[i+len("/backend/"):], f.Line})
		}
		if !more {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Capture reports an error. route is a route pattern, never a real URL.
func Capture(err error, route string) { def.capture(err, route, "error", 3) }

func (r *Reporter) capture(err error, route, level string, skip int) {
	if r == nil || err == nil {
		return
	}
	msg := Scrub(err.Error())
	typ := fmt.Sprintf("%T", err)
	frames := stack(skip)
	fp := typ + "|" + route + "|" + msg
	if len(frames) > 0 {
		last := frames[len(frames)-1]
		fp += fmt.Sprintf("|%s:%d", last.Filename, last.Lineno)
	}
	var id [16]byte
	_, _ = rand.Read(id[:])
	eid := hex.EncodeToString(id[:])
	ts := r.now().UTC().Format(time.RFC3339)
	ev := map[string]any{
		"event_id": eid, "timestamp": ts, "platform": "go",
		"level": level, "environment": r.env, "release": r.release, "server_name": "connectme-api",
		"logger": "connectme",
		"exception": map[string]any{"values": []any{map[string]any{
			"type": typ, "value": msg, "stacktrace": map[string]any{"frames": frames}}}},
		"tags": map[string]string{"route": route},
	}
	payload, _ := json.Marshal(ev)
	hdr, _ := json.Marshal(map[string]string{"event_id": eid, "sent_at": ts})
	item, _ := json.Marshal(map[string]any{"type": "event", "length": len(payload)})
	body := append(append(append(append(hdr, '\n'), item...), '\n'), payload...)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || !r.allowLocked(fp) {
		return
	}
	select {
	case r.queue <- body:
	default: // queue full: drop rather than slow the API
	}
}

// allowLocked dedupes identical errors for a minute and caps the global rate.
func (r *Reporter) allowLocked(fp string) bool {
	now := r.now()
	if t, dup := r.seen[fp]; dup && now.Sub(t) < time.Minute {
		return false
	}
	if len(r.seen) > 500 {
		r.seen = map[string]time.Time{}
	}
	keep := r.sentAt[:0]
	for _, t := range r.sentAt {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	r.sentAt = keep
	if len(r.sentAt) >= r.maxPerMn {
		return false
	}
	r.seen[fp] = now
	r.sentAt = append(r.sentAt, now)
	return true
}
