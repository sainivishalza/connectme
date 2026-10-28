package integration

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type matchVoice struct {
	MatchID string `json:"match_id"`
	Voice   *struct {
		ByMe     bool `json:"by_me"`
		Unlocked bool `json:"unlocked"`
		Seconds  int  `json:"seconds"`
	} `json:"voice"`
}

func (e *env) voiceStep(uid, matchID string) *matchVoice {
	e.t.Helper()
	_, b := e.call("GET", "/matching/matches", nil, uid)
	var out struct {
		Matches []matchVoice `json:"matches"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		e.t.Fatal(err)
	}
	for i := range out.Matches {
		if out.Matches[i].MatchID == matchID {
			return &out.Matches[i]
		}
	}
	e.t.Fatalf("match not listed: %s", b)
	return nil
}

func TestAnonymousMatchesUnlockVoiceFirstOnlyWhenBothAgree(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	a, b, mute := e.user("Voice A"), e.user("Voice B"), e.user("Voice Mute")
	for _, u := range []string{a, b, mute} {
		e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, u)
		e.locate(u, lat, lng)
	}
	clipA, clipB := mp4Clip(5), mp4Clip(6)
	e.putVoice(a, "9", clipA)
	e.putVoice(b, "14", clipB)

	e.swipe(a, e.refOf(b), "like")
	_, r := e.swipe(b, e.refOf(a), "like")
	if r.MatchID == "" {
		t.Fatalf("no match: %+v", r)
	}
	m := r.MatchID

	// Nothing before anyone agrees; no length, no audio.
	if st := e.voiceStep(a, m); st.Voice == nil || st.Voice.Unlocked || st.Voice.Seconds != 0 {
		t.Fatalf("initial: %+v", st.Voice)
	}
	if c, _ := e.call("GET", "/matching/matches/"+m+"/voice", nil, a); c != 404 {
		t.Errorf("voice before consent: %d", c)
	}

	// Needs your own voice intro to swap.
	e.swipe(mute, e.refOf(a), "like")
	_, r2 := e.swipe(a, e.refOf(mute), "like")
	if c, _ := e.call("POST", "/matching/matches/"+r2.MatchID+"/reveal-voice", nil, mute); c != 422 {
		t.Errorf("no voice intro: %d", c)
	}

	// One side agrees: still locked for both, and the answer does not say the other is waiting.
	c, body := e.call("POST", "/matching/matches/"+m+"/reveal-voice", nil, a)
	if c != 200 || strings.Contains(string(body), `"unlocked":true`) {
		t.Fatalf("first consent: %d %s", c, body)
	}
	if c, _ := e.call("GET", "/matching/matches/"+m+"/voice", nil, a); c != 404 {
		t.Errorf("voice after one consent: %d", c)
	}
	if st := e.voiceStep(b, m); st.Voice.Unlocked || st.Voice.ByMe {
		t.Errorf("other side sees %+v", st.Voice)
	}

	// Both agree: each hears the other's clip, with its length, and the card stays anonymous.
	if c, body := e.call("POST", "/matching/matches/"+m+"/reveal-voice", nil, b); c != 200 || !strings.Contains(string(body), `"unlocked":true`) {
		t.Fatalf("second consent: %d %s", c, body)
	}
	if c, got := e.call("GET", "/matching/matches/"+m+"/voice", nil, a); c != 200 || !bytes.Equal(got, clipB) {
		t.Errorf("a hears b: %d", c)
	}
	if c, got := e.call("GET", "/matching/matches/"+m+"/voice", nil, b); c != 200 || !bytes.Equal(got, clipA) {
		t.Errorf("b hears a: %d", c)
	}
	if st := e.voiceStep(a, m); !st.Voice.Unlocked || st.Voice.Seconds != 14 {
		t.Errorf("a's view: %+v", st.Voice)
	}
	_, list := e.call("GET", "/matching/matches", nil, a)
	if strings.Contains(string(list), `"voice_intro_s"`) || strings.Contains(string(list), `"display_name":"Voice B"`) {
		t.Errorf("the voice step must not unmask the card: %s", list)
	}

	// Outsiders and unmatching.
	if c, _ := e.call("GET", "/matching/matches/"+m+"/voice", nil, mute); c != 404 {
		t.Errorf("outsider: %d", c)
	}
	e.call("DELETE", "/matching/matches/"+m, nil, a)
	if c, _ := e.call("GET", "/matching/matches/"+m+"/voice", nil, b); c != 404 {
		t.Errorf("after unmatch: %d", c)
	}
}
