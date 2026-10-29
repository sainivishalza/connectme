package integration

import (
	"connectme/internal/csam"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectme/internal/idcheck"
	"connectme/internal/users"
	"connectme/pkg/middleware"
)

func (e *env) idSubmit(uid string, id, selfie []byte) (int, string) {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if id != nil {
		fw, _ := mw.CreateFormFile("id_photo", "id.jpg")
		fw.Write(id)
	}
	if selfie != nil {
		fw, _ := mw.CreateFormFile("selfie", "me.jpg")
		fw.Write(selfie)
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/idcheck/submit", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	tok, _ := middleware.SignAccessToken([]byte(secret), uid, "user", time.Minute)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1", rand.Intn(256), rand.Intn(256), rand.Intn(256))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (e *env) sweepIDChecks() error {
	st := users.LocalStore{Dir: filepath.Join(e.deps.Cfg.MediaDir, "idcheck")}
	return idcheck.NewService(e.db, st, csam.Stub{}).Sweep(context.Background())
}

func idFilesOnDisk(e *env) int {
	n := 0
	_ = filepath.Walk(e.deps.Cfg.MediaDir, func(p string, i os.FileInfo, err error) error {
		if err == nil && !i.IsDir() && strings.Contains(filepath.ToSlash(p), "/idcheck/") {
			n++
		}
		return nil
	})
	return n
}

func TestIDCheckIsReviewedByAHumanAndImagesAreDeleted(t *testing.T) {
	e := setup(t)
	member, other := e.user("Applicant"), e.user("Other")
	mod := e.newUser("moderator", true, "Reviewer")
	img := jpegBytesFor(t)

	// Nothing to submit until a check is started.
	if c, _ := e.idSubmit(member, img, img); c != 422 {
		t.Errorf("submit without start: %d", c)
	}
	c, b := e.call("GET", "/idcheck/status", nil, member)
	if c != 200 || !strings.Contains(string(b), `"state":"none"`) {
		t.Fatalf("status: %d %s", c, b)
	}
	c, b = e.call("POST", "/idcheck/start", nil, member)
	var st struct{ State, Code string }
	_ = json.Unmarshal(b, &st)
	if c != 200 || st.State != "awaiting" || len(st.Code) != 4 {
		t.Fatalf("start: %d %s", c, b)
	}
	// Starting again returns the same open code.
	_, b2 := e.call("POST", "/idcheck/start", nil, member)
	if !strings.Contains(string(b2), st.Code) {
		t.Errorf("second start changed the code: %s vs %s", b2, st.Code)
	}

	// Input checks: both files, real images only.
	if c, _ := e.idSubmit(member, img, nil); c != 400 {
		t.Errorf("missing selfie: %d", c)
	}
	if c, _ := e.idSubmit(member, []byte("not an image at all"), img); c != 415 {
		t.Errorf("fake image: %d", c)
	}
	if c, b := e.idSubmit(member, img, img); c != 200 || !strings.Contains(b, "pending") {
		t.Fatalf("submit: %d %s", c, b)
	}
	if idFilesOnDisk(e) != 2 {
		t.Errorf("expected 2 stored images, found %d", idFilesOnDisk(e))
	}
	if c, _ := e.idSubmit(member, img, img); c != 422 {
		t.Errorf("a second submission while pending: %d", c)
	}
	if c, _ := e.call("POST", "/idcheck/start", nil, member); c != 422 {
		t.Errorf("start while pending: %d", c)
	}

	// Members cannot reach the queue, the images or the decision.
	var queue struct {
		Items []struct {
			ID          string `json:"id"`
			Code        string `json:"code"`
			DisplayName string `json:"display_name"`
			DeclaredDOB string `json:"declared_birth_date"`
		} `json:"items"`
	}
	for _, p := range []struct{ m, path string }{{"GET", "/idcheck/queue"}, {"POST", "/idcheck/00000000-0000-0000-0000-000000000000/decision"}, {"GET", "/idcheck/00000000-0000-0000-0000-000000000000/image/selfie"}} {
		if c, _ := e.call(p.m, p.path, nil, other); c != 403 {
			t.Errorf("member %s %s: %d", p.m, p.path, c)
		}
	}
	c, b = e.callAs("GET", "/idcheck/queue", nil, mod, "moderator")
	_ = json.Unmarshal(b, &queue)
	var item = -1
	for i, q := range queue.Items {
		if q.Code == st.Code && q.DisplayName == "Applicant" {
			item = i
		}
	}
	if c != 200 || item < 0 || queue.Items[item].DeclaredDOB == "" {
		t.Fatalf("queue: %d %s", c, b)
	}
	id := queue.Items[item].ID

	// Viewing an image works for staff, is never cached, and leaves an audit entry.
	var before, after int
	_ = e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM staff_audit_log WHERE action='view_id_check'`).Scan(&before)
	rec := e.fire("GET", "/idcheck/"+id+"/image/id_photo", nil, e.tokenFor(mod, "moderator"), nil)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" || !bytes.Equal(rec.Body.Bytes(), img) {
		t.Errorf("image: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	_ = e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM staff_audit_log WHERE action='view_id_check'`).Scan(&after)
	if after != before+1 {
		t.Errorf("image view not audited: %d -> %d", before, after)
	}
	if c, _ := e.callAs("GET", "/idcheck/"+id+"/image/passport", nil, mod, "moderator"); c != 404 {
		t.Errorf("unknown image kind: %d", c)
	}

	// A decision needs a valid action and, for rejection, a reason.
	for name, body := range map[string]map[string]string{
		"action": {"action": "maybe"}, "no reason": {"action": "reject"}, "bad reason": {"action": "reject", "reason": "dislike"},
	} {
		if c, _ := e.callAs("POST", "/idcheck/"+id+"/decision", body, mod, "moderator"); c != 422 {
			t.Errorf("%s accepted: %d", name, c)
		}
	}
	// Reject first: the member sees why and the images are gone.
	if c, b := e.callAs("POST", "/idcheck/"+id+"/decision", map[string]string{"action": "reject", "reason": "face_mismatch"}, mod, "moderator"); c != 204 {
		t.Fatalf("reject: %d %s", c, b)
	}
	if n := idFilesOnDisk(e); n != 0 {
		t.Errorf("%d images left after a decision", n)
	}
	if c, _ := e.callAs("POST", "/idcheck/"+id+"/decision", map[string]string{"action": "approve"}, mod, "moderator"); c != 404 {
		t.Errorf("deciding twice: %d", c)
	}
	_, b = e.call("GET", "/idcheck/status", nil, member)
	if !strings.Contains(string(b), `"state":"rejected"`) || !strings.Contains(string(b), "face_mismatch") {
		t.Errorf("status after reject: %s", b)
	}

	// Try again; this time approve.
	_, b = e.call("POST", "/idcheck/start", nil, member)
	_ = json.Unmarshal(b, &st)
	if c, b := e.idSubmit(member, img, img); c != 200 {
		t.Fatalf("resubmit: %d %s", c, b)
	}
	_, b = e.callAs("GET", "/idcheck/queue", nil, mod, "moderator")
	queue.Items = nil
	_ = json.Unmarshal(b, &queue)
	for _, q := range queue.Items {
		if q.Code == st.Code {
			id = q.ID
		}
	}
	if c, b := e.callAs("POST", "/idcheck/"+id+"/decision", map[string]string{"action": "approve"}, mod, "moderator"); c != 204 {
		t.Fatalf("approve: %d %s", c, b)
	}
	_, b = e.call("GET", "/idcheck/status", nil, member)
	if !strings.Contains(string(b), `"state":"verified"`) {
		t.Errorf("status after approve: %s", b)
	}
	if n := idFilesOnDisk(e); n != 0 {
		t.Errorf("%d images left after approval", n)
	}
	if c, _ := e.call("POST", "/idcheck/start", nil, member); c != 422 {
		t.Errorf("starting when already verified: %d", c)
	}
	var shown bool
	_, me := e.call("GET", "/users/me", nil, member)
	shown = strings.Contains(string(me), `"id_verified":true`)
	if !shown {
		t.Errorf("profile does not show the verification: %s", me)
	}
}

