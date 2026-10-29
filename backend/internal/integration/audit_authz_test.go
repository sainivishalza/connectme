package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"connectme/internal/auth"
)

// API3 (mass assignment): server-controlled fields can never be set by a client.
func TestAuditMassAssignmentIsImpossible(t *testing.T) {
	e := setup(t)
	u := e.user("Mass")
	forbidden := []string{"role", "is_verified", "is_banned", "password_hash", "id", "user_id", "email",
		"date_of_birth", "public_ref", "location", "boosted_until", "prev_mode", "mode", "photos", "id_verified_at"}
	for _, f := range forbidden {
		body, _ := json.Marshal(map[string]any{f: "x"})
		rec := e.fire("PATCH", "/users/me", body, e.tokenFor(u, "user"), map[string]string{"Content-Type": "application/json"})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PATCH /users/me accepted unknown field %q: %d", f, rec.Code)
		}
	}
	var role string
	var verified, banned bool
	if err := e.db.QueryRow(context.Background(), `SELECT role, is_verified, is_banned FROM users WHERE id=$1`, u).
		Scan(&role, &verified, &banned); err != nil {
		t.Fatal(err)
	}
	if role != "user" || !verified || banned {
		t.Fatalf("account state changed: %s %v %v", role, verified, banned)
	}
	// Registration cannot grant a role either.
	rec := e.fire("POST", "/auth/register",
		[]byte(`{"email":"mass@example.com","password":"Correct-Horse-9!","date_of_birth":"1990-01-01","role":"admin"}`),
		"", map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("register with role: %d", rec.Code)
	}
	// Groups: ownership cannot be spoofed.
	loc, lng := randomBase()
	g := e.located("Owner", loc, lng)
	body, _ := json.Marshal(map[string]any{"name": "n", "type": "double", "activity": "dinner", "created_by": u})
	if rec := e.fire("POST", "/groups", body, e.tokenFor(g, "user"), map[string]string{"Content-Type": "application/json"}); rec.Code != 400 {
		t.Errorf("group with created_by: %d", rec.Code)
	}
}

