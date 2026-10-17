package chat

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Counts returns how many messages each participant has sent in a match.
// Only counts are exposed; message content is ciphertext and never read.
func (s *Service) Counts(ctx context.Context, matchID, userA, userB string) (aToB, bToA int, err error) {
	a, err := s.messages.CountDocuments(ctx, bson.M{"match_id": matchID, "sender_id": userA})
	if err != nil {
		return 0, 0, err
	}
	b, err := s.messages.CountDocuments(ctx, bson.M{"match_id": matchID, "sender_id": userB})
	if err != nil {
		return 0, 0, err
	}
	return int(a), int(b), nil
}
