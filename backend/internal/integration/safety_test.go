package integration

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"connectme/internal/safety"
	"connectme/internal/users"
)

type outbox struct {
	mu   sync.Mutex
	sent []struct{ to, subject, body string }
}

func (o *outbox) Send(_ context.Context, to, subject, body string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, struct{ to, subject, body string }{to, subject, body})
	return nil
}

func (o *outbox) to(addr string) []struct{ to, subject, body string } {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []struct{ to, subject, body string }
	for _, m := range o.sent {
		if m.to == addr {
			out = append(out, m)
		}
	}
	return out
}

func (e *env) safetySvc() (*safety.Service, *outbox) {
	ob := &outbox{}
	return safety.NewService(e.db, users.NewService(e.db, users.LocalStore{Dir: e.t.TempDir()}, nil), ob, "https://app.test"), ob
}

var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

func TestTrustedContactsMustConfirmBeforeTheyReceiveAnything(t *testing.T) {
	e := setup(t)
	svc, ob := e.safetySvc()
	ctx := context.Background()
	me := e.user("Safe Member")

	// Validation: bad address, yourself, and the limit.
	if _, err := svc.AddContact(ctx, me, "Mum", "not an email"); err == nil {
		t.Error("bad address accepted")
	}
	var own string
	_ = e.db.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, me).Scan(&own)
	if _, err := svc.AddContact(ctx, me, "Me", own); err == nil {
		t.Error("own address accepted")
	}
	c, err := svc.AddContact(ctx, me, "Aunt Rosa", "rosa@example.org")
	if err != nil || c.Confirmed {
		t.Fatalf("add: %+v %v", c, err)
	}
	mails := ob.to("rosa@example.org")
	if len(mails) != 1 || !strings.Contains(mails[0].body, "https://app.test/safety/confirm?token=") {
		t.Fatalf("confirmation mail: %+v", mails)
	}
	token := tokenRe.FindStringSubmatch(mails[0].body)[1]

	// An unconfirmed contact cannot be used for a plan, and a plan sends nothing.
	meet := time.Now().Add(3 * time.Hour)
	if _, err := svc.CreatePlan(ctx, me, safety.PlanInput{ContactID: c.ID, Place: "Cafe Luna", MeetAt: meet, AfterMin: 120}); err == nil {
		t.Fatal("plan with an unconfirmed contact")
	}
	if len(ob.to("rosa@example.org")) != 1 {
		t.Error("something was sent to an unconfirmed contact")
	}
	if err := svc.ConfirmContact(ctx, "x"+token); err == nil {
		t.Error("wrong token accepted")
	}
	if err := svc.ConfirmContact(ctx, token); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	// Plan: validation, then the contact is emailed the place and times.
	for name, in := range map[string]safety.PlanInput{
		"in the past":   {ContactID: c.ID, Place: "Cafe Luna", MeetAt: time.Now().Add(-5 * time.Hour), AfterMin: 120},
		"too soon":      {ContactID: c.ID, Place: "Cafe Luna", MeetAt: meet, AfterMin: 5},
		"no place":      {ContactID: c.ID, Place: "  ", MeetAt: meet, AfterMin: 120},
		"unknown":       {ContactID: "00000000-0000-0000-0000-000000000000", Place: "Cafe Luna", MeetAt: meet, AfterMin: 120},
		"other persons": {ContactID: c.ID, Place: "Cafe Luna", MeetAt: meet, AfterMin: 120, MatchID: "00000000-0000-0000-0000-000000000000"},
	} {
		if _, err := svc.CreatePlan(ctx, me, in); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	p, err := svc.CreatePlan(ctx, me, safety.PlanInput{ContactID: c.ID, Place: "<b>Cafe Luna</b>, Main St", MeetAt: meet, AfterMin: 120})
	if err != nil || p.Place != "Cafe Luna, Main St" {
		t.Fatalf("plan: %+v %v", p, err)
	}
	if m := ob.to("rosa@example.org"); len(m) != 2 || !strings.Contains(m[1].body, "Cafe Luna, Main St") || !strings.Contains(m[1].body, "Safe Member") {
		t.Fatalf("plan mail: %+v", m)
	}

	// Nobody else can touch my contacts or plans.
	other := e.user("Snooper")
	if err := svc.RemoveContact(ctx, other, c.ID); err == nil {
		t.Error("another member removed my contact")
	}
	if err := svc.Finish(ctx, other, p.ID, "safe"); err == nil {
		t.Error("another member checked in for me")
	}
	if ps, _ := svc.Plans(ctx, other); len(ps) != 0 {
		t.Errorf("plans leaked: %+v", ps)
	}

	// Checking in tells the contact; it can only be done once.
	if err := svc.Finish(ctx, me, p.ID, "safe"); err != nil {
		t.Fatalf("check in: %v", err)
	}
	if m := ob.to("rosa@example.org"); !strings.Contains(m[len(m)-1].subject, "checked in") {
		t.Errorf("no check-in mail: %+v", m)
	}
	if err := svc.Finish(ctx, me, p.ID, "safe"); err == nil {
		t.Error("checked in twice")
	}
}