// API1 (BOLA): a stranger gets "not found" for every object that is not theirs.
func TestAuditObjectLevelAuthorizationMatrix(t *testing.T) {
	e := needChat(t)
	lat, lng := randomBase()

	owner, buddy, stranger := e.user("Owner"), e.user("Buddy"), e.user("Stranger")
	e.locate(owner, lat, lng)
	e.call("POST", "/users/me/mode", map[string]string{"mode": "incognito"}, owner)
	if c := uploadPhoto(e, owner, jpegBytesFor(t)); c != 201 {
		t.Fatalf("photo: %d", c)
	}
	_, me := e.call("GET", "/users/me", nil, owner)
	var prof struct {
		Photos []struct{ ID string }
		Ref    string
	}
	decodeInto(t, me, &prof)
	photoID := prof.Photos[0].ID

	e.swipe(owner, e.refOf(buddy), "like")
	_, m := e.swipe(buddy, e.refOf(owner), "like")
	e.send(owner, m.MatchID, map[string]any{"type": "text", "ciphertext": ct("secret")})
	_, mb := e.raw("POST", "/chat/matches/"+m.MatchID+"/media", []byte("blob"), owner)
	var up struct {
		MediaID string `json:"media_id"`
	}
	decodeInto(t, mb, &up)

	_, grp := e.newGroup(owner, "group", "hiking", 3)
	e.call("POST", "/groups/"+grp.ID+"/invite", map[string]string{"ref": e.refOf(buddy)}, owner)
	_, inv := e.call("GET", "/groups/invites", nil, buddy)
	var invites struct{ Invites []struct{ ID string } }
	decodeInto(t, inv, &invites)

	type probe struct {
		name, method, path string
		body               any
		who                string
	}
	probes := []probe{
		{"incognito photo", "GET", "/media/photos/" + photoID, nil, stranger},
		{"delete photo", "DELETE", "/users/me/photos/" + photoID, nil, stranger},
		{"match messages", "GET", "/chat/matches/" + m.MatchID + "/messages", nil, stranger},
		{"send message", "POST", "/chat/matches/" + m.MatchID + "/messages", map[string]any{"type": "text", "ciphertext": ct("x")}, stranger},
		{"read receipts", "POST", "/chat/matches/" + m.MatchID + "/read", nil, stranger},
		{"delete conversation", "DELETE", "/chat/matches/" + m.MatchID, nil, stranger},
		{"chat media", "GET", "/chat/media/" + up.MediaID, nil, stranger},
		{"key bundle", "GET", "/chat/keys/" + m.MatchID, nil, stranger},
		{"unmatch", "DELETE", "/matching/matches/" + m.MatchID, nil, stranger},
		{"reveal", "POST", "/matching/matches/" + m.MatchID + "/reveal", nil, stranger},
		{"rescore", "POST", "/matching/matches/" + m.MatchID + "/rescore", nil, stranger},
		{"group detail", "GET", "/groups/" + grp.ID, nil, stranger},
		{"group invite", "POST", "/groups/" + grp.ID + "/invite", map[string]string{"ref": e.refOf(buddy)}, stranger},
		{"group leave", "DELETE", "/groups/" + grp.ID + "/members/me", nil, stranger},
		{"group suggestions", "POST", "/groups/" + grp.ID + "/venue", nil, stranger},
		{"accept someone else's invite", "POST", "/groups/invites/" + invites.Invites[0].ID + "/accept", nil, stranger},
		{"join without invite", "POST", "/groups/" + grp.ID + "/join", nil, stranger},
		{"someone else's profile by guessed id", "GET", "/users/" + sampleUUID, nil, stranger},
	}
	for _, p := range probes {
		var who = stranger
		var body []byte
		if p.body != nil {
			body, _ = json.Marshal(p.body)
		}
		rec := e.fire(p.method, p.path, body, e.tokenFor(who, "user"), map[string]string{"Content-Type": "application/json"})
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
			t.Errorf("%s: stranger got %d (%s)", p.name, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
	// Sanity: the rightful owner still can (so the matrix is not just failing everywhere).
	if rec := e.fire("GET", "/chat/matches/"+m.MatchID+"/messages", nil, e.tokenFor(owner, "user"), nil); rec.Code != 200 {
		t.Fatalf("owner lost access: %d", rec.Code)
	}
}

// API3 (excessive data exposure): other people's responses never carry
// internal ids, contact details, hashes or coordinates.
func TestAuditNoSensitiveDataInOtherUsersResponses(t *testing.T) {
	e := needChat(t)
	lat, lng := randomBase()
	a, email := e.userWithPassword("Sensitive Sam")
	b := e.user("Observer")
	c := e.user("Observer2")
	e.locate(a, lat, lng)
	e.locate(b, lat+0.001, lng)
	e.locate(c, lat+0.002, lng)
	e.call("PATCH", "/users/me", map[string]any{"bio": "open bio", "tags": []string{"hiking"}}, a)
	e.exec(`UPDATE users SET phone='+14155550199' WHERE id=$1`, a)
	e.swipe(a, e.refOf(b), "like")
	_, mt := e.swipe(b, e.refOf(a), "like")
	_, grp := e.newGroup(a, "double", "dinner", 0)
	e.join(a, c, grp.ID)

	secrets := []string{a, email, "1990-04-04", "password_hash", "+14155550199", "date_of_birth", "stripe", "is_banned",
		"user1_id", "user2_id", "created_by", "sender_id", "user_id"}
	coordRE := regexp.MustCompile(`"(lat|lng|latitude|longitude|location)"\s*:`)

	paths := []string{
		"/users/" + e.refOf(a), "/users/nearby?radius_km=25", "/matching/matches", "/matching/likes-you",
		"/groups", "/groups/" + grp.ID, "/groups/matches", "/chat/matches/" + mt.MatchID + "/messages",
		"/payments/gifts", "/geo/detect", "/groups/invites", "/matching/matches?limit=50",
	}
	for _, who := range []string{b, c} {
		for _, p := range paths {
			rec := e.fire("GET", p, nil, e.tokenFor(who, "user"), nil)
			body := rec.Body.String()
			for _, s := range secrets {
				if strings.Contains(body, s) {
					t.Errorf("GET %s leaks %q to another user: %.200s", p, s, body)
				}
			}
			if coordRE.MatchString(body) {
				t.Errorf("GET %s exposes coordinates: %.200s", p, body)
			}
		}
	}
	// The one place the user's own id legitimately appears is their own export/profile, never as "user_id".
	rec := e.fire("GET", "/users/me", nil, e.tokenFor(a, "user"), nil)
	if strings.Contains(rec.Body.String(), "password_hash") || coordRE.MatchString(rec.Body.String()) {
		t.Errorf("own profile exposes secrets: %s", rec.Body.String())
	}
}

// Logging hygiene (A09): production mode never writes tokens to logs.
func TestAuditProductionLogsContainNoTokens(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	prod := auth.LogMailer{Production: true}
	_ = prod.SendVerification(context.Background(), "u@example.com", "SUPER-SECRET-TOKEN")
	_ = prod.SendPasswordReset(context.Background(), "u@example.com", "ANOTHER-SECRET-TOKEN")
	if s := buf.String(); strings.Contains(s, "SECRET-TOKEN") || strings.Contains(s, "u@example.com") {
		t.Fatalf("production mailer logged sensitive data: %s", s)
	}
	// Development: hidden by default, printed only when a developer opts in.
	buf.Reset()
	dev := auth.LogMailer{}
	_ = dev.SendVerification(context.Background(), "u@example.com", "DEV-TOKEN")
	if s := buf.String(); strings.Contains(s, "DEV-TOKEN") || strings.Contains(s, "u@example.com") {
		t.Fatalf("dev mailer logged a token or address without DEV_LOG_SECRETS=true: %s", s)
	}
	t.Setenv("DEV_LOG_SECRETS", "true")
	_ = dev.SendVerification(context.Background(), "u@example.com", "DEV-TOKEN")
	if !strings.Contains(buf.String(), "DEV-TOKEN") {
		t.Fatal("with DEV_LOG_SECRETS=true the dev mailer should log the token so developers can verify accounts")
	}
	buf.Reset()
	t.Setenv("DEV_LOG_SECRETS", "true")
	_ = prod.SendVerification(context.Background(), "u@example.com", "SUPER-SECRET-TOKEN")
	if strings.Contains(buf.String(), "SUPER-SECRET-TOKEN") {
		t.Fatal("production must never log a token, even if DEV_LOG_SECRETS is set")
	}
}

// Sessions: a token minted for a deleted/banned account stops working promptly.
func TestAuditBannedUserLosesAccessAtRefresh(t *testing.T) {
	e := setup(t)
	u := e.user("Soon Banned")
	tokens := auth.NewTokenStore(e.rdb.Client, time.Hour)
	refresh, _, _ := tokens.Issue(context.Background(), u, "")
	e.exec(`UPDATE users SET is_banned=TRUE WHERE id=$1`, u)
	c, _ := e.call("POST", "/auth/refresh", map[string]string{"refresh_token": refresh}, "anon")
	if c != http.StatusUnauthorized {
		t.Fatalf("banned user refreshed a session: %d", c)
	}
}
