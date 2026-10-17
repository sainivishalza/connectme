package chat

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// PurgeUser removes a user's chat data for account erasure: every 1:1
// conversation they were in (both sides lose it, as for an unmatch), their
// messages in group chats, and all attachments they uploaded. Idempotent.
func (s *Service) PurgeUser(ctx context.Context, userID string) error {
	rows, err := s.db.Query(ctx,
		`SELECT id::text FROM matches WHERE user1_id=$1::uuid OR user2_id=$1::uuid`, userID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) > 0 {
		if err := s.deleteByFilter(ctx, bson.M{"match_id": bson.M{"$in": ids}}); err != nil {
			return err
		}
	}
	// Group conversations belong to other people too: remove only this user's contributions.
	if err := s.deleteByFilter(ctx, bson.M{"uploader_id": userID}); err != nil {
		return err
	}
	_, err = s.messages.DeleteMany(ctx, bson.M{"sender_id": userID})
	return err
}
