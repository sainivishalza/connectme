// Package matching implements swipes, mutual matches, boosts and reveals.
package matching

import (
	"errors"
	"time"

	"connectme/internal/users"
)

const (
	SuperLikeCoins = 5
	BoostCoins     = 20
	BoostDuration  = 30 * time.Minute
)

type Action string

const (
	ActionLike      Action = "like"
	ActionPass      Action = "pass"
	ActionSuperLike Action = "superlike"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrSelf          = errors.New("cannot swipe yourself")
	ErrNeedsVerified = errors.New("verify your email first")
	ErrAlreadySwiped = errors.New("already swiped differently on this person")
	ErrInsufficient  = errors.New("not enough coins")
	ErrDailyLimit    = errors.New("daily like limit reached; subscribe for unlimited likes")
	ErrNeedsGold     = errors.New("rewind is a Gold feature")
)

type SwipeRequest struct {
	TargetRef string `json:"target_ref"`
	Action    Action `json:"action"`
	// Comment is an optional note sent with a like or super like.
	Comment string `json:"comment,omitempty"`
}

type TargetRequest struct {
	TargetRef string `json:"target_ref"`
	Comment   string `json:"comment,omitempty"` // optional note with a super like
}

type SwipeResult struct {
	Matched bool   `json:"matched"`
	MatchID string `json:"match_id,omitempty"`
	// TheirComment is the note the other person sent with their earlier like,
	// when this swipe completed a match and they are visible in full.
	TheirComment string `json:"their_comment,omitempty"`
}

type MatchView struct {
	MatchID       string               `json:"match_id"`
	Mode          users.Mode           `json:"mode"`
	CreatedAt     time.Time            `json:"created_at"`
	RevealedByMe  bool                 `json:"revealed_by_me"`
	CompatScore   *int                 `json:"compat_score,omitempty"`
	CompatReasons []string             `json:"compat_reasons,omitempty"`
	MyComment     string               `json:"my_comment,omitempty"`
	TheirComment  string               `json:"their_comment,omitempty"`
	RevealTimer   *RevealTimer         `json:"reveal_timer,omitempty"`
	Voice         *VoiceStep           `json:"voice,omitempty"`  // anonymous matches: the voice step of the staged reveal
	Random        *RandomInfo          `json:"random,omitempty"` // set for anonymous random chats
	Profile       *users.PublicProfile `json:"profile"`
}

// RandomInfo describes a match that began as an anonymous random chat.
type RandomInfo struct {
	ExpiresAt *time.Time `json:"expires_at"` // nil once both people kept it
	KeptByMe  bool       `json:"kept_by_me"`
}

type RevealResult struct {
	Mutual bool `json:"mutual"`
}
