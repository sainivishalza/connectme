package live

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Opt-in: run a real server first, e.g.
//
//	docker run --rm -p 7880:7880 -e LIVEKIT_KEYS="devkey: <32+ byte secret>" livekit/livekit-server --bind 0.0.0.0
//	TEST_LIVEKIT_URL=ws://127.0.0.1:7880 TEST_LIVEKIT_SECRET=<secret> go test ./internal/live -run Real
func realProvider(t *testing.T) LiveKit {
	t.Helper()
	url, secret := os.Getenv("TEST_LIVEKIT_URL"), os.Getenv("TEST_LIVEKIT_SECRET")
	if url == "" || secret == "" {
		t.Skip("TEST_LIVEKIT_URL / TEST_LIVEKIT_SECRET not set")
	}
	return LiveKit{WSURL: url, APIKey: "devkey", APISecret: secret}
}

func twirp(t *testing.T, l LiveKit, method string, body any, grant map[string]any) (int, string) {
	t.Helper()
	tok, err := l.sign(jwt.MapClaims{"iss": l.APIKey, "exp": time.Now().Add(time.Minute).Unix(), "video": grant})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body)
	base := strings.Replace(l.WSURL, "ws://", "http://", 1)
	req, _ := http.NewRequest("POST", base+"/twirp/livekit.RoomService/"+method, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	out.ReadFrom(resp.Body)
	return resp.StatusCode, out.String()
}

func TestRealLiveKitAcceptsOurTokensAndCloseDeletesTheRoom(t *testing.T) {
	l := realProvider(t)
	room := "itest-" + time.Now().Format("150405.000")

	if c, b := twirp(t, l, "CreateRoom", map[string]string{"name": room}, map[string]any{"roomCreate": true}); c != 200 {
		t.Fatalf("CreateRoom: %d %s", c, b)
	}
	listed := func() bool {
		c, b := twirp(t, l, "ListRooms", map[string]any{}, map[string]any{"roomList": true})
		if c != 200 {
			t.Fatalf("ListRooms: %d %s", c, b)
		}
		return strings.Contains(b, room)
	}
	if !listed() {
		t.Fatal("room not created")
	}

	// A viewer token is accepted by the signalling endpoint (101 = upgraded).
	tok, err := l.Token(room, "viewer-1", false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got := wsStatus(t, l.WSURL, tok); got != http.StatusSwitchingProtocols {
		t.Errorf("real server rejected our token: %d", got)
	}
	bad, _ := l.Token(room, "viewer-1", false, time.Minute)
	if got := wsStatus(t, l.WSURL, bad+"x"); got == http.StatusSwitchingProtocols {
		t.Error("server accepted a corrupted token")
	}

	// RemoveParticipant must be authorised by the server. The identity is not
	// connected, so a "not found" answer proves auth and routing worked, while
	// 401/403 would mean our admin token is wrong.
	err = l.Remove(context.Background(), room, "nobody-here")
	if err != nil && !strings.Contains(err.Error(), "404") {
		t.Errorf("Remove: %v", err)
	}

	if err := l.Close(context.Background(), room); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if listed() {
		t.Error("room still exists after Close")
	}
}

// wsStatus performs the WebSocket handshake by hand and returns the status.
func wsStatus(t *testing.T, wsURL, token string) int {
	t.Helper()
	base := strings.Replace(wsURL, "ws://", "http://", 1)
	req, _ := http.NewRequest("GET", base+"/rtc?protocol=15&sdk=go&access_token="+token, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
