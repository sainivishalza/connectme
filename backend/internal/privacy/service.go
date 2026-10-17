// Package privacy implements GDPR / CCPA data subject rights: erasure
// (right to be forgotten) and a machine-readable export (access/portability).
package privacy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrBadPassword  = errors.New("incorrect password")
	ErrStaffAccount = errors.New("staff accounts must be demoted before erasure")
	ErrNotFound     = errors.New("not found")
)

// Collaborators are narrow interfaces so this package depends on none of the
// feature packages.
type (
	SessionRevoker interface {
		RevokeAllForUser(ctx context.Context, userID string) error
	}
	PhotoPurger interface {
		PurgePhotoFiles(ctx context.Context, userID string) error
	}
	ChatPurger interface {
		PurgeUser(ctx context.Context, userID string) error
	}
	FilePurger interface {
		PurgeFiles(ctx context.Context, userID string) error
	}
	SubscriptionCanceller interface {
		CancelNow(ctx context.Context, subscriptionID string) error
	}
)

type Service struct {
	db       *pgxpool.Pool
	sessions SessionRevoker
	photos   PhotoPurger
	chat     ChatPurger            // nil when MongoDB is not configured
	billing  SubscriptionCanceller // nil when Stripe is not configured
	files    []FilePurger          // other features' uploaded files (videos)
	now      func() time.Time
}

func NewService(db *pgxpool.Pool, s SessionRevoker, p PhotoPurger, c ChatPurger, b SubscriptionCanceller) *Service {
	return &Service{db: db, sessions: s, photos: p, chat: c, billing: b, now: time.Now}
}

// AddFilePurger registers a feature whose uploaded files must go on erasure.
func (s *Service) AddFilePurger(p FilePurger) { s.files = append(s.files, p) }

// Erase permanently deletes an account and its personal data.
//
// Order matters and every step is idempotent, so a failure part-way is simply
// retried by the user:
//
//  1. re-authenticate with the password;
//  2. stop billing at Stripe (an error aborts: never delete the customer
//     while a subscription is still charging);
//  3. purge external stores (chat in MongoDB, photo files);
//  4. delete the user row, which cascades to profile, swipes, matches,
//     consents, memberships, keys, ledger and subscriptions;
//  5. end all sessions.
//
// Financial records (payments) are retained with user_id set NULL, as tax law
// requires. A banned account leaves a one-way email hash so it cannot simply
// re-register.
func (s *Service) Erase(ctx context.Context, userID, password string) error {
	var hash, email, role string
	var banned bool
	err := s.db.QueryRow(ctx, `SELECT password_hash, email, role, is_banned FROM users WHERE id=$1::uuid`, userID).
		Scan(&hash, &email, &role, &banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return ErrBadPassword
	}
	if role != "user" {
		return ErrStaffAccount
	}

	if s.billing != nil {
		rows, err := s.db.Query(ctx, `
			SELECT stripe_subscription_id FROM subscriptions
			 WHERE user_id=$1::uuid AND stripe_subscription_id IS NOT NULL AND status NOT IN ('canceled','incomplete_expired')`, userID)
		if err != nil {
			return err
		}
		var subs []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			subs = append(subs, id)
		}
		rows.Close()
		for _, id := range subs {
			if err := s.billing.CancelNow(ctx, id); err != nil {
				return fmt.Errorf("cancel subscription: %w", err)
			}
		}
	}

	if s.chat != nil {
		if err := s.chat.PurgeUser(ctx, userID); err != nil {
			return fmt.Errorf("purge chat: %w", err)
		}
	}
	if err := s.photos.PurgePhotoFiles(ctx, userID); err != nil {
		return fmt.Errorf("purge photos: %w", err)
	}
	for _, p := range s.files {
		if err := p.PurgeFiles(ctx, userID); err != nil {
			return fmt.Errorf("purge files: %w", err)
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if banned {
		sum := sha256.Sum256([]byte(email))
		if _, err := tx.Exec(ctx,
			`INSERT INTO banned_identities (email_hash) VALUES ($1) ON CONFLICT DO NOTHING`, sum[:]); err != nil {
			return err
		}
	}
	// Group matches of groups this person was in can no longer be "complete".
	if _, err := tx.Exec(ctx, `
		UPDATE group_matches SET status='ended'
		 WHERE status='matched' AND (
		       group_a IN (SELECT group_id FROM group_members WHERE user_id=$1::uuid)
		    OR group_b IN (SELECT group_id FROM group_members WHERE user_id=$1::uuid))`, userID); err != nil {
		return err
	}
	var groupIDs []string
	rows, err := tx.Query(ctx, `SELECT group_id::text FROM group_members WHERE user_id=$1::uuid`, userID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			rows.Close()
			return err
		}
		groupIDs = append(groupIDs, g)
	}
	rows.Close()

	if _, err := tx.Exec(ctx, `
		INSERT INTO erasure_requests (user_id, requested_at, completed_at) VALUES ($1::uuid, $2, $2)
		ON CONFLICT (user_id) DO UPDATE SET completed_at = EXCLUDED.completed_at`, userID, s.now()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1::uuid`, userID); err != nil {
		return err
	}
	for _, g := range groupIDs {
		if _, err := tx.Exec(ctx, `DELETE FROM groups WHERE id=$1::uuid AND NOT EXISTS
			(SELECT 1 FROM group_members WHERE group_id=$1::uuid)`, g); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE group_members SET role='owner'
			 WHERE group_id=$1::uuid
			   AND user_id = (SELECT user_id FROM group_members WHERE group_id=$1::uuid ORDER BY joined_at LIMIT 1)
			   AND NOT EXISTS (SELECT 1 FROM group_members WHERE group_id=$1::uuid AND role='owner')`, g); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := s.sessions.RevokeAllForUser(ctx, userID); err != nil {
		// The user row is gone, so refresh would fail anyway; just record it.
		slog.Warn("privacy: could not revoke sessions after erasure", "err", err)
	}
	return nil
}
