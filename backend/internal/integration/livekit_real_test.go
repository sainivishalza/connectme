package integration

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Opt-in: needs a real LiveKit server and `npm install` in ops/dev/livekit-check.
//
//	docker run -d -p 7880:7880 -p 7881:7881 -p 50000-50100:50000-50100/udp -e LIVEKIT_KEYS="devkey: <32+ byte secret>" livekit/livekit-server --bind 0.0.0.0
//	TEST_LIVEKIT_URL=ws://127.0.0.1:7880 TEST_LIVEKIT_SECRET=<secret> go test ./internal/integration -run RealLiveKit -v
//
// It starts an audio room through the API, joins as the host and as a
// listener with the tokens the API issued, and proves audio really reaches the
// listener and that the listener cannot publish.
func TestRealLiveKitAudioRoomCarriesMedia(t *testing.T) {
	url := os.Getenv("TEST_LIVEKIT_URL")
	if url == "" || os.Getenv("TEST_LIVEKIT_SECRET") == "" {
		t.Skip("TEST_LIVEKIT_URL / TEST_LIVEKIT_SECRET not set")
	}
	e := setup(t)
	host, guest := e.user("Real Host"), e.user("Real Guest")
	room := e.openAudio(host, "Media check", false)
	c, g := e.audioJoin(guest, room.Room.ID)
	if c != 200 || g.CanPublish {
		t.Fatalf("listener join: %d %+v", c, g)
	}
	cmd := exec.Command("node", "check.mjs", url, room.Connect.Token, g.Token)
	cmd.Dir = "../../../ops/dev/livekit-check"
	done := make(chan struct{})
	var out []byte
	var err error
	go func() { out, err = cmd.CombinedOutput(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("media check hung")
	}
	if err != nil || !strings.Contains(string(out), "OK:") {
		t.Fatalf("media check failed: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}
