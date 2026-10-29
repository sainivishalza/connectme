// Package livekit is the media-server boundary shared by calls and rooms:
// it mints short-lived LiveKit access tokens and calls the RoomService API.
// Media is carried by LiveKit (WebRTC SFU); callers decide who may join.
package livekit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Provider is the media-server boundary. Tests and local development use a
// Provider that signs tokens but never calls a server.
type Provider interface {
	// URL is the WebSocket URL clients connect to.
	URL() string
	// Token mints a LiveKit access token scoped to one room and one identity.
	Token(room, identity string, publish bool, ttl time.Duration) (string, error)
	// Close tears the room down; best effort.
	Close(ctx context.Context, room string) error
	// Remove disconnects one participant; best effort.
	Remove(ctx context.Context, room, identity string) error
}

// Client signs real LiveKit access tokens (HS256 with the API secret) and
// calls the RoomService API to close rooms.
type Client struct {
	WSURL, APIKey, APISecret string
	HTTP                     *http.Client
}

func (l Client) URL() string { return l.WSURL }

func (l Client) sign(claims jwt.MapClaims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(l.APISecret))
}

func (l Client) Token(room, identity string, publish bool, ttl time.Duration) (string, error) {
	now := time.Now()
	return l.sign(jwt.MapClaims{
		"iss": l.APIKey, "sub": identity, "nbf": now.Unix(), "exp": now.Add(ttl).Unix(),
		"video": map[string]any{
			"room": room, "roomJoin": true,
			"canPublish": publish, "canPublishData": false, "canSubscribe": true,
		},
	})
}

func (l Client) Close(ctx context.Context, room string) error {
	err := l.twirp(ctx, "DeleteRoom", map[string]string{"room": room}, room)
	if err != nil && strings.Contains(err.Error(), "404") {
		return nil // LiveKit creates rooms when the first person connects; nobody came, so nothing to close
	}
	return err
}

// SetPublish lets or stops one participant publishing audio/video (used by
// audio rooms when the host promotes or demotes a speaker); best effort.
func (l Client) SetPublish(ctx context.Context, room, identity string, can bool) error {
	return l.twirp(ctx, "UpdateParticipant", map[string]any{
		"room": room, "identity": identity,
		"permission": map[string]any{"can_subscribe": true, "can_publish": can, "can_publish_data": false},
	}, room)
}

func (l Client) Remove(ctx context.Context, room, identity string) error {
	return l.twirp(ctx, "RemoveParticipant", map[string]string{"room": room, "identity": identity}, room)
}

func (l Client) twirp(ctx context.Context, method string, payload any, room string) error {
	tok, err := l.sign(jwt.MapClaims{
		"iss": l.APIKey, "exp": time.Now().Add(time.Minute).Unix(),
		"video": map[string]any{"roomCreate": true, "roomAdmin": true, "room": room},
	})
	if err != nil {
		return err
	}
	body, _ := json.Marshal(payload)
	base := strings.NewReplacer("wss://", "https://", "ws://", "http://").Replace(l.WSURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(base, "/")+"/twirp/livekit.RoomService/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	c := l.HTTP
	if c == nil {
		c = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("livekit %s: %s", method, resp.Status)
	}
	return nil
}
