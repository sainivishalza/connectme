package integration

import (
	"strings"
	"testing"
)

func TestBannedMemberCanAppealAndStaffCanLiftOrUpholdTheBan(t *testing.T) {
	e := setup(t)
	mod := e.newUser("moderator", true, "Mod")
	user := e.user("Appealing Member")
	other := e.user("Another Banned")
	for _, u := range []string{user, other} {
		e.exec(`UPDATE users SET password_hash = crypt('Correct-Horse-9!', gen_salt('bf')), is_banned = TRUE WHERE id=$1::uuid`, u)
	}
	email := func(id string) string {
		var s string
		if err := e.db.QueryRow(t.Context(), `SELECT email FROM users WHERE id=$1::uuid`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	appeal := func(mail, pw, msg string) int {
		c, _ := e.call("POST", "/moderation/appeals", map[string]string{"email": mail, "password": pw, "message": msg}, "")
		return c
	}
	msg := "I was reported by someone I had argued with; please look again."

	// Wrong password, unknown email and an account that is not banned all get the same refusal.
	if c := appeal(email(user), "wrong-password-1", msg); c != 403 {
		t.Errorf("wrong password: %d", c)
	}
	if c := appeal("nobody@example.com", "Correct-Horse-9!", msg); c != 403 {
		t.Errorf("unknown email: %d", c)
	}
	free := e.user("Not Banned")
	e.exec(`UPDATE users SET password_hash = crypt('Correct-Horse-9!', gen_salt('bf')) WHERE id=$1::uuid`, free)
	if c := appeal(email(free), "Correct-Horse-9!", msg); c != 403 {
		t.Errorf("not banned: %d", c)
	}
	if c := appeal(email(user), "Correct-Horse-9!", "short"); c != 422 {
		t.Errorf("too short: %d", c)
	}
	if c := appeal(email(user), "Correct-Horse-9!", msg); c != 202 {
		t.Fatalf("appeal: %d", c)
	}
	appeal(email(user), "Correct-Horse-9!", msg) // a second one while open changes nothing
	if n := e.count(`SELECT COUNT(*) FROM appeals WHERE user_id=$1::uuid`, user); n != 1 {
		t.Errorf("open appeals: %d", n)
	}
	if c := appeal(email(other), "Correct-Horse-9!", msg+" Second member."); c != 202 {
		t.Fatalf("second appeal: %d", c)
	}

	// Only staff read them, and they see no email.
	if c, _ := e.call("GET", "/moderation/appeals", nil, user); c != 403 {
		t.Errorf("appealing member read the queue: %d", c)
	}
	c, b := e.callAs("GET", "/moderation/appeals", nil, mod, "moderator")
	if c != 200 || !strings.Contains(string(b), e.refOf(user)) || strings.Contains(string(b), "@example.com") {
		t.Fatalf("queue: %d %s", c, b)
	}
	var id string
	e.db.QueryRow(t.Context(), `SELECT id::text FROM appeals WHERE user_id=$1::uuid`, user).Scan(&id)
	var id2 string
	e.db.QueryRow(t.Context(), `SELECT id::text FROM appeals WHERE user_id=$1::uuid`, other).Scan(&id2)

	if c, _ := e.callAs("POST", "/moderation/appeals/"+id+"/decide", map[string]string{"decision": "lift", "note": "ok"}, mod, "moderator"); c != 422 {
		t.Errorf("short note: %d", c)
	}
	if c, b := e.callAs("POST", "/moderation/appeals/"+id+"/decide", map[string]string{"decision": "lift", "note": "Reports were not upheld on review"}, mod, "moderator"); c != 204 {
		t.Fatalf("lift: %d %s", c, b)
	}
	if n := e.count(`SELECT COUNT(*) FROM users WHERE id=$1::uuid AND NOT is_banned`, user); n != 1 {
		t.Error("ban not lifted")
	}
	if c, _ := e.callAs("POST", "/moderation/appeals/"+id+"/decide", map[string]string{"decision": "lift", "note": "again please"}, mod, "moderator"); c != 404 {
		t.Errorf("deciding twice: %d", c)
	}
	if c, _ := e.callAs("POST", "/moderation/appeals/"+id2+"/decide", map[string]string{"decision": "uphold", "note": "Evidence of harassment stands"}, mod, "moderator"); c != 204 {
		t.Fatalf("uphold: %d", c)
	}
	if n := e.count(`SELECT COUNT(*) FROM users WHERE id=$1::uuid AND is_banned`, other); n != 1 {
		t.Error("upheld appeal unbanned the member")
	}
	if n := e.count(`SELECT COUNT(*) FROM staff_audit_log WHERE actor_id=$1::uuid AND action IN ('unban_user','appeal_upheld')`, mod); n != 2 {
		t.Errorf("audit entries: %d", n)
	}
}

func TestStaffSeeReportedMaskedRoomsTogether(t *testing.T) {
	e := setup(t)
	mod := e.newUser("moderator", true, "Mod")
	a, b, bad := e.user("Reporter One"), e.user("Reporter Two"), e.user("Reported One")
	room := "99999999-9999-9999-9999-999999999999"
	e.exec(`DELETE FROM reports WHERE ref_id=$1::uuid`, room)
	for _, who := range []string{a, b} {
		e.exec(`INSERT INTO reports (reporter_id, reported_id, reason, details, ref_kind, ref_id, evidence)
		        VALUES ($1::uuid, $2::uuid, 'harassment', 'masked room answer', 'masked_room', $3::uuid, '["REPORTED: x / rude line","Wolf: truth / hi"]')`, who, bad, room)
	}
	if c, _ := e.call("GET", "/moderation/rooms", nil, a); c != 403 {
		t.Errorf("member read reported rooms: %d", c)
	}
	c, body := e.callAs("GET", "/moderation/rooms", nil, mod, "moderator")
	if c != 200 || !strings.Contains(string(body), room) || !strings.Contains(string(body), `"reporters":2`) || !strings.Contains(string(body), "rude line") {
		t.Fatalf("rooms: %d %s", c, body)
	}
	e.exec(`UPDATE reports SET status='dismissed' WHERE ref_id=$1::uuid`, room)
	if _, body := e.callAs("GET", "/moderation/rooms", nil, mod, "moderator"); strings.Contains(string(body), room) {
		t.Error("a settled room is still listed")
	}
	e.exec(`DELETE FROM reports WHERE ref_id=$1::uuid`, room)
}
