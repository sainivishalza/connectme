// Package push delivers notifications to members' phones through a provider
// (Firebase Cloud Messaging, which also reaches iOS via APNs).
//
// Privacy rules, because a notification lands on a lock screen:
//   - payloads are generic ("You have a new message"): never a name, a photo,
//     message text or a venue, so nothing end-to-end encrypted or anonymous can
//     leak through the provider or onto the screen;
//   - the only data attached is the kind and an opaque id (a match or call id)
//     so the app can open the right screen after it authenticates;
//   - members choose which kinds they receive, and banned or unverified
//     accounts receive nothing.
package push

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// Message is one notification for one device.
type Message struct {
	Title      string
	Body       string
	Data       map[string]string
	HighPrio   bool   // wake the device now (calls)
	TTLSeconds int    // drop it if it cannot be delivered within this time
	Collapse   string // a newer message with the same key replaces an older one
}

// ErrUnregistered means the provider says the token is no longer valid; the
// caller deletes it.
var ErrUnregistered = errors.New("push token is no longer registered")

// Provider sends a notification to one device token.
type Provider interface {
	Name() string
	Send(ctx context.Context, platform, token string, m Message) error
}

// Stub records what would have been sent. It is the default in development and
// in tests, and in production simply means push is off.
type Stub struct {
	mu   sync.Mutex
	Sent []Sent
	Bad  map[string]bool // tokens that answer ErrUnregistered
}

type Sent struct {
	Platform, Token string
	Msg             Message
}

func (*Stub) Name() string { return "stub" }

func (s *Stub) Send(_ context.Context, platform, token string, m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Bad[token] {
		return ErrUnregistered
	}
	s.Sent = append(s.Sent, Sent{platform, token, m})
	slog.Debug("push: stub send", "platform", platform, "title", m.Title)
	return nil
}

// Taken returns and clears what the stub has recorded.
func (s *Stub) Taken() []Sent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Sent
	s.Sent = nil
	return out
}
