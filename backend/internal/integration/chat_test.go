package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func needChat(t *testing.T) *env {
	t.Helper()
	e := setup(t)
	if e.chat == nil {
		t.Skip("TEST_MONGO_URL not set")
	}
	if err := e.chat.EnsureIndexes(context.Background()); err != nil {
		t.Fatal(err)
	}
	return e
}

func ct(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

type msg struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	Ciphertext string     `json:"ciphertext"`
	MediaID    string     `json:"media_id"`
	FromMe     bool       `json:"from_me"`
	ReadAt     *time.Time `json:"read_at"`
}

func (e *env) send(uid, matchID string, body map[string]any) (int, msg) {
	c, b := e.call("POST", "/chat/matches/"+matchID+"/messages", body, uid)
	var m msg
	_ = json.Unmarshal(b, &m)
	return c, m
}

func (e *env) list(uid, matchID string) []msg {
	e.t.Helper()
	c, b := e.call("GET", "/chat/matches/"+matchID+"/messages", nil, uid)
	if c != 200 {
		e.t.Fatalf("list: %d %s", c, b)
	}
	var r struct{ Messages []msg }
	decodeInto(e.t, b, &r)
	return r.Messages
}

func TestSendListAndAuthorization(t *testing.T) {
	e := needChat(t)
	a, b, mid := e.matched()
	stranger := e.user("Stranger")

	secret := "meet me at 8 — definitely plaintext"
	c, m := e.send(a, mid, map[string]any{"type": "text", "ciphertext": ct("opaque-bytes-1")})
	if c != 201 || !m.FromMe || m.Ciphertext != ct("opaque-bytes-1") {
		t.Fatalf("send: %d %+v", c, m)
	}
	e.send(b, mid, map[string]any{"type": "text", "ciphertext": ct("opaque-bytes-2")})

	// The server stores exactly the envelope: no plaintext/content field exists.
	var doc bson.M
	oid, _ := bson.ObjectIDFromHex(m.ID)
	if err := e.deps.Mongo.Collection("chat_messages").FindOne(context.Background(), bson.M{"_id": oid}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	for k := range doc {
		if k == "content" || k == "text" || k == "plaintext" || k == "body" {
			t.Fatalf("unexpected readable field %q stored", k)
		}
	}
	if strings.Contains(fmt.Sprint(doc), secret) {
		t.Fatal("plaintext found in storage")
	}

	// from_me is per viewer; ordering is oldest first.
	la, lb := e.list(a, mid), e.list(b, mid)
	if len(la) != 2 || !la[0].FromMe || la[1].FromMe || lb[0].FromMe || !lb[1].FromMe {
		t.Fatalf("from_me wrong: a=%+v b=%+v", la, lb)
	}

	// Envelope validation.
	bad := map[string]map[string]any{
		"not base64":     {"type": "text", "ciphertext": "***"},
		"empty":          {"type": "text", "ciphertext": ""},
		"gift via chat":  {"type": "gift", "ciphertext": ct("x")},
		"bad burn":       {"type": "text", "ciphertext": ct("x"), "burn_after_s": 7},
		"text w/ media":  {"type": "text", "ciphertext": ct("x"), "media_id": "abc"},
		"image no media": {"type": "image", "ciphertext": ct("x")},
	}
	for name, body := range bad {
		if c, _ := e.send(a, mid, body); c != 422 {
			t.Errorf("%s: got %d want 422", name, c)
		}
	}
	big := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 20<<10))
	if c, _ := e.send(a, mid, map[string]any{"type": "text", "ciphertext": big}); c != 413 && c != 422 {
		t.Errorf("oversize: got %d", c)
	}

	// Outsiders get nothing; unknown fields are rejected.
	if c, _ := e.send(stranger, mid, map[string]any{"type": "text", "ciphertext": ct("x")}); c != 404 {
		t.Fatalf("stranger send: %d", c)
	}
	if c, _ := e.call("GET", "/chat/matches/"+mid+"/messages", nil, stranger); c != 404 {
		t.Fatalf("stranger list: %d", c)
	}
	if c, _ := e.call("POST", "/chat/matches/"+mid+"/messages", map[string]any{"type": "text", "ciphertext": ct("x"), "sender": "x"}, a); c != 400 {
		t.Fatalf("unknown field: %d", c)
	}
}

func TestSendRateLimit(t *testing.T) {
	e := needChat(t)
	a, _, mid := e.matched()
	for i := 1; i <= 30; i++ {
		if c, _ := e.send(a, mid, map[string]any{"type": "text", "ciphertext": ct("x")}); c != 201 {
			t.Fatalf("message %d: %d", i, c)
		}
	}
	if c, _ := e.send(a, mid, map[string]any{"type": "text", "ciphertext": ct("x")}); c != 429 {
		t.Fatalf("31st message in a minute: %d want 429", c)
	}
}

