package integration

import (
	"context"
	"encoding/json"
	"testing"

	"connectme/internal/users"
)

// Cards is the batch form of Card and must never differ from it: same people
// visible, same fields, same masking. This compares them across the cases that
// matter for privacy.
func TestCardsMatchCardForEveryRelationship(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	svc := users.NewService(e.db, users.LocalStore{Dir: t.TempDir()}, nil)

	lat, lng := randomBase()
	viewer := e.user("Viewer")
	open := e.user("Open Person")
	shy := e.user("Shy Person")
	matchedShy := e.user("Matched Shy")
	revealed := e.user("Revealed")
	blocked := e.user("Blocked Person")
	banned := e.user("Banned Person")
	unverified := e.newUser("user", false, "Unverified Person")
	e.exec(`UPDATE profiles SET mode='incognito' WHERE user_id = ANY($1::uuid[])`, []string{shy, matchedShy, revealed})
	for _, u := range []string{viewer, open, shy, matchedShy, revealed, blocked, banned, unverified} {
		e.locate(u, lat, lng)
	}
	e.exec(`UPDATE profiles SET tags='{hiking,jazz}', intro='hello' WHERE user_id = ANY($1::uuid[])`, []string{open, shy, matchedShy, revealed})
	e.exec(`UPDATE users SET is_banned=true WHERE id=$1`, banned)
	e.call("POST", "/moderation/blocks", map[string]string{"ref": e.refOf(blocked)}, viewer)

	// Two incognito matches, one with mutual reveal.
	for _, other := range []string{matchedShy, revealed} {
		e.swipe(viewer, e.refOf(other), "like")
		e.swipe(other, e.refOf(viewer), "like")
	}
	var m1 string
	if err := e.db.QueryRow(ctx, `SELECT id::text FROM matches WHERE user1_id=LEAST($1::uuid,$2::uuid) AND user2_id=GREATEST($1::uuid,$2::uuid)`, viewer, revealed).Scan(&m1); err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE matches SET mode='incognito' WHERE id=$1::uuid`, m1)
	e.exec(`INSERT INTO reveal_consents (match_id, user_id) VALUES ($1::uuid,$2), ($1::uuid,$3)`, m1, viewer, revealed)

	all := []string{viewer, open, shy, matchedShy, revealed, blocked, banned, unverified}
	for _, hotel := range []string{"", "Grand Hotel"} {
		batch, err := svc.Cards(ctx, viewer, all, hotel)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range all {
			single, serr := svc.Card(ctx, viewer, id, hotel)
			got, ok := batch[id]
			if serr != nil {
				if ok {
					t.Errorf("hotel=%q: batch shows %s but Card refuses: %v", hotel, id, serr)
				}
				continue
			}
			if !ok {
				t.Errorf("hotel=%q: batch hides %s but Card shows them", hotel, id)
				continue
			}
			a, _ := json.Marshal(single)
			b, _ := json.Marshal(got)
			if string(a) != string(b) {
				t.Errorf("hotel=%q: %s differs\n card:  %s\n cards: %s", hotel, id, a, b)
			}
		}
		if _, ok := batch[blocked]; ok {
			t.Error("blocked person in batch")
		}
		if _, ok := batch[banned]; ok {
			t.Error("banned person in batch")
		}
		if _, ok := batch[unverified]; ok {
			t.Error("unverified person in batch")
		}
	}
	if m, err := svc.Cards(ctx, viewer, nil, ""); err != nil || len(m) != 0 {
		t.Errorf("empty input: %v %v", m, err)
	}
}
