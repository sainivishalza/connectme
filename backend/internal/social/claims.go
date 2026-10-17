package social

import (
	"context"
	"errors"
	"time"

	"connectme/internal/push"
	"connectme/internal/users"
	"connectme/pkg/metrics"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Claim says "that's me" on a missed-connection post. Only someone currently
// checked in at the same venue can, never the author, never someone either
// side has blocked. Claiming twice is a no-op.
func (s *Service) Claim(ctx context.Context, claimantID, postID string) error {
	if _, err := uuid.Parse(postID); err != nil {
		return ErrNotFound
	}
	var ok bool
	err := s.db.QueryRow(ctx, `
		SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, claimantID).Scan(&ok)
	if err != nil || !ok {
		return ErrForbidden
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO missed_claims (post_id, claimant_id)
		SELECT p.id, $1
		  FROM posts p
		 WHERE p.id = $2::uuid AND p.kind = 'missed' AND p.expires_at > NOW() AND p.author_id <> $1
		   AND `+users.NotBlockedSQL("p.author_id", "$1::uuid")+`
		   AND p.hotel_id IN (SELECT c.hotel_id FROM hotel_checkins c
		                       WHERE c.user_id = $1 AND c.checked_out_at IS NULL AND c.auto_delete_at > NOW())
		ON CONFLICT (post_id, claimant_id) DO NOTHING`, claimantID, postID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Either it was already claimed (fine) or the post is not claimable by this person.
		var already bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM missed_claims WHERE post_id=$1::uuid AND claimant_id=$2)`,
			postID, claimantID).Scan(&already); err != nil {
			return err
		}
		if !already {
			return ErrNotFound
		}
	}
	metrics.Event("missed_claim")
	return nil
}

// ClaimView is one pending "that's me" on the author's post.
type ClaimView struct {
	ID        string               `json:"id"`
	Claimant  *users.PublicProfile `json:"claimant"`
	CreatedAt time.Time            `json:"created_at"`
}

// Claims lists the pending claims on the caller's own post. Each claimant is
// shown the way their mode allows, so an anonymous claimant stays anonymous
// until both of you choose otherwise.
func (s *Service) Claims(ctx context.Context, authorID, postID string) ([]ClaimView, error) {
	if _, err := uuid.Parse(postID); err != nil {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(ctx, `
		SELECT c.id::text, c.claimant_id::text, c.created_at
		  FROM missed_claims c JOIN posts p ON p.id = c.post_id
		 WHERE c.post_id = $1::uuid AND p.author_id = $2 AND p.expires_at > NOW() AND c.status = 'pending'
		 ORDER BY c.created_at`, postID, authorID)
	if err != nil {
		return nil, err
	}
	type rec struct {
		id, claimant string
		at           time.Time
	}
	var recs []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.claimant, &r.at); err != nil {
			rows.Close()
			return nil, err
		}
		recs = append(recs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []ClaimView{}
	for _, r := range recs {
		card, err := s.users.Card(ctx, authorID, r.claimant, "")
		if err != nil {
			continue
		}
		out = append(out, ClaimView{ID: r.id, Claimant: card, CreatedAt: r.at})
	}
	return out, nil
}

// owned loads a pending claim on a post the caller wrote.
func (s *Service) owned(ctx context.Context, tx pgx.Tx, authorID, claimID string) (claimant, checkin string, err error) {
	if _, perr := uuid.Parse(claimID); perr != nil {
		return "", "", ErrNotFound
	}
	err = tx.QueryRow(ctx, `
		SELECT c.claimant_id::text, p.checkin_id::text
		  FROM missed_claims c JOIN posts p ON p.id = c.post_id
		 WHERE c.id = $1::uuid AND p.author_id = $2 AND p.expires_at > NOW() AND c.status = 'pending'
		 FOR UPDATE OF c`, claimID, authorID).Scan(&claimant, &checkin)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return claimant, checkin, err
}

// Accept turns a claim into a match between the author and the claimant. The
// match belongs to the author's stay, so it is deleted with it like any other
// hotel match. Returns the match id.
func (s *Service) Accept(ctx context.Context, authorID, claimID string) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	claimant, checkin, err := s.owned(ctx, tx, authorID, claimID)
	if err != nil {
		return "", err
	}
	var okBoth bool
	if err := tx.QueryRow(ctx, `
		SELECT (SELECT is_verified AND NOT is_banned FROM users WHERE id=$1)
		   AND (SELECT is_verified AND NOT is_banned FROM users WHERE id=$2)
		   AND `+users.NotBlockedSQL("$1::uuid", "$2::uuid"), authorID, claimant).Scan(&okBoth); err != nil || !okBoth {
		return "", ErrForbidden
	}
	u1, u2 := authorID, claimant
	if u2 < u1 {
		u1, u2 = u2, u1
	}
	var matchID, status string
	err = tx.QueryRow(ctx, `
		INSERT INTO matches (user1_id, user2_id, status, mode, checkin_id)
		VALUES ($1,$2,'matched','hotel',$3::uuid)
		ON CONFLICT (user1_id, user2_id) DO UPDATE SET user1_id = matches.user1_id
		RETURNING id::text, status`, u1, u2, checkin).Scan(&matchID, &status)
	if err != nil {
		return "", err
	}
	if status != "matched" {
		return "", ErrForbidden // one of you ended an earlier match; that stays ended
	}
	if _, err := tx.Exec(ctx, `UPDATE missed_claims SET status='accepted' WHERE id=$1::uuid`, claimID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	metrics.Event("match_created")
	if s.notifier != nil {
		s.notifier.Notify(ctx, claimant, push.KindMatch, matchID)
	}
	return matchID, nil
}

// Decline hides a claim. The claimant is not told.
func (s *Service) Decline(ctx context.Context, authorID, claimID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, _, err := s.owned(ctx, tx, authorID, claimID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE missed_claims SET status='declined' WHERE id=$1::uuid`, claimID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