func TestMissedCheckInAlertsTheContactOnceAndAReminderComesFirst(t *testing.T) {
	e := setup(t)
	svc, ob := e.safetySvc()
	ctx := context.Background()
	me := e.user("Late Member")
	c, _ := svc.AddContact(ctx, me, "Brother", "bro@example.org")
	_ = svc.ConfirmContact(ctx, tokenRe.FindStringSubmatch(ob.to("bro@example.org")[0].body)[1])
	soon, _ := svc.CreatePlan(ctx, me, safety.PlanInput{ContactID: c.ID, Place: "Harbour pub", MeetAt: time.Now().Add(-90 * time.Minute), AfterMin: 120})
	e.exec(`UPDATE date_plans SET checkin_by = NOW() + INTERVAL '10 minutes' WHERE id=$1`, soon.ID)

	// Nothing is sent to the contact for an on-time plan; the member is reminded.
	n, err := svc.Sweep(ctx)
	if err != nil || n != 0 {
		t.Fatalf("sweep: %d %v", n, err)
	}
	if len(ob.to("bro@example.org")) != 2 { // confirmation + plan
		t.Errorf("contact mailed too early: %+v", ob.to("bro@example.org"))
	}

	// Extending works, up to three times, and clears the reminder.
	if err := svc.Extend(ctx, me, soon.ID, 30); err != nil {
		t.Fatal(err)
	}
	if err := svc.Extend(ctx, me, soon.ID, 1); err == nil {
		t.Error("tiny extension accepted")
	}

	// Deadline passes: the contact is emailed exactly once.
	e.exec(`UPDATE date_plans SET checkin_by = NOW() - INTERVAL '1 minute' WHERE id=$1`, soon.ID)
	if n, _ := svc.Sweep(ctx); n != 1 {
		t.Fatalf("alerts: %d", n)
	}
	if n, _ := svc.Sweep(ctx); n != 0 {
		t.Errorf("alerted twice: %d", n)
	}
	m := ob.to("bro@example.org")
	last := m[len(m)-1]
	if !strings.Contains(last.subject, "has not checked in") || !strings.Contains(last.body, "Harbour pub") {
		t.Errorf("alert mail: %+v", last)
	}
	// Late but fine: checking in afterwards tells the contact all is well.
	if err := svc.Finish(ctx, me, soon.ID, "safe"); err != nil {
		t.Fatal(err)
	}
	m = ob.to("bro@example.org")
	if !strings.Contains(m[len(m)-1].subject, "checked in") {
		t.Errorf("no all-clear: %+v", m[len(m)-1])
	}
	// A cancelled plan never alerts.
	p2, _ := svc.CreatePlan(ctx, me, safety.PlanInput{ContactID: c.ID, Place: "Park", MeetAt: time.Now(), AfterMin: 60})
	_ = svc.Finish(ctx, me, p2.ID, "cancel")
	e.exec(`UPDATE date_plans SET checkin_by = NOW() - INTERVAL '1 minute' WHERE id=$1`, p2.ID)
	if n, _ := svc.Sweep(ctx); n != 0 {
		t.Errorf("a cancelled plan alerted: %d", n)
	}
}

func TestSafetyRoutesNeedSignInAndTheConfirmLinkRejectsGuesses(t *testing.T) {
	e := setup(t)
	me := e.user("Route Member")
	if c, _ := e.call("GET", "/safety/contacts", nil, ""); c != 401 {
		t.Errorf("signed out: %d", c)
	}
	if c, _ := e.call("POST", "/safety/confirm", map[string]string{"token": strings.Repeat("a", 32)}, ""); c != 404 {
		t.Errorf("guessed token: %d", c)
	}
	if c, b := e.call("POST", "/safety/contacts", map[string]string{"name": "Gran", "email": "gran@example.org"}, me); c != 201 {
		t.Fatalf("add: %d %s", c, b)
	}
	if c, b := e.call("GET", "/safety/contacts", nil, me); c != 200 || !strings.Contains(string(b), `"confirmed":false`) {
		t.Errorf("contacts: %d %s", c, b)
	}
	if c, _ := e.call("POST", "/safety/contacts", map[string]string{"name": "x", "email": "bad"}, me); c != 422 {
		t.Errorf("bad email: %d", c)
	}
}

// One contact who does not read their mail must not be the only safety net:
// every confirmed contact is told, and every one of them hears the all-clear.
func TestMissedCheckInAlertsEveryConfirmedContactAndTheAllClearReachesThemToo(t *testing.T) {
	e := setup(t)
	svc, ob := e.safetySvc()
	ctx := context.Background()
	me := e.user("Careful Member")
	first, _ := svc.AddContact(ctx, me, "Sister", "sis@example.org")
	_, _ = svc.AddContact(ctx, me, "Friend", "friend@example.org")
	_, _ = svc.AddContact(ctx, me, "Stranger", "unconfirmed@example.org")
	for _, to := range []string{"sis@example.org", "friend@example.org"} {
		if err := svc.ConfirmContact(ctx, tokenRe.FindStringSubmatch(ob.to(to)[0].body)[1]); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := svc.CreatePlan(ctx, me, safety.PlanInput{ContactID: first.ID, Place: "Old Mill", MeetAt: time.Now().Add(-90 * time.Minute), AfterMin: 120})
	if err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE date_plans SET checkin_by = NOW() - INTERVAL '1 minute' WHERE id=$1`, plan.ID)
	if n, _ := svc.Sweep(ctx); n != 1 {
		t.Fatalf("alerts: %d", n)
	}
	for _, to := range []string{"sis@example.org", "friend@example.org"} {
		m := ob.to(to)
		if last := m[len(m)-1]; !strings.Contains(last.subject, "has not checked in") {
			t.Errorf("%s was not alerted: %+v", to, last)
		}
	}
	if m := ob.to("unconfirmed@example.org"); len(m) != 1 { // only the confirmation request
		t.Errorf("an unconfirmed address was mailed: %+v", m)
	}
	if err := svc.Finish(ctx, me, plan.ID, "safe"); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"sis@example.org", "friend@example.org"} {
		m := ob.to(to)
		if !strings.Contains(m[len(m)-1].subject, "checked in") || strings.Contains(m[len(m)-1].subject, "not") {
			t.Errorf("%s got no all-clear: %+v", to, m[len(m)-1])
		}
	}
}
