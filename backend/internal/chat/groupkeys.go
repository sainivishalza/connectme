package chat

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"connectme/pkg/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

const (
	maxEnvelopeBytes  = 16 << 10
	maxEnvelopesQueue = 200
)

// GroupMemberBundle is one member's public bundle, keyed by an opaque member
// token (their user id is never exposed): clients use it to set up pairwise
// Signal sessions through which they distribute their sender key.
type GroupMemberBundle struct {
	MemberToken string `json:"member_token"`
	KeyBundle
}

// Envelope is an opaque, client-encrypted blob addressed to one member.
type Envelope struct {
	ID         string    `json:"id"`
	From       string    `json:"from"` // member token
	Ciphertext string    `json:"ciphertext"`
	CreatedAt  time.Time `json:"created_at"`
}

type SendEnvelopeRequest struct {
	To         string `json:"to"` // member token
	Ciphertext string `json:"ciphertext"`
}

func (s *Service) groupMembers(ctx context.Context, convID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `
		SELECT m.user_id::text
		  FROM group_matches gm JOIN group_members m ON m.group_id IN (gm.group_a, gm.group_b)
		 WHERE gm.id = $1::uuid AND gm.status = 'matched'`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// token is a stable per-conversation pseudonym for a member. User ids are
// random 128-bit values that are never exposed, so the hash cannot be
// reversed or linked to a user outside the conversation.
func token(userID, convID string) string {
	h := sha256.Sum256([]byte(userID + ":" + convID))
	return hex.EncodeToString(h[:16])
}

// FetchGroupBundles returns the other members' bundles, consuming one
// one-time prekey from each.
func (s *Service) FetchGroupBundles(ctx context.Context, userID, convID string) ([]GroupMemberBundle, error) {
	if _, err := s.participant(ctx, userID, convID); err != nil {
		return nil, err
	}
	members, err := s.groupMembers(ctx, convID)
	if err != nil {
		return nil, err
	}
	out := []GroupMemberBundle{}
	for _, m := range members {
		if m == userID {
			continue
		}
		b, err := s.bundleOf(ctx, m)
		if errors.Is(err, ErrNotFound) {
			continue // member has not uploaded keys yet
		}
		if err != nil {
			return nil, err
		}
		out = append(out, GroupMemberBundle{MemberToken: token(m, convID), KeyBundle: *b})
	}
	return out, nil
}

// SendEnvelope queues an opaque blob for another member of the conversation.
func (s *Service) SendEnvelope(ctx context.Context, userID, convID string, req SendEnvelopeRequest) error {
	if _, err := s.participant(ctx, userID, convID); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(req.Ciphertext)
	if err != nil || len(raw) == 0 {
		return ValidationError{"ciphertext must be base64"}
	}
	if len(raw) > maxEnvelopeBytes {
		return ErrTooLarge
	}
	members, err := s.groupMembers(ctx, convID)
	if err != nil {
		return err
	}
	var to string
	for _, m := range members {
		if m != userID && token(m, convID) == req.To {
			to = m
		}
	}
	if to == "" {
		return ErrNotFound
	}
	var queued int
	if err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_key_envelopes WHERE to_user=$1::uuid AND group_match_id=$2::uuid`, to, convID).Scan(&queued); err != nil {
		return err
	}
	if queued >= maxEnvelopesQueue {
		return ValidationError{"recipient's mailbox is full"}
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO group_key_envelopes (group_match_id, from_user, to_user, ciphertext)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4)`, convID, userID, to, req.Ciphertext)
	return err
}

// FetchEnvelopes returns and deletes the caller's queued envelopes.
func (s *Service) FetchEnvelopes(ctx context.Context, userID, convID string) ([]Envelope, error) {
	if _, err := s.participant(ctx, userID, convID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		DELETE FROM group_key_envelopes
		 WHERE to_user=$1::uuid AND group_match_id=$2::uuid
		RETURNING id::text, from_user::text, ciphertext, created_at`, userID, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Envelope{}
	for rows.Next() {
		var e Envelope
		var from string
		if err := rows.Scan(&e.ID, &from, &e.Ciphertext, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.From = token(from, convID)
		out = append(out, e)
	}
	return out, rows.Err()
}

// bundleOf loads (and consumes one prekey of) a user's bundle.
func (s *Service) bundleOf(ctx context.Context, userID string) (*KeyBundle, error) {
	var b KeyBundle
	err := s.db.QueryRow(ctx, `
		SELECT registration_id, identity_key, signed_prekey_id, signed_prekey, signed_prekey_sig
		  FROM key_bundles WHERE user_id = $1::uuid`, userID).
		Scan(&b.RegistrationID, &b.IdentityKey, &b.SignedPreKeyID, &b.SignedPreKey, &b.SignedPreKeySig)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var pk PreKey
	err = s.db.QueryRow(ctx, `
		DELETE FROM one_time_prekeys
		 WHERE (user_id, key_id) = (
		   SELECT user_id, key_id FROM one_time_prekeys WHERE user_id = $1::uuid
		    ORDER BY key_id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING key_id, public_key`, userID).Scan(&pk.KeyID, &pk.PublicKey)
	if err == nil {
		b.PreKey = &pk
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return &b, nil
}

// --- HTTP ---

func (h *Handler) groupKeys(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.FetchGroupBundles(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"members": res})
}

func (h *Handler) sendEnvelope(w http.ResponseWriter, r *http.Request) {
	var req SendEnvelopeRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.svc.SendEnvelope(r.Context(), uid(r), chi.URLParam(r, "id"), req); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) fetchEnvelopes(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.FetchEnvelopes(r.Context(), uid(r), chi.URLParam(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"envelopes": res})
}
