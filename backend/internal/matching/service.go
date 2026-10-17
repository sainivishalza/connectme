package matching

import (
	"context"
	"errors"
	"time"

	"connectme/internal/entitlements"
	"connectme/internal/push"
	"connectme/internal/users"
	"connectme/internal/wallet"
	"connectme/pkg/metrics"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db    *pgxpool.Pool
	users *users.Service

	scorer   Scorer          // optional AI scoring
	history  HistoryProvider // optional conversation metadata
	notifier push.Notifier
}

// SetNotifier enables push notifications (nil keeps them off).
func (s *Service) SetNotifier(n push.Notifier) { s.notifier = n }

func NewService(db *pgxpool.Pool, u *users.Service) *Service { return &Service{db: db, users: u} }

// Swipe records a like/pass/superlike and creates a match on mutual interest.
//
// A per-pair advisory lock serialises the two directions, so two people
// liking each other at the same instant always produce exactly one match.
func (s *Service) Swipe(ctx context.Context, userID, targetRef string, action Action) (*SwipeResult, error) {
	if action != ActionLike && action != ActionPass && action != ActionSuperLike {
		return nil, users.ValidationError{Msg: "action must be like, pass or superlike"}
	}
	targetID, err := s.users.ResolveVisibleRef(ctx, userID, targetRef)
	if errors.Is(err, users.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if targetID == userID {
		return nil, ErrSelf
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var verified bool
	if err := tx.QueryRow(ctx, `SELECT is_verified FROM users WHERE id=$1`, userID).Scan(&verified); err != nil {
		return nil, err
	}
	if !verified {
		return nil, ErrNeedsVerified
	}

	u1, u2 := orderPair(userID, targetID)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, u1+"|"+u2); err != nil {
		return nil, err
	}

	// Idempotent repeat of the same swipe; a different one is a conflict.
	var existing string
	err = tx.QueryRow(ctx, `SELECT action FROM swipes WHERE actor_id=$1 AND target_id=$2`, userID, targetID).Scan(&existing)
	if err == nil {
		if existing != string(action) {
			return nil, ErrAlreadySwiped
		}
		return s.currentResult(ctx, tx, u1, u2)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	if action != ActionPass {
		// Serialise this user's own likes so daily allowances cannot be raced.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
			return nil, err
		}
		tier, err := entitlements.TierOf(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		perks := entitlements.PerksFor(tier)
		day := entitlements.StartOfDayUTC(time.Now())
		var likesToday, superToday int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FILTER (WHERE action IN ('like','superlike')), COUNT(*) FILTER (WHERE action='superlike')
			  FROM swipes WHERE actor_id=$1 AND created_at >= $2`, userID, day).Scan(&likesToday, &superToday); err != nil {
			return nil, err
		}
		if !perks.UnlimitedLikes && likesToday >= entitlements.FreeLikesPerDay {
			return nil, ErrDailyLimit
		}
		// Subscribers get a daily allowance of free super likes; beyond it
		// (and for everyone else) a super like costs coins.
		if action == ActionSuperLike && superToday >= perks.SuperLikesPerDay {
			if err := wallet.Spend(ctx, tx, userID, SuperLikeCoins, "superlike", ""); err != nil {
				if errors.Is(err, wallet.ErrInsufficient) {
					return nil, ErrInsufficient
				}
				return nil, err
			}
		}
	}

	// A swipe made while both people are checked in at the same hotel is
	// bound to that stay and disappears with it.
	var swipeCheckin *string
	_ = tx.QueryRow(ctx, `
		SELECT a.id::text FROM hotel_checkins a JOIN hotel_checkins b ON b.hotel_id = a.hotel_id
		 WHERE a.user_id=$1 AND b.user_id=$2 AND a.checked_out_at IS NULL AND b.checked_out_at IS NULL`,
		userID, targetID).Scan(&swipeCheckin)
	if _, err := tx.Exec(ctx,
		`INSERT INTO swipes (actor_id, target_id, action, checkin_id) VALUES ($1,$2,$3,$4)`,
		userID, targetID, string(action), swipeCheckin); err != nil {
		return nil, err
	}
	if action == ActionPass {
		return &SwipeResult{}, tx.Commit(ctx)
	}

	var reverse bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM swipes WHERE actor_id=$1 AND target_id=$2 AND action IN ('like','superlike'))`,
		targetID, userID).Scan(&reverse); err != nil {
		return nil, err
	}
	if !reverse {
		return &SwipeResult{}, tx.Commit(ctx)
	}

	matchID, err := s.createMatch(ctx, tx, u1, u2)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	metrics.Event("match_created")
	s.scoreAsync(matchID)
	if s.notifier != nil {
		s.notifier.Notify(ctx, targetID, push.KindMatch, matchID) // the swiper already sees it
	}
	return &SwipeResult{Matched: true, MatchID: matchID}, nil
}

// createMatch derives the match mode from the stricter of the two people's
// current modes and, for hotel matches, binds it to the earlier-expiring
// check-in so it is purged with that stay.
func (s *Service) createMatch(ctx context.Context, tx pgx.Tx, u1, u2 string) (string, error) {
	var m1, m2 string
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT mode FROM profiles WHERE user_id=$1), (SELECT mode FROM profiles WHERE user_id=$2)`, u1, u2).
		Scan(&m1, &m2); err != nil {
		return "", err
	}
	mode := strictest(users.Mode(m1), users.Mode(m2))

	var checkin *string
	if mode == users.ModeHotel {
		_ = tx.QueryRow(ctx, `
			SELECT id::text FROM hotel_checkins
			 WHERE user_id IN ($1,$2) AND checked_out_at IS NULL
			 ORDER BY auto_delete_at LIMIT 1`, u1, u2).Scan(&checkin)
	}
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO matches (user1_id, user2_id, status, mode, checkin_id)
		VALUES ($1,$2,'matched',$3,$4)
		ON CONFLICT (user1_id, user2_id) DO UPDATE SET status = matches.status
		RETURNING id::text`, u1, u2, string(mode), checkin).Scan(&id)
	return id, err
}

func (s *Service) currentResult(ctx context.Context, tx pgx.Tx, u1, u2 string) (*SwipeResult, error) {
	var id string
	err := tx.QueryRow(ctx,
		`SELECT id::text FROM matches WHERE user1_id=$1 AND user2_id=$2 AND status='matched'`, u1, u2).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return &SwipeResult{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &SwipeResult{Matched: true, MatchID: id}, nil
}

// strictest returns the most private mode of the two: hotel > incognito > open.
func strictest(a, b users.Mode) users.Mode {
	for _, m := range []users.Mode{users.ModeHotel, users.ModeIncognito} {
		if a == m || b == m {
			return m
		}
	}
	return users.ModeOpen
}

func orderPair(a, b string) (string, string) {
	if b < a {
		return b, a
	}
	return a, b
}
