// Package geo implements hotel / same-place discovery: geofenced check-in,
// the anonymous in-hotel feed, board, drinks, and the auto-delete job.
package geo

import (
	"context"
	"errors"
	"time"

	"connectme/internal/users"
)

const (
	// MaxStay is how long a check-in lasts without an explicit checkout; the
	// job then checks the user out automatically.
	MaxStay = 72 * time.Hour
	// RetentionAfterCheckout is the spec's 24h: everything tied to a stay is
	// deleted this long after checkout.
	RetentionAfterCheckout = 24 * time.Hour
	// LocationFreshness bounds how old the stored location may be at check-in.
	LocationFreshness = 10 * time.Minute
	// ExitGrace is how far outside the fence a fresh location may drift
	// (GPS noise) before the user is automatically checked out.
	ExitGraceMeters = 250
	// MaxFenceAreaM2 rejects fences that cover more than a large venue.
	MaxFenceAreaM2 = 5_000_000
)

var (
	ErrNotFound         = errors.New("not found")
	ErrForbidden        = errors.New("forbidden")
	ErrNeedsVerified    = errors.New("verify your email first")
	ErrStaleLocation    = errors.New("update your location, then try again")
	ErrOutsideFence     = errors.New("you are not at this venue")
	ErrAlreadyCheckedIn = errors.New("already checked in")
	ErrNotCheckedIn     = errors.New("you are not checked in at this venue")
	ErrAlreadySent      = errors.New("already sent")
)

type Hotel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
}

type CheckIn struct {
	ID           string     `json:"id"`
	Hotel        Hotel      `json:"hotel"`
	CheckedInAt  time.Time  `json:"checked_in_at"`
	CheckedOutAt *time.Time `json:"checked_out_at,omitempty"`
	Mood         string     `json:"mood,omitempty"`
	DeleteAt     time.Time  `json:"data_deleted_at"`
}

type CreateHotelRequest struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	// Polygon is one closed ring of [lng, lat] pairs.
	Polygon [][2]float64 `json:"polygon"`
}

type CheckInRequest struct {
	HotelID string `json:"hotel_id"`
}

type BoardPost struct {
	ID        string    `json:"id"`
	AuthorRef string    `json:"author_ref"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type PostRequest struct {
	Message string `json:"message"`
}

type DrinkRequest struct {
	ToRef string `json:"to_ref"`
}

type Drink struct {
	FromRef   string    `json:"from_ref"`
	CreatedAt time.Time `json:"created_at"`
}

// Purger removes data held outside Postgres (e.g. MongoDB chat messages)
// that belongs to a check-in. It must be idempotent.
type Purger interface {
	PurgeCheckin(ctx context.Context, checkinID string) error
}

type FeedResult struct {
	Users []users.PublicProfile `json:"users"`
}