func TestIDCheckRulesExpiryErasureAndNoSelfReview(t *testing.T) {
	e := setup(t)
	mod := e.newUser("moderator", true, "SelfReviewer")
	img := jpegBytesFor(t)

	// A moderator cannot approve their own document.
	_, b := e.call("POST", "/idcheck/start", nil, mod)
	if !strings.Contains(string(b), "awaiting") {
		t.Fatalf("start: %s", b)
	}
	if c, _ := e.idSubmit(mod, img, img); c != 200 {
		t.Fatal("submit")
	}
	var own string
	_ = e.db.QueryRow(context.Background(), `SELECT id::text FROM id_checks WHERE user_id=$1::uuid`, mod).Scan(&own)
	if c, _ := e.callAs("POST", "/idcheck/"+own+"/decision", map[string]string{"action": "approve"}, mod, "moderator"); c != 403 {
		t.Errorf("self approval: %d", c)
	}
	if c, _ := e.callAs("GET", "/idcheck/"+own+"/image/selfie", nil, mod, "moderator"); c != 403 {
		t.Errorf("self view: %d", c)
	}

	// An expired code cannot be used.
	late := e.user("Late")
	e.call("POST", "/idcheck/start", nil, late)
	e.exec(`UPDATE id_checks SET expires_at = NOW() - INTERVAL '1 minute' WHERE user_id=$1::uuid`, late)
	if c, _ := e.idSubmit(late, img, img); c != 422 {
		t.Errorf("expired code accepted: %d", c)
	}
	if c, b := e.call("POST", "/idcheck/start", nil, late); c != 200 || !strings.Contains(string(b), "awaiting") {
		t.Errorf("new code after expiry: %d %s", c, b)
	}

	// Three rejections in a day stop further attempts until tomorrow.
	spam := e.user("Spammer")
	for i := 0; i < 3; i++ {
		e.exec(`INSERT INTO id_checks (user_id, status, code, expires_at, reviewed_at, reject_reason) VALUES ($1::uuid,'rejected','1234',NOW(),NOW(),'unreadable')`, spam)
	}
	if c, b := e.call("POST", "/idcheck/start", nil, spam); c != 422 || !strings.Contains(string(b), "tomorrow") {
		t.Errorf("rejection limit: %d %s", c, b)
	}

	// A pending check that nobody reviews expires and its images are removed.
	slow := e.user("Waiting")
	e.call("POST", "/idcheck/start", nil, slow)
	if c, _ := e.idSubmit(slow, img, img); c != 200 {
		t.Fatal("submit")
	}
	e.exec(`UPDATE id_checks SET submitted_at = NOW() - INTERVAL '8 days' WHERE user_id=$1::uuid`, slow)
	before := idFilesOnDisk(e)
	if err := e.sweepIDChecks(); err != nil {
		t.Fatal(err)
	}
	if after := idFilesOnDisk(e); after != before-2 {
		t.Errorf("stale images not removed: %d -> %d", before, after)
	}
	_, b = e.call("GET", "/idcheck/status", nil, slow)
	if !strings.Contains(string(b), `"state":"none"`) {
		t.Errorf("status after expiry: %s", b)
	}

	// Erasing the account removes the images that are still stored.
	gone, _ := e.userWithPassword("Eraser")
	e.call("POST", "/idcheck/start", nil, gone)
	if c, _ := e.idSubmit(gone, img, img); c != 200 {
		t.Fatal("submit")
	}
	before = idFilesOnDisk(e)
	if c := e.erase(gone, erasePW, "DELETE"); c != 204 && c != 200 {
		t.Fatalf("erase: %d", c)
	}
	if after := idFilesOnDisk(e); after != before-2 {
		t.Errorf("erasure left ID images: %d -> %d", before, after)
	}
}
