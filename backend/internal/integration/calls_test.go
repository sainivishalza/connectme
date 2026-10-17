package integration

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type callResp struct {
	Call struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Video  bool   `json:"video"`
		Mine   bool   `json:"mine"`
		Peer   *struct {
			Anonymous bool `json:"anonymous"`
		} `json:"peer"`
	} `json:"call"`
	Connect struct {
		Token string `json:"token"`
		Room  string `json:"room"`
		URL   string `json:"url"`
	} `json:"connect"`
}

func (e *env) startCall(uid, matchID string, video bool) (int, callResp) {
	e.t.Helper()
	c, b := e.call("POST", "/calls/", map[string]any{"match_id": matchID, "video": video}, uid)
	var r callResp
	if c == 201 {
		decodeInto(e.t, b, &r)
	}
	return c, r
}

func (e *env) callStatus(uid, id string) string {
	e.t.Helper()
	c, b := e.call("GET", "/calls/"+id, nil, uid)
	if c != 200 {
		e.t.Fatalf("get call: %d %s", c, b)
	}
	var v struct {
		Status string `json:"status"`
	}
	decodeInto(e.t, b, &v)
	return v.Status
}

func (e *env) endAllCalls() {
	e.exec(`UPDATE calls SET status='ended' WHERE status IN ('ringing','active')`)
}

func TestCallRingAcceptEnd(t *testing.T) {
	e := setup(t)
	a, b, m := e.matched()

	c, call := e.startCall(a, m, false)
	if c != 201 || call.Call.Status != "ringing" || !call.Call.Mine {
		t.Fatalf("start: %d %+v", c, call)
	}
	// The caller's token is scoped to this call's room and lets them speak.
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(call.Connect.Token, claims, func(*jwt.Token) (any, error) { return []byte(liveSecret), nil },
		jwt.WithValidMethods([]string{"HS256"})); err != nil {
		t.Fatalf("token: %v", err)
	}
	grant := claims["video"].(map[string]any)
	if grant["room"] != call.Connect.Room || grant["canPublish"] != true || claims["sub"] != a {
		t.Errorf("token not scoped to the call: %v", claims)
	}

	// The callee sees it ringing; the caller does not see their own call as incoming.
	_, body := e.call("GET", "/calls/incoming", nil, b)
	var inc struct {
		Call *struct {
			ID string `json:"id"`
		} `json:"call"`
	}
	decodeInto(t, body, &inc)
	if inc.Call == nil || inc.Call.ID != call.Call.ID {
		t.Fatalf("callee does not see the ringing call: %s", body)
	}
	_, body = e.call("GET", "/calls/incoming", nil, a)
	decodeInto(t, body, &inc)
	if inc.Call != nil {
		t.Error("the caller sees their own call as incoming")
	}

	c, ans := e.call("POST", "/calls/"+call.Call.ID+"/accept", nil, b)
	if c != 200 {
		t.Fatalf("accept: %d %s", c, ans)
	}
	var acc callResp
	decodeInto(t, ans, &acc)
	if acc.Call.Status != "active" || acc.Connect.Room != call.Connect.Room || acc.Connect.Token == "" {
		t.Errorf("accept result: %+v", acc)
	}
	if e.callStatus(a, call.Call.ID) != "active" {
		t.Error("caller does not see the call as active")
	}

	if c, _ := e.call("POST", "/calls/"+call.Call.ID+"/end", nil, b); c != 204 {
		t.Errorf("end: %d", c)
	}
	if c, _ := e.call("POST", "/calls/"+call.Call.ID+"/end", nil, a); c != 204 {
		t.Errorf("ending twice should be a no-op: %d", c)
	}
	if got := e.callStatus(a, call.Call.ID); got != "ended" {
		t.Errorf("status %q, want ended", got)
	}
}

func TestCallDeclineBusyOutsiderAndMissed(t *testing.T) {
	e := setup(t)
	a, b, m := e.matched()
	outsider := e.user("Outsider")

	_, call := e.startCall(a, m, false)
	// Everyone involved is busy while it rings.
	if c, _ := e.startCall(b, m, false); c != 409 {
		t.Errorf("callee calling back while ringing: %d", c)
	}
	// An outsider can neither see nor answer it.
	for _, p := range []string{"/calls/" + call.Call.ID} {
		if c, _ := e.call("GET", p, nil, outsider); c != 404 {
			t.Errorf("outsider get: %d", c)
		}
	}
	if c, _ := e.call("POST", "/calls/"+call.Call.ID+"/accept", nil, outsider); c != 404 {
		t.Errorf("outsider accept: %d", c)
	}
	// The caller cannot answer their own call.
	if c, _ := e.call("POST", "/calls/"+call.Call.ID+"/accept", nil, a); c == 200 {
		t.Error("caller accepted their own call")
	}
	if c, _ := e.call("POST", "/calls/"+call.Call.ID+"/decline", nil, b); c != 204 {
		t.Fatalf("decline: %d", c)
	}
	if got := e.callStatus(a, call.Call.ID); got != "declined" {
		t.Errorf("caller sees %q, want declined", got)
	}
	if c, _ := e.call("POST", "/calls/"+call.Call.ID+"/accept", nil, b); c == 200 {
		t.Error("accepted a declined call")
	}

	// A call nobody answers becomes missed and cannot be answered late.
	_, c2 := e.startCall(a, m, false)
	e.exec(`UPDATE calls SET created_at = NOW() - INTERVAL '2 minutes' WHERE id=$1::uuid`, c2.Call.ID)
	_, body := e.call("GET", "/calls/incoming", nil, b)
	if string(body) != "" && containsCall(body) {
		t.Errorf("a stale call is still ringing: %s", body)
	}
	if got := e.callStatus(a, c2.Call.ID); got != "missed" {
		t.Errorf("status %q, want missed", got)
	}
	if c, _ := e.call("POST", "/calls/"+c2.Call.ID+"/accept", nil, b); c == 200 {
		t.Error("answered a missed call")
	}
}

