package levels

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db    *pgxpool.Pool
	now   func() time.Time
	mu    sync.Mutex
	cache map[string]cached
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db, now: time.Now, cache: map[string]cached{}}
}

// Mine is the caller's own view: both ladders and what others may see.
type Mine struct {
	Charm         Level `json:"charm"`
	Supporter     Level `json:"supporter"`
	ShowCharm     bool  `json:"show_charm"`
	ShowSupporter bool  `json:"show_supporter"`
}

// MineFor returns the caller's levels and visibility switches.
func (s *Service) MineFor(ctx context.Context, userID string) (*Mine, error) {
	var recv, sent int64
	var m Mine
	err := s.db.QueryRow(ctx, `
		SELECT charm_coins(p.user_id), support_coins(p.user_id), p.show_charm, p.show_supporter
		  FROM profiles p WHERE p.user_id = $1::uuid`, userID).Scan(&recv, &sent, &m.ShowCharm, &m.ShowSupporter)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.Charm, m.Supporter = For(recv), For(sent)
	return &m, nil
}

// SetVisibility changes what others may see of the caller's levels.
func (s *Service) SetVisibility(ctx context.Context, userID string, showCharm, showSupporter bool) error {
	tag, err := s.db.Exec(ctx, `UPDATE profiles SET show_charm=$2, show_supporter=$3 WHERE user_id=$1::uuid`, userID, showCharm, showSupporter)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CharmLevels returns the Charm level (2 and up) of each profile in refs that
// has chosen to show it. Level 1 is the starting point and is not worth a
// badge, so it is omitted. Used by the profile projection.
func CharmLevels(ctx context.Context, db *pgxpool.Pool, refs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(refs) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `
		SELECT p.public_ref::text, charm_coins(p.user_id)
		  FROM profiles p WHERE p.public_ref = ANY($1::uuid[]) AND p.show_charm`, refs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var total int64
		if err := rows.Scan(&ref, &total); err != nil {
			return nil, err
		}
		if l := For(total).Level; l >= 2 {
			out[ref] = l
		}
	}
	return out, rows.Err()
}

var ErrNotFound = errors.New("not found")
