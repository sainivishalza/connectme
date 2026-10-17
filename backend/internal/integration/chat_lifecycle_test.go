package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"connectme/internal/auth"
	"connectme/internal/geo"
	"connectme/internal/users"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestKeyDirectory(t *testing.T) {
	e := needChat(t)
	a, b, mid := e.matched()
	stranger := e.user("Stranger")
	key := func(seed byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{seed}, 33)) }
	bundle := map[string]any{
		"registration_id": 1234, "identity_key": key(1), "signed_prekey_id": 1,
		"signed_prekey": key(2), "signed_prekey_sig": key(3),
		"one_time_prekeys": []map[string]any{{"key_id": 1, "public_key": key(10)}, {"key_id": 2, "public_key": key(11)}},
	}
	if c, _ := e.call("POST", "/chat/keys", bundle, a); c != 204 {
		t.Fatalf("upload: %d", c)
	}
	if c, _ := e.call("POST", "/chat/keys", map[string]any{"registration_id": 1, "identity_key": "!!", "signed_prekey_id": 1,
		"signed_prekey": key(2), "signed_prekey_sig": key(3)}, a); c != 422 {
		t.Fatalf("garbage key accepted: %d", c)
	}
	if _, body := e.call("GET", "/chat/keys/count", nil, a); !strings.Contains(string(body), `"one_time_prekeys":2`) {
		t.Fatalf("count: %s", body)
	}

	// A requester consumes one one-time prekey per conversation per day, so a
	// matched person cannot drain someone's supply by fetching in a loop; the
	// bundle itself is always served.
	var ids []int
	for i := 0; i < 3; i++ {
		c, body := e.call("GET", "/chat/keys/"+mid, nil, b)
		var r struct {
			IdentityKey string `json:"identity_key"`
			PreKey      *struct {
				KeyID int `json:"key_id"`
			} `json:"prekey"`
		}
		decodeInto(t, body, &r)
		if c != 200 || r.IdentityKey != key(1) {
			t.Fatalf("fetch %d: %d %s", i, c, body)
		}
		if r.PreKey != nil {
			ids = append(ids, r.PreKey.KeyID)
		}
	}
	if len(ids) != 1 {
		t.Fatalf("only the first fetch may consume a prekey, got %v", ids)
	}
	if _, body := e.call("GET", "/chat/keys/count", nil, a); !strings.Contains(string(body), `"one_time_prekeys":1`) {
		t.Fatalf("repeat fetches drained prekeys: %s", body)
	}
	// Another person's fetch has its own allowance, and each prekey is single-use.
	e.exec(`DELETE FROM prekey_fetches`)
	c2, body2 := e.call("GET", "/chat/keys/"+mid, nil, b)
	var r2 struct {
		PreKey *struct {
			KeyID int `json:"key_id"`
		} `json:"prekey"`
	}
	decodeInto(t, body2, &r2)
	if c2 != 200 || r2.PreKey == nil || r2.PreKey.KeyID == ids[0] {
		t.Fatalf("next allowance: %d %s", c2, body2)
	}
	// Non-participants cannot obtain someone's keys; you cannot fetch your own via the match.
	if c, _ := e.call("GET", "/chat/keys/"+mid, nil, stranger); c != 404 {
		t.Fatalf("stranger fetch: %d", c)
	}
}

func TestHotelRetentionPurgesChatAndMedia(t *testing.T) {
	e := needChat(t)
	ctx := context.Background()
	lat, lng := randomBase()
	hotelID := e.hotel(lat, lng)
	u1, u2 := e.user("H1"), e.user("H2")
	e.locate(u1, lat, lng)
	e.locate(u2, lat+0.0002, lng)
	e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u1)
	e.call("POST", "/geo/checkin", map[string]string{"hotel_id": hotelID}, u2)
	e.swipe(u1, e.refOf(u2), "like")
	_, r := e.swipe(u2, e.refOf(u1), "like")
	if !r.Matched {
		t.Fatal("expected hotel match")
	}
	e.send(u1, r.MatchID, map[string]any{"type": "text", "ciphertext": ct("hotel secret")})
	_, body := e.raw("POST", "/chat/matches/"+r.MatchID+"/media", []byte("blob"), u1)
	var up struct {
		MediaID string `json:"media_id"`
	}
	decodeInto(t, body, &up)
	e.send(u1, r.MatchID, map[string]any{"type": "voice", "ciphertext": ct("k"), "media_id": up.MediaID})

	coll := e.deps.Mongo.Collection("chat_messages")
	if n, _ := coll.CountDocuments(ctx, bson.M{"match_id": r.MatchID, "checkin_id": bson.M{"$exists": true}}); n != 2 {
		t.Fatalf("hotel messages must carry the check-in id, got %d", n)
	}

	e.call("POST", "/geo/checkout", nil, u1)
	e.exec(`UPDATE hotel_checkins SET auto_delete_at = NOW() - INTERVAL '1 minute' WHERE user_id=$1`, u1)
	svc := geo.NewService(e.db, users.NewService(e.db, nil, auth.StubCSAMScanner{}), e.chat)
	if res, err := svc.RunOnce(ctx); err != nil || res.Purged < 1 {
		t.Fatalf("purge: %+v %v", res, err)
	}
	if n, _ := coll.CountDocuments(ctx, bson.M{"match_id": r.MatchID}); n != 0 {
		t.Fatalf("%d chat messages survived the hotel purge", n)
	}
	if n, _ := e.deps.Mongo.Collection("chat_media").CountDocuments(ctx, bson.M{"match_id": r.MatchID}); n != 0 {
		t.Fatal("chat media records survived the hotel purge")
	}
	if c, _ := e.raw("GET", "/chat/media/"+up.MediaID, nil, u2); c != 404 {
		t.Fatalf("purged media still downloadable: %d", c)
	}
}

func TestUnmatchAndDeleteWipeConversation(t *testing.T) {
	e := needChat(t)
	ctx := context.Background()
	a, b, mid := e.matched()
	e.send(a, mid, map[string]any{"type": "text", "ciphertext": ct("one")})
	if c, _ := e.call("DELETE", "/chat/matches/"+mid, nil, b); c != 204 {
		t.Fatalf("delete conversation: %d", c)
	}
	if n := len(e.list(a, mid)); n != 0 {
		t.Fatalf("both sides must lose the conversation, a still sees %d", n)
	}

	// After an unmatch the chat is inaccessible and the sweeper removes leftovers.
	e.send(a, mid, map[string]any{"type": "text", "ciphertext": ct("two")})
	if c, _ := e.call("DELETE", "/matching/matches/"+mid, nil, a); c != 204 {
		t.Fatalf("unmatch: %d", c)
	}
	if c, _ := e.send(a, mid, map[string]any{"type": "text", "ciphertext": ct("three")}); c != 404 {
		t.Fatalf("send after unmatch: %d", c)
	}
	if _, err := e.chat.RunJob(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.deps.Mongo.Collection("chat_messages").CountDocuments(ctx, bson.M{"match_id": mid}); n != 0 {
		t.Fatalf("%d messages left after unmatch sweep", n)
	}
}
