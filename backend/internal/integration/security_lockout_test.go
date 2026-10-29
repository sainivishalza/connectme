package integration

import (
	"fmt"
	"math/rand"
	"net/http/httptest"
	"strings"
	"testing"
)

// loginFrom posts a login from a chosen client address.
func (e *env) loginFrom(ip, email, pw string) int {
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, pw)
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	req.RemoteAddr = ip + ":4000"
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec.Code
}

// Guessing wrong passwords for someone's email must lock out the guesser, not
// the owner: the owner can still sign in from their own address.
func TestAttackerCannotLockTheOwnerOut(t *testing.T) {
	e := setup(t)
	email := fmt.Sprintf("owner%d@example.com", rand.Intn(1e9))
	pw := "Correct-Horse-9-Battery"
	if c, b := authRegister(e, email, pw); c >= 300 {
		t.Fatalf("register: %d %s", c, b)
	}
	attacker := fmt.Sprintf("198.51.%d.%d", rand.Intn(250), rand.Intn(250))
	owner := fmt.Sprintf("203.0.%d.%d", rand.Intn(250), rand.Intn(250))

	for i := 0; i < 10; i++ {
		if c := e.loginFrom(attacker, email, "wrong-guess-"+fmt.Sprint(i)); c != 401 {
			t.Fatalf("guess %d: want 401, got %d", i, c)
		}
	}
	if c := e.loginFrom(attacker, email, pw); c != 429 {
		t.Errorf("the guesser must be locked out even with the right password: got %d", c)
	}
	if c := e.loginFrom(owner, email, pw); c != 200 {
		t.Errorf("the owner, from their own address, must still get in: got %d", c)
	}
}

// Guessing from many addresses is still stopped by a higher per-email limit.
func TestGuessingFromManyAddressesStillLocksTheEmail(t *testing.T) {
	e := setup(t)
	email := fmt.Sprintf("spread%d@example.com", rand.Intn(1e9))
	pw := "Correct-Horse-9-Battery"
	if c, b := authRegister(e, email, pw); c >= 300 {
		t.Fatalf("register: %d %s", c, b)
	}
	locked := false
	for i := 0; i < 80 && !locked; i++ {
		ip := fmt.Sprintf("192.0.%d.%d", i%250, rand.Intn(250))
		if e.loginFrom(ip, email, fmt.Sprintf("wrong-%d", i)) == 429 {
			locked = true
		}
	}
	if !locked {
		t.Error("80 wrong guesses from 80 different addresses never locked the email")
	}
	if c := e.loginFrom("198.18.1.1", email, pw); c != 429 {
		t.Errorf("a distributed guess run must lock the email for everyone: got %d", c)
	}
}
