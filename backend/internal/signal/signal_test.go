package signal

import "testing"

func TestTopicFor(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	cases := map[string]string{
		"/games/masked/rooms/" + id + "/finish":   "masked:" + id,
		"/games/party/" + id + "/pick":            "game:" + id,
		"/games/td/" + id + "/done":               "game:" + id,
		"/games/group/" + id + "/mlt/vote":        "game:" + id,
		"/chat/matches/" + id + "/messages":       "",
	}
	for path, want := range cases {
		if got := TopicFor(path); got != want {
			t.Errorf("%s: got %q want %q", path, got, want)
		}
	}
}
