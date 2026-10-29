package integration

import (
	"time"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// bolaCase is one route that takes the id of somebody else's object. The same
// request is made by a stranger, by a member the owner blocked, and by nobody;
// none of them may learn anything, and the answer must not confirm that the
// object exists (404, not 403).
type bolaCase struct {
	name, method, path string // path uses {match} {call} {group} {post} {contact} {photo}
	body               any
	needsChat          bool
}

func (e *env) bolaFixtures(t *testing.T) (owner, partner, stranger, blocked string, ids map[string]string) {
	t.Helper()
	ids = map[string]string{}
	lat, lng := randomBase()

	// A match between owner and partner.
	owner, partner, ids["match"] = e.matched()
	e.locate(owner, lat, lng)
	e.locate(partner, lat+0.001, lng)

	stranger = e.located("Stranger", lat+50, lng) // far away, no relation
	blocked = e.located("Blocked", lat+0.002, lng)
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(blocked)}, owner)

	// A call between owner and partner.
	if c, call := e.startCall(owner, ids["match"], false); c == 201 {
		ids["call"] = call.Call.ID
	}
	// A group owned by the owner.
	if c, g := e.newGroup(owner, "double", "dinner", 0); c == 201 {
		ids["group"] = g.ID
	}
	// A nearby moment by the owner.
	if c, p := e.post(owner, "moment", "Coffee near the station?"); c == 201 {
		ids["post"] = p.ID
	}
	// A trusted contact of the owner.
	if c, b := e.call("POST", "/safety/contacts", map[string]string{"name": "Gran", "email": fmt.Sprintf("gran%d@example.org", len(ids))}, owner); c == 201 {
		var ct struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(b, &ct)
		ids["contact"] = ct.ID
	}
	// A photo of the owner, who is incognito, so a stranger may not fetch it.
	if c := uploadPhoto(e, owner, jpegBytesFor(t)); c == 201 {
		var me struct {
			Photos []struct {
				ID string `json:"id"`
			} `json:"photos"`
		}
		_, b := e.call("GET", "/users/me", nil, owner)
		_ = json.Unmarshal(b, &me)
		if len(me.Photos) > 0 {
			ids["photo"] = me.Photos[0].ID
		}
	}
	e.call("POST", "/users/me/mode", map[string]string{"mode": "incognito"}, owner)
	return owner, partner, stranger, blocked, ids
}

