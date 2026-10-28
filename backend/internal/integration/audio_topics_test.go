package integration

import (
	"strings"
	"testing"
)

func TestVoiceRoomsHaveTopicsAndAnonymousHostsStayAnonymous(t *testing.T) {
	e := setup(t)
	host, other, viewer := e.user("Topic Host"), e.user("Other Host"), e.user("Topic Viewer")
	if c, _ := e.call("POST", "/audio/rooms", map[string]any{"title": "Bad topic", "topic": "politics"}, host); c != 422 {
		t.Errorf("unknown topic: %d", c)
	}
	c, b := e.call("POST", "/audio/rooms", map[string]any{"title": "Midnight confessions", "topic": "confess", "anonymous": true}, host)
	if c != 201 || !strings.Contains(string(b), `"topic":"confess"`) {
		t.Fatalf("start: %d %s", c, b)
	}
	if c, _ := e.call("POST", "/audio/rooms", map[string]any{"title": "Default topic room"}, other); c != 201 {
		t.Fatalf("default start: %d", c)
	}
	_, b = e.call("GET", "/audio/rooms?topic=confess", nil, viewer)
	if !strings.Contains(string(b), "Midnight confessions") || strings.Contains(string(b), "Default topic room") {
		t.Errorf("topic filter: %s", b)
	}
	_, b = e.call("GET", "/audio/rooms?topic=open", nil, viewer)
	if strings.Contains(string(b), "Midnight confessions") || !strings.Contains(string(b), `"topic":"open"`) {
		t.Errorf("default topic: %s", b)
	}
	if c, _ := e.call("GET", "/audio/rooms?topic=nonsense", nil, viewer); c != 422 {
		t.Errorf("bad filter: %d", c)
	}
	if strings.Contains(string(b), "Topic Host") {
		t.Errorf("anonymous host name leaked in list: %s", b)
	}
}
