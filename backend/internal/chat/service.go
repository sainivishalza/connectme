package chat

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"connectme/internal/push"
	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Service struct {
	db       *pgxpool.Pool
	messages *mongo.Collection
	media    *mongo.Collection
	blobs    users.MediaStore
	notifier push.Notifier
	now      func() time.Time
}

// SetNotifier enables push notifications (nil keeps them off).
func (s *Service) SetNotifier(n push.Notifier) { s.notifier = n }

func NewService(db *pgxpool.Pool, mdb *mongo.Database, blobs users.MediaStore) *Service {
	return &Service{db: db, messages: mdb.Collection("chat_messages"),
		media: mdb.Collection("chat_media"), blobs: blobs, now: time.Now}
}

// EnsureIndexes creates the Mongo indexes. Safe to call on every start.
func (s *Service) EnsureIndexes(ctx context.Context) error {
	_, err := s.messages.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "match_id", Value: 1}, {Key: "_id", Value: 1}}},
		{Keys: bson.D{{Key: "burn_at", Value: 1}}, Options: options.Index().SetSparse(true)},
		{Keys: bson.D{{Key: "checkin_id", Value: 1}}, Options: options.Index().SetSparse(true)},
	})
	if err != nil {
		return err
	}
	_, err = s.media.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "checkin_id", Value: 1}}, Options: options.Index().SetSparse(true)},
		{Keys: bson.D{{Key: "match_id", Value: 1}}},
	})
	return err
}

