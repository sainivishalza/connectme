package integration

import (
	"strings"
	"testing"
)

type pair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (e *env) loginPair(t *testing.T, email, pw string) (int, pair) {
	t.Helper()
	c, b := e.call("POST", "/auth/login", map[string]string{"email": email, "password": pw}, "anon")
	var p pair
	if c == 200 {
		decodeInto(t, b, &p)
	}
	return c, p
}

func (e *env) refresh(t *testing.T, rt string) int {
	t.Helper()
	c, _ := e.call("POST", "/auth/refresh", map[string]string{"refresh_token": rt}, "anon")
	return c
}

const newPW = "Brand-New-Horse-42!"

func TestChangePasswordRules(t *testing.T) {
	e := setup(t)
	uid, email := e.userWithPassword("Changer")

	cases := map[string]struct {
		body map[string]string
		want int
	}{
		"wrong current":  {map[string]string{"current_password": "not-my-password", "new_password": newPW}, 403},
		"weak new":       {map[string]string{"current_password": erasePW, "new_password": "short"}, 422},
		"same as before": {map[string]string{"current_password": erasePW, "new_password": erasePW}, 422},
	}
	for name, c := range cases {
		if got, b := e.call("POST", "/auth/change-password", c.body, uid); got != c.want {
			t.Errorf("%s: got %d want %d (%s)", name, got, c.want, b)
		}
	}
	// None of the failures changed anything.
	if c, _ := e.loginPair(t, email, erasePW); c != 200 {
		t.Errorf("old password stopped working after failed changes: %d", c)
	}
}

func TestChangePasswordSignsOutOtherSessionsButKeepsThisOne(t *testing.T) {
	e := setup(t)
	uid, email := e.userWithPassword("Changer2")
	_, laptop := e.loginPair(t, email, erasePW)
	_, phone := e.loginPair(t, email, erasePW)

	c, b := e.call("POST", "/auth/change-password",
		map[string]string{"current_password": erasePW, "new_password": newPW}, uid)
	if c != 200 {
		t.Fatalf("change: %d %s", c, b)
	}
	var fresh pair
	decodeInto(t, b, &fresh)
	if fresh.AccessToken == "" || fresh.RefreshToken == "" {
		t.Fatal("no fresh token pair returned")
	}
	if got := e.refresh(t, laptop.RefreshToken); got != 401 {
		t.Errorf("old laptop session survived: %d", got)
	}
	if got := e.refresh(t, phone.RefreshToken); got != 401 {
		t.Errorf("old phone session survived: %d", got)
	}
	if got := e.refresh(t, fresh.RefreshToken); got != 200 {
		t.Errorf("the changing device was signed out too: %d", got)
	}
	if c, _ := e.loginPair(t, email, erasePW); c != 401 {
		t.Errorf("old password still works: %d", c)
	}
	if c, _ := e.loginPair(t, email, newPW); c != 200 {
		t.Errorf("new password does not work: %d", c)
	}
}

func TestLogoutAllRevokesEverySession(t *testing.T) {
	e := setup(t)
	uid, email := e.userWithPassword("Everywhere")
	_, a := e.loginPair(t, email, erasePW)
	_, b := e.loginPair(t, email, erasePW)
	if c, _ := e.call("POST", "/auth/logout-all", nil, uid); c != 204 {
		t.Fatalf("logout-all: %d", c)
	}
	for i, rt := range []string{a.RefreshToken, b.RefreshToken} {
		if got := e.refresh(t, rt); got != 401 {
			t.Errorf("session %d survived logout-all: %d", i, got)
		}
	}
	if c, _ := e.loginPair(t, email, erasePW); c != 200 {
		t.Error("account locked out by logout-all")
	}
}

func TestChangePasswordIsRateLimitedAgainstGuessing(t *testing.T) {
	e := setup(t)
	uid, _ := e.userWithPassword("Guesser")
	var last int
	for i := 0; i < 8; i++ {
		last, _ = e.call("POST", "/auth/change-password",
			map[string]string{"current_password": "guess-" + strings.Repeat("x", i+1), "new_password": newPW}, uid)
	}
	if last != 429 {
		t.Errorf("guessing the current password is not rate limited: last status %d", last)
	}
}
