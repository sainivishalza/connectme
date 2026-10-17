package groups

import (
	"context"
	"errors"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Invite asks another (visible) user to join. Pending invites never exceed
// the open slots, and a group with an active match is locked.
func (s *Service) Invite(ctx context.Context, userID, groupID, inviteeRef string) error {
	inviteeID, err := s.users.ResolveVisibleRef(ctx, userID, inviteeRef)
	if errors.Is(err, users.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if inviteeID == userID {
		return ValidationError{Msg: "you cannot invite yourself"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	_, target, members, locked, err := lockGroup(ctx, tx, groupID)
	if err != nil {
		return err
	}
	if ok, err := isMember(ctx, tx, userID, groupID); err != nil || !ok {
		if err != nil {
			return err
		}
		return ErrNotFound
	}
	if locked {
		return ErrGroupLocked
	}
	if already, err := isMember(ctx, tx, inviteeID, groupID); err != nil {
		return err
	} else if already {
		return ErrConflict
	}
	var pending int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_invites WHERE group_id=$1::uuid AND status='pending'`, groupID).Scan(&pending); err != nil {
		return err
	}
	if members+pending >= target {
		return ErrGroupFull
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO group_invites (group_id, inviter_id, invitee_id) VALUES ($1::uuid,$2,$3)
		ON CONFLICT (group_id, invitee_id) DO UPDATE SET status='pending', inviter_id=EXCLUDED.inviter_id, created_at=NOW()
		  WHERE group_invites.status = 'declined'`, groupID, userID, inviteeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return tx.Commit(ctx)
}

// Invites lists the caller's pending invitations.
func (s *Service) Invites(ctx context.Context, userID string) ([]Invite, error) {
	rows, err := s.db.Query(ctx, `
		SELECT i.id::text, g.id::text, g.name, g.activity, g.type, i.created_at
		  FROM group_invites i JOIN groups g ON g.id = i.group_id
		 WHERE i.invitee_id=$1 AND i.status='pending'
		   AND `+users.NotBlockedSQL("i.inviter_id", "$1::uuid")+`
		 ORDER BY i.created_at DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var v Invite
		if err := rows.Scan(&v.ID, &v.GroupID, &v.GroupName, &v.Activity, &v.Type, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Respond accepts (joins) or declines an invitation addressed to the caller.
func (s *Service) Respond(ctx context.Context, userID, inviteID string, accept bool) error {
	if _, err := uuid.Parse(inviteID); err != nil {
		return ErrNotFound
	}
	var groupID string
	err := s.db.QueryRow(ctx,
		`SELECT group_id::text FROM group_invites WHERE id=$1::uuid AND invitee_id=$2 AND status='pending'`,
		inviteID, userID).Scan(&groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !accept {
		_, err := s.db.Exec(ctx, `UPDATE group_invites SET status='declined' WHERE id=$1::uuid`, inviteID)
		return err
	}
	return s.join(ctx, userID, groupID)
}

// Join accepts the caller's pending invitation to a specific group.
func (s *Service) Join(ctx context.Context, userID, groupID string) error {
	if _, err := uuid.Parse(groupID); err != nil {
		return ErrNotFound
	}
	var inviteID string
	err := s.db.QueryRow(ctx,
		`SELECT id::text FROM group_invites WHERE group_id=$1::uuid AND invitee_id=$2 AND status='pending'`,
		groupID, userID).Scan(&inviteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound // groups are invite-only
	}
	if err != nil {
		return err
	}
	return s.join(ctx, userID, groupID)
}

func (s *Service) join(ctx context.Context, userID, groupID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := s.requireVerified(ctx, tx, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return err
	}
	_, target, members, locked, err := lockGroup(ctx, tx, groupID)
	if err != nil {
		return err
	}
	switch {
	case locked:
		return ErrGroupLocked
	case members >= target:
		return ErrGroupFull
	}
	var mine int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM group_members WHERE user_id=$1`, userID).Scan(&mine); err != nil {
		return err
	}
	if mine >= MaxActiveGroups {
		return ErrLimit
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1::uuid,$2) ON CONFLICT DO NOTHING`, groupID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE group_invites SET status='accepted' WHERE group_id=$1::uuid AND invitee_id=$2`, groupID, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Leave removes the caller. A group that is no longer full cannot stay
// matched, so its matches end (and their chats are swept). Ownership passes
// to the longest-standing member; an empty group is deleted.
func (s *Service) Leave(ctx context.Context, userID, groupID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, _, _, _, err := lockGroup(ctx, tx, groupID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM group_members WHERE group_id=$1::uuid AND user_id=$2`, groupID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM group_match_consents WHERE user_id=$1 AND (group_id=$2::uuid OR target_group_id=$2::uuid)`,
		userID, groupID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE group_matches SET status='ended' WHERE status='matched' AND $1::uuid IN (group_a, group_b)`, groupID); err != nil {
		return err
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM group_members WHERE group_id=$1::uuid`, groupID).Scan(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM groups WHERE id=$1::uuid`, groupID); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `
		UPDATE group_members SET role='owner'
		 WHERE group_id=$1::uuid AND user_id = (SELECT user_id FROM group_members WHERE group_id=$1::uuid ORDER BY joined_at LIMIT 1)
		   AND NOT EXISTS (SELECT 1 FROM group_members WHERE group_id=$1::uuid AND role='owner')`, groupID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
