package signal

import "testing"

func TestTopicFor(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	cases := map[string]string{
		"/live/rooms/" + id + "/poll/vote":        "live:" + id,
		"/live/rooms/" + id + "/chat":             "live:" + id,
		"/live/rooms/" + id + "/join":             "",
		"/live/rooms":                             "",
		"/games/masked/rooms/" + id + "/finish":   "masked:" + id,
		"/games/party/" + id + "/pick":            "game:" + id,
		"/games/td/" + id + "/done":               "game:" + id,
		"/games/group/" + id + "/mlt/vote":        "game:" + id,
		"/eventgroups/" + id + "/messages":        "event:" + id,
		"/eventgroups/messages/" + id + "/report": "",
		"/chat/matches/" + id + "/messages":       "",
	}
	for path, want := range cases {
		if got := TopicFor(path); got != want {
			t.Errorf("%s: got %q want %q", path, got, want)
		}
	}
}