func TestCrossUserAccessIsRefused(t *testing.T) {
	e := needChat(t)
	owner, _, stranger, blocked, ids := e.bolaFixtures(t)
	for _, k := range []string{"match", "call", "group", "post", "contact", "photo"} {
		if ids[k] == "" {
			t.Fatalf("fixture %q could not be created", k)
		}
	}
	// The objects really exist for their owner. Without this a broken fixture
	// would make every "404 for a stranger" look like a pass.
	for name, path := range map[string]string{
		"conversation": "/chat/matches/" + ids["match"] + "/messages",
		"call":         "/calls/" + ids["call"],
		"group":        "/groups/" + ids["group"],
		"post":         "/social/posts/" + ids["post"] + "/comments",
		"photo":        "/media/photos/" + ids["photo"],
	} {
		if code, body := e.call("GET", path, nil, owner); code != 200 {
			t.Fatalf("the owner cannot read their own %s (%s): %d %s", name, path, code, trim(body))
		}
	}
	if _, body := e.call("GET", "/safety/contacts", nil, owner); !strings.Contains(string(body), ids["contact"]) {
		t.Fatalf("the owner's trusted contact is missing: %s", trim(body))
	}

	cipher := base64.StdEncoding.EncodeToString([]byte("not-a-real-ciphertext"))
	msg := map[string]any{"type": "text", "ciphertext": cipher, "burn_after_s": 0}

	cases := []bolaCase{
		{"read messages", "GET", "/chat/matches/{match}/messages", nil, true},
		{"send a message", "POST", "/chat/matches/{match}/messages", msg, true},
		{"mark read", "POST", "/chat/matches/{match}/read", map[string]string{}, true},
		{"chat access check", "GET", "/chat/matches/{match}/access", nil, true},
		{"delete the conversation", "DELETE", "/chat/matches/{match}", nil, true},
		{"unsend a message", "DELETE", "/chat/matches/{match}/messages/000000000000000000000000", nil, true},
		{"fetch key bundle", "GET", "/chat/keys/{match}", nil, true},
		{"reveal", "POST", "/matching/matches/{match}/reveal", map[string]string{}, false},
		{"rescore", "POST", "/matching/matches/{match}/rescore", map[string]string{}, false},
		{"reveal voice", "POST", "/matching/matches/{match}/reveal-voice", map[string]string{}, false},
		{"voice of a match", "GET", "/matching/matches/{match}/voice", nil, false},
		{"propose a reveal timer", "POST", "/matching/matches/{match}/reveal-timer", map[string]any{"hours": 24}, false},
		{"cancel a reveal timer", "DELETE", "/matching/matches/{match}/reveal-timer", nil, false},
		{"unmatch", "DELETE", "/matching/matches/{match}", nil, false},
		{"scam check", "POST", "/scamcheck/{match}", map[string]any{"lines": []map[string]string{{"who": "other", "text": "hi"}}}, false},
		{"quiz of a match", "GET", "/games/quiz/match/{match}", nil, false},
		{"signal access", "GET", "/signal/game/{match}/access", nil, false},

		{"view a call", "GET", "/calls/{call}", nil, false},
		{"accept a call", "POST", "/calls/{call}/accept", map[string]string{}, false},
		{"decline a call", "POST", "/calls/{call}/decline", map[string]string{}, false},
		{"end a call", "POST", "/calls/{call}/end", map[string]string{}, false},

		{"view a group", "GET", "/groups/{group}", nil, false},
		{"invite to a group", "POST", "/groups/{group}/invite", map[string]string{"ref": "00000000-0000-0000-0000-000000000000"}, false},
		{"schedule a group", "POST", "/groups/{group}/schedule", map[string]any{"group_match_id": "00000000-0000-0000-0000-000000000001", "venue": "Cafe", "address": "1 Main St", "starts_at": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)}, false},
		{"leave a group", "DELETE", "/groups/{group}/members/me", nil, false},

		{"like a post", "POST", "/social/posts/{post}/like", map[string]string{}, false},
		{"read comments", "GET", "/social/posts/{post}/comments", nil, false},
		{"comment on a post", "POST", "/social/posts/{post}/comments", map[string]string{"body": "hello"}, false},
		{"report a post", "POST", "/social/posts/{post}/report", map[string]string{"reason": "spam"}, false},

		{"remove a trusted contact", "DELETE", "/safety/contacts/{contact}", nil, false},
		{"set a contact's phone", "PUT", "/safety/contacts/{contact}/phone", map[string]string{"phone": "+15550100"}, false},

		{"fetch a private photo", "GET", "/media/photos/{photo}", nil, false},
		{"delete someone else's photo", "DELETE", "/users/me/photos/{photo}", nil, false},
		{"caption someone else's photo", "PUT", "/users/me/photos/{photo}/caption", map[string]string{"caption": "hi"}, false},
		{"make someone else's photo main", "PUT", "/users/me/photos/{photo}/main", map[string]string{}, false},
	}

	fill := func(s string) string {
		for k, v := range ids {
			s = strings.ReplaceAll(s, "{"+k+"}", v)
		}
		return s
	}
	for _, c := range cases {
		path := fill(c.path)
		if strings.Contains(path, "{") {
			t.Fatalf("%s: unresolved placeholder in %s", c.name, c.path)
		}
		t.Run(c.name, func(t *testing.T) {
			if code, body := e.call(c.method, path, c.body, stranger); code != 404 {
				t.Errorf("stranger: want 404, got %d %s", code, trim(body))
			}
			if code, body := e.call(c.method, path, c.body, blocked); code != 404 {
				t.Errorf("blocked member: want 404, got %d %s", code, trim(body))
			}
			if rr := e.fire(c.method, path, bolaJSON(c.body), "", nil); rr.Code != 401 {
				t.Errorf("no token: want 401, got %d", rr.Code)
			}
		})
	}
}

func trim(b []byte) string {
	s := string(b)
	if len(s) > 100 {
		s = s[:100] + "..."
	}
	return s
}

func bolaJSON(v any) []byte {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}
