package chat

import (
	"context"
	"errors"
	"io"
	"time"

	"connectme/internal/users"
	"connectme/pkg/metrics"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// UploadMedia stores an opaque, client-encrypted attachment and returns its
// id, to be referenced by a later image/voice message. Because the blob is
// ciphertext the server cannot inspect or CSAM-scan it; that check must
// happen on the sender's device before encryption (see docs/DECISIONS.md).
func (s *Service) UploadMedia(ctx context.Context, userID, matchID string, body io.Reader) (string, error) {
	checkinID, err := s.participant(ctx, userID, matchID)
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxMediaBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", ValidationError{"empty attachment"}
	}
	if len(data) > MaxMediaBytes {
		return "", ErrTooLarge
	}
	id := uuid.NewString()
	if err := s.blobs.Put(ctx, id, data); err != nil {
		return "", err
	}
	_, err = s.media.InsertOne(ctx, mediaDoc{ID: id, MatchID: matchID, CheckinID: checkinID,
		UploaderID: userID, CreatedAt: s.now().UTC()})
	if err != nil {
		_ = s.blobs.Delete(ctx, id)
		return "", err
	}
	return id, nil
}

// OpenMedia returns an attachment to a participant of its match.
func (s *Service) OpenMedia(ctx context.Context, userID, mediaID string) ([]byte, error) {
	if _, err := uuid.Parse(mediaID); err != nil {
		return nil, ErrNotFound
	}
	var m mediaDoc
	err := s.media.FindOne(ctx, bson.M{"_id": mediaID}).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.participant(ctx, userID, m.MatchID); err != nil {
		return nil, err
	}
	b, err := s.blobs.Get(ctx, mediaID)
	if errors.Is(err, users.ErrNotFound) {
		return nil, ErrNotFound
	}
	return b, err
}

// RunJob is one pass of the retention job:
//   - delete burned messages (burn_at passed) and their attachments;
//   - delete attachments never attached to a message after an hour;
//   - delete messages/attachments of matches that are no longer active.
func (s *Service) RunJob(ctx context.Context) (burned int64, err error) {
	now := s.now()
	cur, err := s.messages.Find(ctx, bson.M{"burn_at": bson.M{"$lte": now}})
	if err != nil {
		return 0, err
	}
	var due []messageDoc
	if err := cur.All(ctx, &due); err != nil {
		return 0, err
	}
	for _, d := range due {
		if d.MediaID != "" {
			if err := s.blobs.Delete(ctx, d.MediaID); err != nil {
				return burned, err
			}
			if _, err := s.media.DeleteOne(ctx, bson.M{"_id": d.MediaID}); err != nil {
				return burned, err
			}
		}
		if _, err := s.messages.DeleteOne(ctx, bson.M{"_id": d.ID}); err != nil {
			return burned, err
		}
		burned++
	}

	stale, err := s.media.Find(ctx, bson.M{"attached": false, "created_at": bson.M{"$lte": now.Add(-time.Hour)}})
	if err != nil {
		return burned, err
	}
	var orphans []mediaDoc
	if err := stale.All(ctx, &orphans); err != nil {
		return burned, err
	}
	for _, m := range orphans {
		if err := s.blobs.Delete(ctx, m.ID); err != nil {
			return burned, err
		}
		if _, err := s.media.DeleteOne(ctx, bson.M{"_id": m.ID}); err != nil {
			return burned, err
		}
	}
	return burned, s.sweepInactiveMatches(ctx)
}

// sweepInactiveMatches removes conversations whose match ended (unmatch,
// block, ban, hotel purge) so ciphertext is not retained without a purpose.
func (s *Service) sweepInactiveMatches(ctx context.Context) error {
	ids, err := s.messages.Distinct(ctx, "match_id", bson.M{}).Raw()
	if err != nil {
		return err
	}
	var matchIDs []string
	vals, err := ids.Values()
	if err != nil {
		return err
	}
	for _, v := range vals {
		if str, ok := v.StringValueOK(); ok {
			matchIDs = append(matchIDs, str)
		}
	}
	if len(matchIDs) == 0 {
		return nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT x FROM unnest($1::text[]) AS x
		 WHERE NOT EXISTS (SELECT 1 FROM matches m WHERE m.id::text = x AND m.status = 'matched')
		   AND NOT EXISTS (SELECT 1 FROM group_matches g WHERE g.id::text = x AND g.status = 'matched')`, matchIDs)
	if err != nil {
		return err
	}
	var dead []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		dead = append(dead, id)
	}
	rows.Close()
	for _, id := range dead {
		if err := s.deleteByFilter(ctx, bson.M{"match_id": id}); err != nil {
			return err
		}
	}
	return rows.Err()
}

// StartJob runs RunJob every interval until ctx ends.
func (s *Service) StartJob(ctx context.Context, interval time.Duration, logErr func(error)) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			started := time.Now()
			n, err := s.RunJob(ctx)
			if ctx.Err() == nil {
				metrics.Job("chat_retention", time.Since(started), err)
				metrics.JobItems("chat_retention", "burned_messages", int(n))
			}
			if err != nil && ctx.Err() == nil {
				logErr(err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