func TestBurnAfterReading(t *testing.T) {
	e := needChat(t)
	ctx := context.Background()
	a, b, mid := e.matched()
	_, m := e.send(a, mid, map[string]any{"type": "text", "ciphertext": ct("burn me"), "burn_after_s": 5})
	_, keep := e.send(a, mid, map[string]any{"type": "text", "ciphertext": ct("keep me")})

	// The timer starts only when the recipient reads.
	oid, _ := bson.ObjectIDFromHex(m.ID)
	coll := e.deps.Mongo.Collection("chat_messages")
	var before bson.M
	_ = coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&before)
	if _, has := before["burn_at"]; has {
		t.Fatal("burn_at must not be set before reading")
	}
	// Reading your own messages does not start the timer.
	e.call("POST", "/chat/matches/"+mid+"/read", nil, a)
	_ = coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&before)
	if _, has := before["burn_at"]; has {
		t.Fatal("sender reading must not start the burn timer")
	}

	c, body := e.call("POST", "/chat/matches/"+mid+"/read", nil, b)
	var rr struct{ Count int }
	decodeInto(t, body, &rr)
	if c != 200 || rr.Count != 2 {
		t.Fatalf("read: %d %s", c, body)
	}
	var after struct {
		BurnAt time.Time `bson:"burn_at"`
		ReadAt time.Time `bson:"read_at"`
	}
	if err := coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&after); err != nil {
		t.Fatal(err)
	}
	if d := after.BurnAt.Sub(after.ReadAt); d < 4*time.Second || d > 6*time.Second {
		t.Fatalf("burn_at - read_at = %v, want ~5s", d)
	}
	if n := len(e.list(b, mid)); n != 2 {
		t.Fatalf("before expiry both visible, got %d", n)
	}

	// Once the time passes the message is hidden at once and deleted by the job.
	if _, err := coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{"burn_at": time.Now().Add(-time.Second)}}); err != nil {
		t.Fatal(err)
	}
	msgs := e.list(b, mid)
	if len(msgs) != 1 || msgs[0].ID != keep.ID {
		t.Fatalf("burned message still visible: %+v", msgs)
	}
	if n, err := e.chat.RunJob(ctx); err != nil || n < 1 {
		t.Fatalf("job: %d %v", n, err)
	}
	if n, _ := coll.CountDocuments(ctx, bson.M{"_id": oid}); n != 0 {
		t.Fatal("burned message not deleted")
	}
}

func TestEncryptedMediaLifecycle(t *testing.T) {
	e := needChat(t)
	ctx := context.Background()
	a, b, mid := e.matched()
	stranger := e.user("Stranger")
	blob := bytes.Repeat([]byte{0xAB, 0xCD}, 5000)

	c, body := e.raw("POST", "/chat/matches/"+mid+"/media", blob, a)
	var up struct {
		MediaID string `json:"media_id"`
	}
	decodeInto(t, body, &up)
	if c != 201 || up.MediaID == "" {
		t.Fatalf("upload: %d %s", c, body)
	}
	if c, _ := e.raw("POST", "/chat/matches/"+mid+"/media", blob, stranger); c != 404 {
		t.Fatalf("stranger upload: %d", c)
	}
	if c, _ := e.raw("POST", "/chat/matches/"+mid+"/media", bytes.Repeat([]byte{1}, 9<<20), a); c != 413 {
		t.Fatalf("oversize media: %d", c)
	}
	// Only the uploader can attach it, and only once.
	if c, _ := e.send(b, mid, map[string]any{"type": "image", "ciphertext": ct("k"), "media_id": up.MediaID}); c != 422 {
		t.Fatalf("attaching someone else's media: %d", c)
	}
	c, m := e.send(a, mid, map[string]any{"type": "image", "ciphertext": ct("key-material"), "media_id": up.MediaID, "burn_after_s": 5})
	if c != 201 {
		t.Fatalf("send image: %d", c)
	}
	if c, _ := e.send(a, mid, map[string]any{"type": "image", "ciphertext": ct("k"), "media_id": up.MediaID}); c != 422 {
		t.Fatalf("reusing media: %d", c)
	}

	cd, got := e.raw("GET", "/chat/media/"+up.MediaID, nil, b)
	if cd != 200 || !bytes.Equal(got, blob) {
		t.Fatalf("download: %d (%d bytes)", cd, len(got))
	}
	if c, _ := e.raw("GET", "/chat/media/"+up.MediaID, nil, stranger); c != 404 {
		t.Fatalf("stranger download: %d", c)
	}

	// Burn removes the blob as well as the message.
	e.call("POST", "/chat/matches/"+mid+"/read", nil, b)
	oid, _ := bson.ObjectIDFromHex(m.ID)
	e.deps.Mongo.Collection("chat_messages").UpdateOne(ctx, bson.M{"_id": oid},
		bson.M{"$set": bson.M{"burn_at": time.Now().Add(-time.Second)}})
	if _, err := e.chat.RunJob(ctx); err != nil {
		t.Fatal(err)
	}
	if c, _ := e.raw("GET", "/chat/media/"+up.MediaID, nil, b); c != 404 {
		t.Fatalf("burned media still downloadable: %d", c)
	}
}
