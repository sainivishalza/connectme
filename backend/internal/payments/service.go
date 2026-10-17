package payments

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectme/internal/entitlements"
	"connectme/internal/users"
	"connectme/internal/wallet"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config carries the values the service needs from process configuration.
type Config struct {
	WebhookSecret string
	SuccessURL    string // fixed, never client-supplied (no open redirect)
	CancelURL     string
}

type Service struct {
	db     *pgxpool.Pool
	users  *users.Service
	stripe Stripe // nil when not configured
	cfg    Config
	now    func() time.Time
}

func NewService(db *pgxpool.Pool, u *users.Service, s Stripe, cfg Config) *Service {
	return &Service{db: db, users: u, stripe: s, cfg: cfg, now: time.Now}
}

// Account is the caller's monetization state.
type Account struct {
	Coins            int64              `json:"coins"`
	Tier             entitlements.Tier  `json:"tier"`
	Perks            entitlements.Perks `json:"perks"`
	SubscriptionEnds *time.Time         `json:"subscription_ends_at,omitempty"`
	CancelsAtEnd     bool               `json:"cancels_at_period_end"`
	SuperLikesLeft   int                `json:"super_likes_left_today"`
	FreeBoostReady   bool               `json:"free_boost_available"`
	LikesLeftToday   *int               `json:"likes_left_today,omitempty"` // nil = unlimited
	Packs            []CoinPack         `json:"coin_packs"`
}

// Account returns balance, tier and remaining daily/weekly allowances.
func (s *Service) Account(ctx context.Context, userID string) (*Account, error) {
	tier, err := entitlements.TierOf(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	a := &Account{Tier: tier, Perks: entitlements.PerksFor(tier), Packs: Packs()}
	if a.Coins, err = wallet.Balance(ctx, s.db, userID); err != nil {
		return nil, err
	}
	day := entitlements.StartOfDayUTC(s.now())
	var superUsed, likesUsed, boosts int
	err = s.db.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE action='superlike'), COUNT(*) FILTER (WHERE action IN ('like','superlike'))
		  FROM swipes WHERE actor_id=$1 AND created_at >= $2`, userID, day).Scan(&superUsed, &likesUsed)
	if err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM boost_log WHERE user_id=$1 AND source='perk' AND created_at >= $2`,
		userID, entitlements.WeekAgo(s.now())).Scan(&boosts); err != nil {
		return nil, err
	}
	a.SuperLikesLeft = max(0, a.Perks.SuperLikesPerDay-superUsed)
	switch pf := a.Perks.FreeBoostsPerWeek; {
	case pf < 0:
		a.FreeBoostReady = true
	default:
		a.FreeBoostReady = boosts < pf
	}
	if !a.Perks.UnlimitedLikes {
		left := max(0, entitlements.FreeLikesPerDay-likesUsed)
		a.LikesLeftToday = &left
	}
	var ends *time.Time
	var cancels bool
	err = s.db.QueryRow(ctx, `
		SELECT expires_at, cancel_at_period_end FROM subscriptions
		 WHERE user_id=$1 AND status IN ('active','trialing') AND (expires_at IS NULL OR expires_at > NOW())
		 ORDER BY expires_at DESC NULLS FIRST LIMIT 1`, userID).Scan(&ends, &cancels)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	a.SubscriptionEnds, a.CancelsAtEnd = ends, cancels
	if tier == entitlements.None || !a.Perks.Invisibility {
		// Lapsed perk: stop hiding the profile.
		_, _ = s.db.Exec(ctx, `UPDATE profiles SET invisible=FALSE WHERE user_id=$1 AND invisible`, userID)
	}
	return a, nil
}

func (s *Service) configured() error {
	if s.stripe == nil || s.cfg.SuccessURL == "" || s.cfg.CancelURL == "" {
		return ErrNotConfigured
	}
	return nil
}

// Subscribe starts a Checkout Session for a tier. Switching plans while
// subscribed is refused (it would create a second, double-billed subscription).
func (s *Service) Subscribe(ctx context.Context, userID string, tier entitlements.Tier) (id, url string, err error) {
	price, ok := Price(tier)
	if !ok {
		return "", "", ValidationError{"tier must be silver, gold or platinum"}
	}
	if err := s.configured(); err != nil {
		return "", "", err
	}
	if cur, err := entitlements.TierOf(ctx, s.db, userID); err != nil {
		return "", "", err
	} else if cur != entitlements.None {
		return "", "", ErrAlreadySubscribed
	}
	if err := s.requireVerified(ctx, userID); err != nil {
		return "", "", err
	}
	return s.stripe.CreateCheckout(ctx, CheckoutParams{
		Mode: "subscription", ProductName: price.Name, Cents: price.Cents, Recurring: true,
		UserID: userID, Metadata: map[string]string{"user_id": userID, "tier": string(tier)},
		SuccessURL: s.cfg.SuccessURL, CancelURL: s.cfg.CancelURL,
	})
}

// BuyCoins starts a one-off Checkout Session for a coin pack.
func (s *Service) BuyCoins(ctx context.Context, userID, packID string) (id, url string, err error) {
	pack, ok := PackByID(packID)
	if !ok {
		return "", "", ValidationError{"unknown coin pack"}
	}
	if err := s.configured(); err != nil {
		return "", "", err
	}
	if err := s.requireVerified(ctx, userID); err != nil {
		return "", "", err
	}
	return s.stripe.CreateCheckout(ctx, CheckoutParams{
		Mode: "payment", ProductName: "ConnectMe coins", Cents: pack.Cents,
		UserID: userID, Metadata: map[string]string{"user_id": userID, "pack_id": pack.ID},
		SuccessURL: s.cfg.SuccessURL, CancelURL: s.cfg.CancelURL,
	})
}

func (s *Service) requireVerified(ctx context.Context, userID string) error {
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1`, userID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ValidationError{"verify your email first"}
	}
	return nil
}

// Cancel asks Stripe to end the subscription at the period end. Local state
// changes only when Stripe's webhook confirms it.
func (s *Service) Cancel(ctx context.Context, userID string) error {
	if err := s.configured(); err != nil {
		return err
	}
	var subID *string
	err := s.db.QueryRow(ctx, `
		SELECT stripe_subscription_id FROM subscriptions
		 WHERE user_id=$1 AND status IN ('active','trialing') AND stripe_subscription_id IS NOT NULL
		 ORDER BY started_at DESC LIMIT 1`, userID).Scan(&subID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && subID == nil) {
		return ErrNoSubscription
	}
	if err != nil {
		return err
	}
	if err := s.stripe.CancelAtPeriodEnd(ctx, *subID); err != nil {
		slog.Error("payments: stripe cancel failed", "err", err)
		return err
	}
	return nil
}