// participant authorizes a user for a conversation. A conversation id is
// either a 1:1 match or a matched pair of groups (merged group chat); both are
// UUIDs from different tables, so one lookup order is unambiguous. For hotel
// matches it returns the check-in the data is bound to.
func (s *Service) participant(ctx context.Context, userID, matchID string) (checkinID string, err error) {
	if _, perr := uuid.Parse(matchID); perr != nil {
		return "", ErrNotFound
	}
	var ck *string
	err = s.db.QueryRow(ctx, `
		SELECT m.checkin_id::text
		  FROM matches m
		  JOIN users a ON a.id = m.user1_id JOIN users b ON b.id = m.user2_id
		 WHERE m.id = $1::uuid AND m.status = 'matched' AND ($2::uuid IN (m.user1_id, m.user2_id))
		   AND (m.expires_at IS NULL OR m.expires_at > NOW())
		   AND NOT a.is_banned AND NOT b.is_banned`, matchID, userID).Scan(&ck)
	if err == nil {
		if ck != nil {
			checkinID = *ck
		}
		return checkinID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	// Merged group chat: any current member of either matched group.
	var ok bool
	err = s.db.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM group_matches gm
		    JOIN group_members gmem ON gmem.group_id IN (gm.group_a, gm.group_b)
		    JOIN users u ON u.id = gmem.user_id
		   WHERE gm.id = $1::uuid AND gm.status = 'matched'
		     AND gmem.user_id = $2::uuid AND NOT u.is_banned)`, matchID, userID).Scan(&ok)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNotFound
	}
	return "", nil
}

// Access reports whether the user may use the match's chat (used by realtime).
func (s *Service) Access(ctx context.Context, userID, matchID string) error {
	_, err := s.participant(ctx, userID, matchID)
	return err
}

// Send stores an already-encrypted message. Content is never inspected: only
// its envelope (size, encoding, type, burn timer) is validated.
func (s *Service) Send(ctx context.Context, userID, matchID string, req SendRequest) (*Message, error) {
	if req.Type != TypeText && req.Type != TypeImage && req.Type != TypeVoice {
		return nil, ValidationError{"type must be text, image or voice"}
	}
	raw, err := base64.StdEncoding.DecodeString(req.Ciphertext)
	if err != nil || len(raw) == 0 {
		return nil, ValidationError{"ciphertext must be base64"}
	}
	if len(raw) > MaxCiphertextBytes {
		return nil, ErrTooLarge
	}
	if !allowedBurn[req.BurnAfterS] {
		return nil, ValidationError{"burn_after_s must be one of 0, 5, 10, 30, 60, 300, 3600, 86400"}
	}
	if (req.Type == TypeText) != (req.MediaID == "") {
		return nil, ValidationError{"media_id is required for image/voice and not allowed for text"}
	}
	checkinID, err := s.participant(ctx, userID, matchID)
	if err != nil {
		return nil, err
	}
	if req.MediaID != "" {
		res, err := s.media.UpdateOne(ctx,
			bson.M{"_id": req.MediaID, "match_id": matchID, "uploader_id": userID, "attached": false},
			bson.M{"$set": bson.M{"attached": true}})
		if err != nil {
			return nil, err
		}
		if res.MatchedCount == 0 {
			return nil, ValidationError{"unknown or already used media_id"}
		}
	}
	doc := messageDoc{
		MatchID: matchID, CheckinID: checkinID, SenderID: userID, Type: string(req.Type),
		Ciphertext: req.Ciphertext, MediaID: req.MediaID, BurnAfterS: req.BurnAfterS,
		CreatedAt: s.now().UTC().Truncate(time.Millisecond),
	}
	res, err := s.messages.InsertOne(ctx, doc)
	if err != nil {
		return nil, err
	}
	doc.ID = res.InsertedID.(bson.ObjectID)
	m := toMessage(doc, userID)
	s.notifyOthers(ctx, userID, matchID)
	return &m, nil
}

// List returns up to 50 messages, oldest first, older than `before` (an id).
// Burned messages are filtered here as well as deleted by the job.
func (s *Service) List(ctx context.Context, userID, matchID, before string) ([]Message, error) {
	if _, err := s.participant(ctx, userID, matchID); err != nil {
		return nil, err
	}
	filter := bson.M{"match_id": matchID, "$or": bson.A{
		bson.M{"burn_at": bson.M{"$exists": false}},
		bson.M{"burn_at": bson.M{"$gt": s.now()}},
	}}
	if before != "" {
		oid, err := bson.ObjectIDFromHex(before)
		if err != nil {
			return nil, ValidationError{"invalid cursor"}
		}
		filter["_id"] = bson.M{"$lt": oid}
	}
	cur, err := s.messages.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(messagesPerPage))
	if err != nil {
		return nil, err
	}
	var docs []messageDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(docs))
	for i := len(docs) - 1; i >= 0; i-- {
		out = append(out, toMessage(docs[i], userID))
	}
	return out, nil
}

// MarkRead marks the other person's unread messages as read and starts the
// burn countdown for those that have one. Returns how many were marked.
func (s *Service) MarkRead(ctx context.Context, userID, matchID string) (int64, time.Time, error) {
	if _, err := s.participant(ctx, userID, matchID); err != nil {
		return 0, time.Time{}, err
	}
	now := s.now().UTC()
	filter := bson.M{"match_id": matchID, "sender_id": bson.M{"$ne": userID}, "read_at": bson.M{"$exists": false}}
	// Two passes so burn_at = read time + each message's own timer.
	burning, err := s.messages.UpdateMany(ctx,
		bson.M{"match_id": matchID, "sender_id": bson.M{"$ne": userID}, "read_at": bson.M{"$exists": false},
			"burn_after_s": bson.M{"$gt": 0}},
		mongo.Pipeline{
			{{Key: "$set", Value: bson.M{
				"read_at": now,
				"burn_at": bson.M{"$dateAdd": bson.M{"startDate": now, "unit": "second", "amount": "$burn_after_s"}},
			}}},
		})
	if err != nil {
		return 0, now, err
	}
	rest, err := s.messages.UpdateMany(ctx, filter, bson.M{"$set": bson.M{"read_at": now}})
	if err != nil {
		return 0, now, err
	}
	return burning.ModifiedCount + rest.ModifiedCount, now, nil
}

// DeleteConversation hard-deletes every message and media blob of a match,
// for both participants (E2E ciphertext cannot be deleted "for one side").
func (s *Service) DeleteConversation(ctx context.Context, userID, matchID string) error {
	if _, err := s.participant(ctx, userID, matchID); err != nil {
		return err
	}
	return s.deleteByFilter(ctx, bson.M{"match_id": matchID})
}

// PurgeCheckin implements geo.Purger: everything from a hotel stay.
func (s *Service) PurgeCheckin(ctx context.Context, checkinID string) error {
	return s.deleteByFilter(ctx, bson.M{"checkin_id": checkinID})
}

// deleteByFilter removes media blobs first, then the documents. A failure
// leaves documents in place so the caller (retention job) retries.
func (s *Service) deleteByFilter(ctx context.Context, filter bson.M) error {
	cur, err := s.media.Find(ctx, filter)
	if err != nil {
		return err
	}
	var ms []mediaDoc
	if err := cur.All(ctx, &ms); err != nil {
		return err
	}
	for _, m := range ms {
		if err := s.blobs.Delete(ctx, m.ID); err != nil {
			return err
		}
	}
	if _, err := s.media.DeleteMany(ctx, filter); err != nil {
		return err
	}
	_, err = s.messages.DeleteMany(ctx, filter)
	return err
}

func toMessage(d messageDoc, viewerID string) Message {
	return Message{
		ID: d.ID.Hex(), MatchID: d.MatchID, Type: MessageType(d.Type), Ciphertext: d.Ciphertext,
		MediaID: d.MediaID, BurnAfterS: d.BurnAfterS, CreatedAt: d.CreatedAt, ReadAt: d.ReadAt,
		FromMe: d.SenderID == viewerID, SenderToken: token(d.SenderID, d.MatchID),
	}
}

// notifyOthers tells the other people in a conversation there is something
// new. The notification says nothing about the message (it is encrypted, and
// the sender may be anonymous); it only carries the conversation id.
func (s *Service) notifyOthers(ctx context.Context, senderID, convID string) {
	if s.notifier == nil {
		return
	}
	var others []string
	rows, err := s.db.Query(ctx, `
		SELECT CASE WHEN user1_id = $2::uuid THEN user2_id ELSE user1_id END::text
		  FROM matches WHERE id = $1::uuid`, convID, senderID)
	if err == nil {
		for rows.Next() {
			var o string
			if rows.Scan(&o) == nil {
				others = append(others, o)
			}
		}
		rows.Close()
	}
	if len(others) == 0 {
		members, err := s.groupMembers(ctx, convID)
		if err != nil {
			return
		}
		for _, m := range members {
			if m != senderID {
				others = append(others, m)
			}
		}
	}
	for _, o := range others {
		s.notifier.Notify(ctx, o, push.KindMessage, convID)
	}
}

// Unsend deletes one of the caller's own messages, and its attachment, for
// everyone. Only the sender can; a message is gone from the server at once,
// and the other person's app removes its local copy when it next syncs (or
// immediately, when it receives the sender's "unsend" notice, which is itself
// an encrypted message the server cannot read).
func (s *Service) Unsend(ctx context.Context, userID, matchID, messageID string) error {
	if _, err := s.participant(ctx, userID, matchID); err != nil {
		return err
	}
	oid, err := bson.ObjectIDFromHex(messageID)
	if err != nil {
		return ErrNotFound
	}
	filter := bson.M{"_id": oid, "match_id": matchID, "sender_id": userID}
	var doc messageDoc
	if err := s.messages.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrNotFound
		}
		return err
	}
	if doc.MediaID != "" {
		if err := s.blobs.Delete(ctx, doc.MediaID); err != nil {
			return err
		}
		if _, err := s.media.DeleteOne(ctx, bson.M{"_id": doc.MediaID}); err != nil {
			return err
		}
	}
	_, err = s.messages.DeleteOne(ctx, filter)
	return err
}
