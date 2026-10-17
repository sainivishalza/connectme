package integration

import (
	"testing"
	"time"
)

func (e *env) getMe(token string) int {
	rec := e.fire("GET", "/users/me", nil, token, nil)
	return rec.Code
}

// Access tokens are short-lived but stateless; revoking a session must end
// them at once, not 15 minutes later.
func TestRevokingSessionsVoidsExistingAccessTokensImmediately(t *testing.T) {
	e := setup(t)
	uid, email := e.userWithPassword("Revoked")
	_, old := e.loginPair(t, email, erasePW)
	if c := e.getMe(old.AccessToken); c != 200 {
		t.Fatalf("fresh token: %d", c)
	}
	time.Sleep(1100 * time.Millisecond) // tokens are dated to the second

	if c, _ := e.call("POST", "/auth/logout-all", nil, uid); c != 204 {
		t.Fatalf("logout-all: %d", c)
	}
	if c := e.getMe(old.AccessToken); c != 401 {
		t.Errorf("access token survived sign-out everywhere: %d", c)
	}
	// Signing in again works straight away.
	c, fresh := e.loginPair(t, email, erasePW)
	if c != 200 || e.getMe(fresh.AccessToken) != 200 {
		t.Errorf("new session after revocation: %d", c)
	}

	// A ban is the same: the banned person is locked out now, not in 15 minutes.
	time.Sleep(1100 * time.Millisecond)
	_, victim := e.loginPair(t, email, erasePW)
	time.Sleep(1100 * time.Millisecond)
	mod := e.newUser("moderator", true, "Mod Ban")
	// Banning through the staff API revokes sessions.
	ref := e.refOf(uid)
	if c, b := e.callAs("POST", "/moderation/users/"+ref+"/ban", map[string]string{"reason": "test of immediate effect"}, mod, "moderator"); c != 204 && c != 200 {
		t.Fatalf("ban: %d %s", c, b)
	}
	if c := e.getMe(victim.AccessToken); c != 401 {
		t.Errorf("banned user's token still works: %d", c)
	}
}

func TestPasswordResetAndChangeAlsoVoidOldAccessTokens(t *testing.T) {
	e := setup(t)
	uid, email := e.userWithPassword("Changer3")
	_, laptop := e.loginPair(t, email, erasePW)
	time.Sleep(1100 * time.Millisecond)
	c, b := e.call("POST", "/auth/change-password", map[string]string{"current_password": erasePW, "new_password": newPW}, uid)
	if c != 200 {
		t.Fatalf("change: %d %s", c, b)
	}
	var fresh pair
	decodeInto(t, b, &fresh)
	if e.getMe(laptop.AccessToken) != 401 {
		t.Error("the other device's access token survived a password change")
	}
	if e.getMe(fresh.AccessToken) != 200 {
		t.Error("the device that changed the password was signed out")
	}
}

func TestAdminCanRevokeSessionsAfterARoleChange(t *testing.T) {
	e := setup(t)
	uid, email := e.userWithPassword("Demoted Mod")
	e.exec(`UPDATE users SET role='moderator' WHERE id=$1`, uid)
	_, p := e.loginPair(t, email, erasePW)
	if c := e.fire("GET", "/moderation/stats", nil, p.AccessToken, nil).Code; c != 200 {
		t.Fatalf("moderator before demotion: %d", c)
	}
	time.Sleep(1100 * time.Millisecond)

	// Demoted in the database only: the token still says moderator.
	e.exec(`UPDATE users SET role='user' WHERE id=$1`, uid)
	if c := e.fire("GET", "/moderation/stats", nil, p.AccessToken, nil).Code; c != 200 {
		t.Fatalf("expected the stale role to still work before revocation, got %d", c)
	}
	mod := e.newUser("moderator", true, "Not Admin")
	admin := e.newUser("admin", true, "Revoker")
	ref := e.refOf(uid)
	if c, _ := e.callAs("POST", "/moderation/users/"+ref+"/revoke-sessions", map[string]string{"reason": "role changed"}, mod, "moderator"); c != 403 {
		t.Errorf("moderator revoked sessions: %d", c)
	}
	if c, _ := e.callAs("POST", "/moderation/users/"+ref+"/revoke-sessions", map[string]string{"reason": "no"}, admin, "admin"); c != 422 {
		t.Errorf("revocation without a reason: %d", c)
	}
	if c, b := e.callAs("POST", "/moderation/users/"+ref+"/revoke-sessions", map[string]string{"reason": "role changed to user"}, admin, "admin"); c != 204 {
		t.Fatalf("revoke: %d %s", c, b)
	}
	if c := e.fire("GET", "/moderation/stats", nil, p.AccessToken, nil).Code; c != 401 {
		t.Errorf("stale staff token after revocation: %d", c)
	}
	if c, _ := e.loginPair(t, email, erasePW); c != 200 {
		t.Errorf("signing in again after revocation: %d", c)
	}
}
