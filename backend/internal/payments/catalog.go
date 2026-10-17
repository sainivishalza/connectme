// Package payments handles subscriptions, coin purchases, gifts and Stripe
// webhooks. Prices are defined here, server-side: clients choose a catalog id
// and never supply an amount.
package payments

import (
	"errors"

	"connectme/internal/entitlements"
)

const Currency = "usd"

var (
	ErrNotFound          = errors.New("not found")
	ErrNotConfigured     = errors.New("payments are not configured")
	ErrAlreadySubscribed = errors.New("already subscribed; cancel the current plan first")
	ErrNoSubscription    = errors.New("no active subscription")
	ErrInsufficient      = errors.New("not enough coins")
)

type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

// TierPrice is a monthly subscription price in cents.
type TierPrice struct {
	Tier  entitlements.Tier
	Name  string
	Cents int64
}

var tierPrices = map[entitlements.Tier]TierPrice{
	entitlements.Silver:   {entitlements.Silver, "ConnectMe Silver", 999},
	entitlements.Gold:     {entitlements.Gold, "ConnectMe Gold", 1999},
	entitlements.Platinum: {entitlements.Platinum, "ConnectMe Platinum", 3999},
}

// CoinPack is a one-off coin purchase.
type CoinPack struct {
	ID    string `json:"id"`
	Coins int    `json:"coins"`
	Cents int64  `json:"price_cents"`
}

// coinPacks span the spec's $0.99 - $99.99 range.
var coinPacks = []CoinPack{
	{"starter", 50, 99},
	{"small", 275, 499},
	{"medium", 600, 999},
	{"large", 1650, 2499},
	{"mega", 3500, 4999},
	{"whale", 7500, 9999},
}

func PackByID(id string) (CoinPack, bool) {
	for _, p := range coinPacks {
		if p.ID == id {
			return p, true
		}
	}
	return CoinPack{}, false
}

func Packs() []CoinPack { return append([]CoinPack(nil), coinPacks...) }

func Price(t entitlements.Tier) (TierPrice, bool) {
	p, ok := tierPrices[t]
	return p, ok
}