func containsCall(b []byte) bool {
	return len(b) > 0 && string(b) != `{"call":null}`+"\n" && string(b) != `{"call":null}`
}

func TestCallsRespectMatchBlockAndVerification(t *testing.T) {
	e := setup(t)
	a, b, m := e.matched()
	other := e.user("NoMatch")
	if c, _ := e.startCall(other, m, false); c != 404 {
		t.Errorf("non-participant started a call: %d", c)
	}
	if c, _ := e.startCall(a, "not-a-uuid", false); c != 404 {
		t.Errorf("bad match id: %d", c)
	}
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(b)}, a)
	if c, _ := e.startCall(a, m, false); c != 404 {
		t.Errorf("called a blocked person: %d", c)
	}
	if c, _ := e.startCall(b, m, false); c != 404 {
		t.Errorf("a blocked person called the blocker: %d", c)
	}
}

func TestVideoWithAnAnonymousMatchNeedsMutualReveal(t *testing.T) {
	e := setup(t)
	a, b, m := e.matched()
	e.exec(`UPDATE matches SET mode='incognito' WHERE id=$1::uuid`, m)

	if c, _ := e.startCall(a, m, true); c != 403 {
		t.Errorf("video before reveal: %d", c)
	}
	// Voice only is allowed.
	c, voice := e.startCall(a, m, false)
	if c != 201 {
		t.Fatalf("audio call in an incognito match: %d", c)
	}
	e.call("POST", "/calls/"+voice.Call.ID+"/end", nil, a)

	// One reveal is not enough; both are.
	e.exec(`INSERT INTO reveal_consents (match_id, user_id) VALUES ($1::uuid,$2)`, m, a)
	if c, _ := e.startCall(a, m, true); c != 403 {
		t.Errorf("video with one-sided reveal: %d", c)
	}
	e.exec(`INSERT INTO reveal_consents (match_id, user_id) VALUES ($1::uuid,$2)`, m, b)
	if c, _ := e.startCall(a, m, true); c != 201 {
		t.Errorf("video after mutual reveal: %d", c)
	}
}

func TestRandomChatsCannotBeCalledUntilBothKeepThem(t *testing.T) {
	e := setup(t)
	e.freshQueue()
	a, b := e.user("A"), e.user("B")
	e.randJoin(a)
	m := e.randJoin(b).Session.MatchID

	if c, _ := e.startCall(a, m, false); c != 403 {
		t.Errorf("called an anonymous stranger: %d", c)
	}
	e.call("POST", "/random/"+m+"/keep", nil, a)
	if c, _ := e.startCall(a, m, false); c != 403 {
		t.Errorf("called after only one person kept the chat: %d", c)
	}
	e.call("POST", "/random/"+m+"/keep", nil, b)
	if c, _ := e.startCall(a, m, false); c != 201 {
		t.Errorf("call after both kept: %d", c)
	}
}

func TestBanningACallerHangsUpTheirCall(t *testing.T) {
	e := setup(t)
	a, _, m := e.matched()
	mod := e.newUser("moderator", true, "Mod")
	_, call := e.startCall(a, m, false)
	c, b := e.callAs("POST", "/moderation/users/"+e.refOf(a)+"/ban", map[string]string{"reason": "Harassing calls"}, mod, "moderator")
	if c != 204 {
		t.Fatalf("ban: %d %s", c, b)
	}
	if n := e.count(`SELECT COUNT(*) FROM calls WHERE id=$1::uuid AND status='ended'`, call.Call.ID); n != 1 {
		t.Error("banned caller's call is still open")
	}
	time.Sleep(10 * time.Millisecond)
}

func TestUnverifiedMembersCannotCall(t *testing.T) {
	e := setup(t)
	a, _, m := e.matched()
	e.exec(`UPDATE users SET is_verified=false WHERE id=$1`, a)
	if c, _ := e.startCall(a, m, false); c == 201 {
		t.Error("unverified caller started a call")
	}
}
