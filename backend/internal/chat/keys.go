package chat

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/jackc/pgx/v5"
)

func validKey(s string) bool {
	b, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(b) >= 32 && len(b) <= 128
}

func validSig(s string) bool {
	b, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(b) >= 32 && len(b) <= 128
}

// UploadKeys replaces the caller's bundle and tops up one-time prekeys.
// Only public material is accepted; the server cannot derive any secret.
func (s *Service) UploadKeys(ctx context.Context, userID string, b KeyBundle) error {
	switch {
	case b.RegistrationID <= 0, b.SignedPreKeyID < 0:
		return ValidationError{"invalid registration or prekey id"}
	case !validKey(b.IdentityKey), !validKey(b.SignedPreKey), !validSig(b.SignedPreKeySig):
		return ValidationError{"keys must be base64 public keys"}
	case len(b.OneTimePreKeys) > 100:
		return ValidationError{"at most 100 one-time prekeys per upload"}
	}
	for _, p := range b.OneTimePreKeys {
		if p.KeyID < 0 || !validKey(p.PublicKey) {
			return ValidationError{"invalid one-time prekey"}
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
		INSERT INTO key_bundles (user_id, registration_id, identity_key, signed_prekey_id, signed_prekey, signed_prekey_sig)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (user_id) DO UPDATE SET registration_id=EXCLUDED.registration_id,
		  identity_key=EXCLUDED.identity_key, signed_prekey_id=EXCLUDED.signed_prekey_id,
		  signed_prekey=EXCLUDED.signed_prekey, signed_prekey_sig=EXCLUDED.signed_prekey_sig, updated_at=NOW()`,
		userID, b.RegistrationID, b.IdentityKey, b.SignedPreKeyID, b.SignedPreKey, b.SignedPreKeySig); err != nil {
		return err
	}
	var have int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM one_time_prekeys WHERE user_id=$1`, userID).Scan(&have); err != nil {
		return err
	}
	if have+len(b.OneTimePreKeys) > maxOneTimePrekeys {
		return ValidationError{"too many stored prekeys"}
	}
	for _, p := range b.OneTimePreKeys {
		if _, err := tx.Exec(ctx, `
			INSERT INTO one_time_prekeys (user_id, key_id, public_key) VALUES ($1,$2,$3)
			ON CONFLICT (user_id, key_id) DO UPDATE SET public_key=EXCLUDED.public_key`,
			userID, p.KeyID, p.PublicKey); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// PreKeyCount tells a client how many one-time prekeys remain (to replenish).
func (s *Service) PreKeyCount(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM one_time_prekeys WHERE user_id=$1`, userID).Scan(&n)
	return n, err
}

// FetchBundle returns the OTHER participant's bundle for an active match and
// consumes one of their one-time prekeys atomically (each is used once).
// Access is by match, so keys of people you have not matched are unreachable.
func (s *Service) FetchBundle(ctx context.Context, userID, matchID string) (*KeyBundle, error) {
	if _, err := s.participant(ctx, userID, matchID); err != nil {
		return nil, err
	}
	var other string
	if err := s.db.QueryRow(ctx, `
		SELECT CASE WHEN user1_id = $2::uuid THEN user2_id ELSE user1_id END::text
		  FROM matches WHERE id = $1::uuid`, matchID, userID).Scan(&other); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound // a group conversation: use /chat/groups/{id}/keys
		}
		return nil, err
	}
	var b KeyBundle
	err := s.db.QueryRow(ctx, `
		SELECT registration_id, identity_key, signed_prekey_id, signed_prekey, signed_prekey_sig
		  FROM key_bundles WHERE user_id = $1::uuid`, other).
		Scan(&b.RegistrationID, &b.IdentityKey, &b.SignedPreKeyID, &b.SignedPreKey, &b.SignedPreKeySig)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// At most one one-time prekey per requester, conversation and day.
	var first bool
	if err := s.db.QueryRow(ctx, `
		INSERT INTO prekey_fetches (match_id, requester) VALUES ($1::uuid, $2::uuid)
		ON CONFLICT (match_id, requester) DO UPDATE SET fetched_at = NOW()
		  WHERE prekey_fetches.fetched_at < NOW() - INTERVAL '24 hours'
		RETURNING TRUE`, matchID, userID).Scan(&first); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if !first {
		return &b, nil
	}
	var pk PreKey
	err = s.db.QueryRow(ctx, `
		DELETE FROM one_time_prekeys
		 WHERE (user_id, key_id) = (
		   SELECT user_id, key_id FROM one_time_prekeys WHERE user_id = $1::uuid
		    ORDER BY key_id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING key_id, public_key`, other).Scan(&pk.KeyID, &pk.PublicKey)
	switch {
	case err == nil:
		b.PreKey = &pk
	case errors.Is(err, pgx.ErrNoRows):
		// No prekeys left: X3DH proceeds without a one-time prekey.
	default:
		return nil, err
	}
	return &b, nil
}
