package integration

import (
	"bytes"
	"strings"
	"testing"
)

// minimal containers: just enough header for the server's format check
func mp4Clip(tag byte) []byte {
	return append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'M', '4', 'A', ' ', 0, 0, 0, 0}, bytes.Repeat([]byte{tag}, 400)...)
}

func (e *env) putVoice(uid string, secs string, body []byte) int {
	e.t.Helper()
	c, _ := e.raw("PUT", "/users/me/voice-intro?seconds="+secs, body, uid)
	return c
}

func (e *env) voiceOf(viewer, ref string) (int, []byte) {
	e.t.Helper()
	return e.call("GET", "/media/voice/"+ref, nil, viewer)
}

func TestVoiceIntroValidationAndRoundTrip(t *testing.T) {
	e := setup(t)
	owner, fan := e.user("Singer"), e.user("Listener")
	clip := mp4Clip(7)

	for name, c := range map[string]struct {
		secs string
		body []byte
		want int
	}{
		"not audio":     {"10", []byte("this is not audio at all, just text"), 422},
		"too short":     {"0", clip, 422},
		"too long":      {"31", clip, 422},
		"no seconds":    {"", clip, 422},
		"empty":         {"10", nil, 422},
		"oversized":     {"10", append(mp4Clip(1), bytes.Repeat([]byte{1}, 1<<20)...), 422},
		"webm accepted": {"5", append([]byte{0x1A, 0x45, 0xDF, 0xA3}, bytes.Repeat([]byte{2}, 300)...), 204},
		"ogg accepted":  {"5", append([]byte("OggS"), bytes.Repeat([]byte{3}, 300)...), 204},
	} {
		if got := e.putVoice(e.user("Tester"), c.secs, c.body); got != c.want { // a fresh user each: uploads are rate limited
			t.Errorf("%s: got %d, want %d", name, got, c.want)
		}
	}
	if got := e.putVoice(owner, "12", clip); got != 204 {
		t.Fatalf("upload: %d", got)
	}
	// Open-mode owner: a stranger can hear it and sees its length on the profile.
	c, got := e.voiceOf(fan, e.refOf(owner))
	if c != 200 || !bytes.Equal(got, clip) {
		t.Fatalf("listener got %d, %d bytes", c, len(got))
	}
	_, prof := e.call("GET", "/users/"+e.refOf(owner), nil, fan)
	if !strings.Contains(string(prof), `"voice_intro_s":12`) {
		t.Errorf("profile does not show the voice intro length: %s", prof)
	}
	_, me := e.call("GET", "/users/me", nil, owner)
	if !strings.Contains(string(me), `"voice_intro_s":12`) {
		t.Errorf("own profile: %s", me)
	}

	// Replacing keeps one clip; deleting removes it for everyone.
	newer := mp4Clip(9)
	if got := e.putVoice(owner, "8", newer); got != 204 {
		t.Fatalf("replace: %d", got)
	}
	if _, got := e.voiceOf(fan, e.refOf(owner)); !bytes.Equal(got, newer) {
		t.Error("listener still hears the old clip")
	}
	if c, _ := e.call("DELETE", "/users/me/voice-intro", nil, owner); c != 204 {
		t.Fatalf("delete: %d", c)
	}
	if c, _ := e.voiceOf(fan, e.refOf(owner)); c != 404 {
		t.Errorf("clip still served after delete: %d", c)
	}
	if c, _ := e.call("DELETE", "/users/me/voice-intro", nil, owner); c != 204 {
		t.Errorf("deleting twice should be fine: %d", c)
	}
}

func TestVoiceIntroNeverReachesPeopleWhoCannotSeeTheFullProfile(t *testing.T) {
	e := setup(t)
	owner, fan, blocked := e.user("Quiet"), e.user("Fan"), e.user("Blocked")
	e.putVoice(owner, "10", mp4Clip(5))
	ref := e.refOf(owner)

	// Incognito: not served, and the profile does not even mention it.
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id=$1`, owner)
	ref = e.refOf(owner) // the ref rotates with a mode change in the app; read it fresh
	if c, _ := e.voiceOf(fan, ref); c != 404 {
		t.Errorf("incognito voice served: %d", c)
	}
	_, prof := e.call("GET", "/users/"+ref, nil, fan)
	if strings.Contains(string(prof), "voice_intro") {
		t.Errorf("anonymous profile mentions a voice intro: %s", prof)
	}

	// Back to open: audible, except to someone the owner blocked.
	e.exec(`UPDATE profiles SET mode='open' WHERE user_id=$1`, owner)
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(blocked)}, owner)
	if c, _ := e.voiceOf(fan, e.refOf(owner)); c != 200 {
		t.Errorf("open profile voice: %d", c)
	}
	if c, _ := e.voiceOf(blocked, e.refOf(owner)); c != 404 {
		t.Errorf("blocked person heard the clip: %d", c)
	}
	// A banned owner disappears entirely.
	e.exec(`UPDATE users SET is_banned=true WHERE id=$1`, owner)
	if c, _ := e.voiceOf(fan, e.refOf(owner)); c != 404 {
		t.Errorf("banned owner's voice served: %d", c)
	}
	// Garbage refs.
	if c, _ := e.voiceOf(fan, "not-a-uuid"); c != 404 {
		t.Errorf("bad ref: %d", c)
	}
}

func TestVoiceIntroInAnIncognitoMatchNeedsMutualReveal(t *testing.T) {
	e := setup(t)
	a, b, m := e.matched()
	e.putVoice(b, "10", mp4Clip(4))
	e.exec(`UPDATE matches SET mode='incognito' WHERE id=$1::uuid`, m)
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id IN ($1,$2)`, a, b)
	ref := e.refOf(b)

	if c, _ := e.voiceOf(a, ref); c != 404 {
		t.Errorf("heard an anonymous match's voice before reveal: %d", c)
	}
	e.exec(`INSERT INTO reveal_consents (match_id, user_id) VALUES ($1::uuid,$2)`, m, a)
	if c, _ := e.voiceOf(a, ref); c != 404 {
		t.Errorf("one-sided reveal unlocked the voice: %d", c)
	}
	e.exec(`INSERT INTO reveal_consents (match_id, user_id) VALUES ($1::uuid,$2)`, m, b)
	if c, _ := e.voiceOf(a, ref); c != 200 {
		t.Errorf("mutual reveal should unlock the voice: %d", c)
	}
}

func TestErasureDeletesTheVoiceFile(t *testing.T) {
	e := setup(t)
	uid, _ := e.userWithPassword("Erasing")
	if got := e.putVoice(uid, "10", mp4Clip(6)); got != 204 {
		t.Fatalf("upload: %d", got)
	}
	var voiceID string
	if err := e.db.QueryRow(t.Context(), `SELECT voice_id::text FROM profiles WHERE user_id=$1`, uid).Scan(&voiceID); err != nil {
		t.Fatal(err)
	}
	if !e.photoStored(voiceID) {
		t.Fatal("voice file not stored")
	}
	if c := e.erase(uid, erasePW, "DELETE"); c != 204 {
		t.Fatalf("erase: %d", c)
	}
	if e.photoStored(voiceID) {
		t.Error("the voice recording survived account erasure")
	}
}
