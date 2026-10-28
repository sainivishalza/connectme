package groups

import (
	"context"
	"errors"

	"connectme/internal/users"
	"connectme/pkg/middleware"
	"connectme/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db        *pgxpool.Pool
	users     *users.Service
	suggester Suggester
	replier   Replier
}

func NewService(db *pgxpool.Pool, u *users.Service, s Suggester) *Service {
	if s == nil {
		s = FallbackSuggester{}
	}
	svc := &Service{db: db, users: u, suggester: s}
	if r, ok := s.(Replier); ok {
		svc.replier = r
	}
	return svc
}

// requireVerified returns ErrNeedsVerified for unverified or banned callers.
func (s *Service) requireVerified(ctx context.Context, q pgx.Tx, userID string) error {
	var ok bool
	err := q.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, userID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNeedsVerified
	}
	return nil
}

// lockGroup takes a row lock so size checks and member changes serialise.
func lockGroup(ctx context.Context, tx pgx.Tx, groupID string) (typ string, target, members int, locked bool, err error) {
	if _, perr := uuid.Parse(groupID); perr != nil {
		return "", 0, 0, false, ErrNotFound
	}
	err = tx.QueryRow(ctx, `SELECT type, target_size FROM groups WHERE id=$1::uuid FOR UPDATE`, groupID).Scan(&typ, &target)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, 0, false, ErrNotFound
	}
	if err != nil {
		return
	}
	err = tx.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM group_members WHERE group_id=$1::uuid),
		       EXISTS (SELECT 1 FROM group_matches WHERE status='matched' AND $1::uuid IN (group_a, group_b))`,
		groupID).Scan(&members, &locked)
	return
}

func isMember(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, userID, groupID string) (bool, error) {
	if _, err := uuid.Parse(groupID); err != nil {
		return false, nil
	}
	var ok bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM group_members WHERE group_id=$1::uuid AND user_id=$2::uuid)`,
		groupID, userID).Scan(&ok)
	return ok, err
}

// Create makes a group with the caller as owner. Double dates are exactly 2
// people; group dates are 2-6 with the size chosen up front.
func (s *Service) Create(ctx context.Context, userID string, req CreateGroupRequest) (*Group, error) {
	name := middleware.SanitizeText(utils.StripTags(req.Name), 100)
	if name == "" {
		return nil, ValidationError{Msg: "name is required (max 100 characters)"}
	}
	target := 2
	switch req.Type {
	case "double":
		if req.TargetSize != 0 && req.TargetSize != 2 {
			return nil, ValidationError{Msg: "a double date is exactly 2 people"}
		}
	case "group":
		if req.TargetSize < 2 || req.TargetSize > 6 {
			return nil, ValidationError{Msg: "a group date is 2 to 6 people"}
		}
		target = req.TargetSize
	default:
		return nil, ValidationError{Msg: "type must be double or group"}
	}
	if !activities[req.Activity] {
		return nil, ValidationError{Msg: "activity must be dinner, hiking, concert, bar_crawl or custom"}
	}
	label := middleware.SanitizeText(utils.StripTags(req.ActivityLabel), 50)
	if req.Activity == "custom" && label == "" {
		return nil, ValidationError{Msg: "activity_label is required for a custom activity"}
	}
	desc := middleware.SanitizeText(utils.StripTags(req.Description), 280)
	if err := s.users.VetText(ctx, userID, "group", name); err != nil {
		return nil, err
	}
	if err := s.users.VetText(ctx, userID, "bio", desc); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := s.requireVerified(ctx, tx, userID); err != nil {
		return nil, err
	}
	var hasLoc bool
	if err := tx.QueryRow(ctx, `SELECT location IS NOT NULL FROM profiles WHERE user_id=$1`, userID).Scan(&hasLoc); err != nil {
		return nil, err
	}
	if !hasLoc {
		return nil, ErrNeedsLocation
	}
	// Serialise per user so the active-group cap cannot be raced.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return nil, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM group_members WHERE user_id=$1`, userID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= MaxActiveGroups {
		return nil, ErrLimit
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO groups (name, type, activity, activity_label, description, target_size, created_by, location)
		SELECT $1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7, p.location FROM profiles p WHERE p.user_id=$7
		RETURNING id::text`, name, req.Type, req.Activity, label, desc, target, userID).Scan(&id)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id, role) VALUES ($1,$2,'owner')`, id, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Get(ctx, userID, id)
}

// Get returns a group the caller belongs to, with member cards.
func (s *Service) Get(ctx context.Context, userID, groupID string) (*Group, error) {
	if _, err := uuid.Parse(groupID); err != nil {
		return nil, ErrNotFound
	}
	ok, err := isMember(ctx, s.db, userID, groupID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	g, err := s.load(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return s.withMembers(ctx, userID, g)
}

func (s *Service) load(ctx context.Context, groupID string) (*Group, error) {
	var g Group
	var label, desc *string
	err := s.db.QueryRow(ctx, `
		SELECT g.id::text, g.name, g.type, g.activity, g.activity_label, g.description, g.target_size,
		       (SELECT COUNT(*) FROM group_members m WHERE m.group_id = g.id),
		       EXISTS (SELECT 1 FROM group_matches gm WHERE gm.status='matched' AND g.id IN (gm.group_a, gm.group_b))
		  FROM groups g WHERE g.id = $1::uuid`, groupID).
		Scan(&g.ID, &g.Name, &g.Type, &g.Activity, &label, &desc, &g.TargetSize, &g.MemberCount, &g.Locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if label != nil {
		g.ActivityLabel = *label
	}
	if desc != nil {
		g.Description = *desc
	}
	g.Ready = g.MemberCount == g.TargetSize
	return &g, nil
}

// withMembers attaches each member as the viewer is allowed to see them.
func (s *Service) withMembers(ctx context.Context, viewerID string, g *Group) (*Group, error) {
	rows, err := s.db.Query(ctx,
		`SELECT user_id::text FROM group_members WHERE group_id=$1::uuid ORDER BY joined_at`, g.ID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	g.Members = []users.PublicProfile{}
	for _, id := range ids {
		card, err := s.users.Card(ctx, viewerID, id, "")
		if errors.Is(err, users.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		g.Members = append(g.Members, *card)
	}
	return g, nil
}

// Mine lists the caller's groups.
func (s *Service) Mine(ctx context.Context, userID string) ([]Group, error) {
	rows, err := s.db.Query(ctx,
		`SELECT group_id::text FROM group_members WHERE user_id=$1 ORDER BY joined_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []Group{}
	for _, id := range ids {
		g, err := s.Get(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, nil
}
