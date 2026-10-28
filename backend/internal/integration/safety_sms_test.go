package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"connectme/internal/safety"
)

type smsBox struct {
	mu   sync.Mutex
	sent []struct{ to, text string }
}

func (b *smsBox) Send(_ context.Context, to, text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, struct{ to, text string }{to, text})
	return nil
}

func (b *smsBox) last() (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.sent) == 0 {
		return "", ""
	}
	l := b.sent[len(b.sent)-1]
	return l.to, l.text
}

func (b *smsBox) count() int { b.mu.Lock(); defer b.mu.Unlock(); return len(b.sent) }

var codeRe = regexp.MustCompile(`code: (\d{6})`)

func TestContactPhoneNeedsTheCodeBeforeItIsTextedAndThenGetsAlertsAndTheAllClear(t *testing.T) {
	e := setup(t)
	svc, ob := e.safetySvc()
	ctx := context.Background()
	me := e.user("Text Member")
	c, _ := svc.AddContact(ctx, me, "Sister", "sis-sms@example.org")

	// Texts are off until a provider is set; and only a contact who has agreed by email can get a number.
	if err := svc.SetPhone(ctx, me, c.ID, "+14155550123"); err == nil {
		t.Error("number accepted with no SMS provider")
	}
	box := &smsBox{}
	svc.SetSMS(box)
	if err := svc.SetPhone(ctx, me, c.ID, "+14155550123"); err == nil {
		t.Error("number accepted before the contact agreed by email")
	}
	if err := svc.ConfirmContact(ctx, tokenRe.FindStringSubmatch(ob.to("sis-sms@example.org")[0].body)[1]); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"4155550123", "+1 (415) 555", "+0123456789", "+1415555012345678", "abc"} {
		if err := svc.SetPhone(ctx, me, c.ID, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := svc.SetPhone(ctx, me, c.ID, "+1 415 555 0123"); err != nil {
		t.Fatal(err)
	}
	to, text := box.last()
	m := codeRe.FindStringSubmatch(text)
	if to != "+14155550123" || m == nil || !strings.Contains(text, "Text Member") {
		t.Fatalf("code text: %q %q", to, text)
	}
	// Until confirmed, no alert text goes to it and the list shows it masked, unconfirmed.
	list, _ := svc.Contacts(ctx, me)
	if list[0].Phone != "+*********23" || list[0].PhoneConfirmed || strings.Contains(list[0].Phone, "4155550") {
		t.Errorf("contact view: %+v", list[0])
	}
	if err := svc.ConfirmPhone(ctx, me, c.ID, "000000"); err == nil && m[1] != "000000" {
		t.Error("wrong code accepted")
	}
	// Someone else cannot confirm it.
	other := e.user("Other Member")
	if err := svc.ConfirmPhone(ctx, other, c.ID, m[1]); err == nil {
		t.Error("another member confirmed my contact's number")
	}
	if err := svc.ConfirmPhone(ctx, me, c.ID, m[1]); err != nil {
		t.Fatalf("right code: %v", err)
	}

	// Missed check-in: email AND text; the all-clear goes to both.
	plan, err := svc.CreatePlan(ctx, me, safety.PlanInput{ContactID: c.ID, Place: "Quay", MeetAt: time.Now().Add(-90 * time.Minute), AfterMin: 120})
	if err != nil {
		t.Fatal(err)
	}
	before := box.count()
	e.exec(`UPDATE date_plans SET checkin_by = NOW() - INTERVAL '1 minute' WHERE id=$1`, plan.ID)
	if n, _ := svc.Sweep(ctx); n != 1 {
		t.Fatalf("alerts: %d", n)
	}
	if box.count() != before+1 {
		t.Fatalf("alert texts: %d", box.count()-before)
	}
	if to, text := box.last(); to != "+14155550123" || !strings.Contains(text, "has not") || !strings.Contains(text, "Quay") {
		t.Errorf("alert text: %q %q", to, text)
	}
	if err := svc.Finish(ctx, me, plan.ID, "safe"); err != nil {
		t.Fatal(err)
	}
	if _, text := box.last(); !strings.Contains(text, "checked in and is safe") {
		t.Errorf("all-clear text: %q", text)
	}

	// Removing the number stops texts.
	if err := svc.RemovePhone(ctx, me, c.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.Contacts(ctx, me); list[0].Phone != "" || list[0].PhoneConfirmed {
		t.Errorf("number not removed: %+v", list[0])
	}
}

func TestPhoneCodesAreLimitedInNumberAndTries(t *testing.T) {
	e := setup(t)
	svc, ob := e.safetySvc()
	ctx := context.Background()
	box := &smsBox{}
	svc.SetSMS(box)
	me := e.user("Limit Member")
	c, _ := svc.AddContact(ctx, me, "Pal", "pal-sms@example.org")
	_ = svc.ConfirmContact(ctx, tokenRe.FindStringSubmatch(ob.to("pal-sms@example.org")[0].body)[1])
	for i := 0; i < 3; i++ {
		if err := svc.SetPhone(ctx, me, c.ID, "+14155550199"); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	if err := svc.SetPhone(ctx, me, c.ID, "+14155550199"); err == nil {
		t.Error("a fourth code in a day was sent")
	}
	if box.count() != 3 {
		t.Errorf("texts sent: %d", box.count())
	}
	_, text := box.last()
	right := codeRe.FindStringSubmatch(text)[1]
	for i := 0; i < 5; i++ {
		_ = svc.ConfirmPhone(ctx, me, c.ID, "999999")
	}
	if err := svc.ConfirmPhone(ctx, me, c.ID, right); err == nil {
		t.Error("the right code worked after five wrong tries")
	}
}

func TestTwilioSMSPostsTheFormToTheAccount(t *testing.T) {
	var gotPath, gotUser, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUser, _, _ = r.BasicAuth()
		_ = r.ParseForm()
		gotBody = r.Form.Get("To") + "|" + r.Form.Get("From") + "|" + r.Form.Get("Body")
		w.WriteHeader(201)
	}))
	defer srv.Close()
	err := safety.TwilioSMS{AccountSID: "ACtest", AuthToken: "tok", From: "+15550001111", BaseURL: srv.URL}.Send(context.Background(), "+14155550123", "hello")
	if err != nil || gotPath != "/2010-04-01/Accounts/ACtest/Messages.json" || gotUser != "ACtest" || gotBody != "+14155550123|+15550001111|hello" {
		t.Errorf("request: %v %q %q %q", err, gotPath, gotUser, gotBody)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	defer bad.Close()
	if err := (safety.TwilioSMS{AccountSID: "x", AuthToken: "y", From: "z", BaseURL: bad.URL}).Send(context.Background(), "+1", "hi"); err == nil {
		t.Error("a provider error was swallowed")
	}
}
