package integration

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestBlockedMembersHideGroups(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	a1, _, ga := e.double(lat, lng, "dinner")
	b1, _, gb := e.double(lat+0.003, lng, "dinner")
	if _, body := e.call("GET", "/groups/browse?for_group="+ga+"&radius_km=25", nil, a1); !strings.Contains(string(body), gb) {
		t.Fatal("setup: group should be visible before the block")
	}
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(b1)}, a1)
	if _, body := e.call("GET", "/groups/browse?for_group="+ga+"&radius_km=25", nil, a1); strings.Contains(string(body), gb) {
		t.Fatal("a group containing a blocked person must be hidden")
	}
	if c, _ := e.groupMatch(ga, gb, a1); c != 404 {
		t.Fatalf("matching a group with a blocked member: %d", c)
	}
}

func TestGroupSuggestionsAndEncryptedKeyExchange(t *testing.T) {
	e := needChat(t)
	lat, lng := randomBase()
	a1, a2, ga := e.double(lat, lng, "hiking")
	b1, b2, gb := e.double(lat+0.002, lng, "hiking")
	for _, u := range []string{a1, a2, b1, b2} {
		e.call("PATCH", "/users/me", map[string]any{"tags": []string{"trail running", "coffee"}}, u)
	}

	// Suggestions: fallback works without any AI service and sends no PII.
	c, body := e.call("POST", "/groups/"+ga+"/venue", nil, a1)
	var sg struct {
		Venues, Icebreakers []string
		Source              string
	}
	decodeInto(t, body, &sg)
	if c != 200 || sg.Source != "fallback" || len(sg.Venues) == 0 || len(sg.Icebreakers) < 5 {
		t.Fatalf("suggestions: %d %s", c, body)
	}
	if c, _ := e.call("POST", "/groups/"+ga+"/venue", nil, b1); c != 404 {
		t.Fatalf("non-member suggestions: %d", c)
	}

	// Match everyone, then exercise group key distribution.
	e.groupMatch(ga, gb, a1)
	e.groupMatch(ga, gb, a2)
	e.groupMatch(gb, ga, b1)
	_, r := e.groupMatch(gb, ga, b2)
	if !r.Matched {
		t.Fatal("setup: expected group match")
	}
	gm := r.GroupMatchID

	mk := func(seed byte) string {
		b := make([]byte, 33)
		for i := range b {
			b[i] = seed
		}
		return base64.StdEncoding.EncodeToString(b)
	}
	for i, u := range []string{a1, a2, b1, b2} {
		c, _ := e.call("POST", "/chat/keys", map[string]any{
			"registration_id": 100 + i, "identity_key": mk(byte(1 + i)), "signed_prekey_id": 1,
			"signed_prekey": mk(byte(20 + i)), "signed_prekey_sig": mk(byte(40 + i)),
			"one_time_prekeys": []map[string]any{{"key_id": 1, "public_key": mk(byte(60 + i))}},
		}, u)
		if c != 204 {
			t.Fatalf("upload keys %d: %d", i, c)
		}
	}
	_, body = e.call("GET", "/chat/groups/"+gm+"/keys", nil, a1)
	var kb struct {
		Members []struct {
			MemberToken string `json:"member_token"`
			IdentityKey string `json:"identity_key"`
		}
	}
	decodeInto(t, body, &kb)
	if len(kb.Members) != 3 {
		t.Fatalf("a1 should get the other 3 members' bundles: %s", body)
	}
	if strings.Contains(string(body), a2) || strings.Contains(string(body), b1) {
		t.Fatal("user ids must not appear in group key responses")
	}
	if c, _ := e.call("GET", "/chat/groups/"+gm+"/keys", nil, e.user("Out")); c != 404 {
		t.Fatalf("outsider keys: %d", c)
	}

	// Envelope mailbox: a1 -> b2 (token from the bundle list), fetched once.
	_, body = e.call("GET", "/chat/groups/"+gm+"/keys", nil, b2)
	var kb2 struct {
		Members []struct {
			MemberToken string `json:"member_token"`
		}
	}
	decodeInto(t, body, &kb2)
	// b2 sees a1 under b2's own view of tokens; tokens are conversation-wide, so
	// a1's token for b2 is the entry in a1's list that b2 can match by identity.
	var b2Token string
	for _, m := range kb.Members {
		if m.IdentityKey == mk(4) { // b2 is the 4th uploader: seed 1+3
			b2Token = m.MemberToken
		}
	}
	if b2Token == "" {
		t.Fatal("b2's token not found")
	}
	blob := base64.StdEncoding.EncodeToString([]byte("sealed sender key"))
	if c, _ := e.call("POST", "/chat/groups/"+gm+"/envelopes", map[string]string{"to": b2Token, "ciphertext": blob}, a1); c != 204 {
		t.Fatalf("send envelope: %d", c)
	}
	if c, _ := e.call("POST", "/chat/groups/"+gm+"/envelopes", map[string]string{"to": "deadbeef", "ciphertext": blob}, a1); c != 404 {
		t.Fatalf("envelope to unknown member: %d", c)
	}
	_, body = e.call("GET", "/chat/groups/"+gm+"/envelopes", nil, b2)
	if !strings.Contains(string(body), blob) {
		t.Fatalf("envelope not delivered: %s", body)
	}
	if _, body = e.call("GET", "/chat/groups/"+gm+"/envelopes", nil, b2); strings.Contains(string(body), blob) {
		t.Fatal("envelope must be deleted after fetch")
	}
	if _, body = e.call("GET", "/chat/groups/"+gm+"/envelopes", nil, a2); strings.Contains(string(body), blob) {
		t.Fatal("envelope leaked to another member")
	}
}
