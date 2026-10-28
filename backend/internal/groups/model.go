// Package groups implements double dates (2 people) and group dates (2-6),
// group-to-group matching with unanimous consent, and scheduling.
package groups

import (
	"errors"
	"time"

	"connectme/internal/users"
)

const (
	MaxActiveGroups  = 5
	maxDateHorizon   = 90 * 24 * time.Hour
	maxOpenProposals = 5
)

var (
	ErrNotFound      = errors.New("not found")
	ErrForbidden     = errors.New("forbidden")
	ErrNeedsVerified = errors.New("verify your email first")
	ErrNeedsLocation = errors.New("set your location first")
	ErrGroupFull     = errors.New("group is full")
	ErrGroupLocked   = errors.New("group has an active match and cannot change members")
	ErrNotReady      = errors.New("group is not complete yet")
	ErrConflict      = errors.New("already done")
	ErrLimit         = errors.New("limit reached")
)

type ValidationError = users.ValidationError

var activities = map[string]bool{"dinner": true, "hiking": true, "concert": true, "bar_crawl": true, "custom": true}

type CreateGroupRequest struct {
	Name          string `json:"name"`
	Type          string `json:"type"` // double | group
	Activity      string `json:"activity"`
	ActivityLabel string `json:"activity_label"` // for custom
	TargetSize    int    `json:"target_size"`    // group only, 2-6
	Description   string `json:"description"`
}

type Group struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Type          string                `json:"type"`
	Activity      string                `json:"activity"`
	ActivityLabel string                `json:"activity_label,omitempty"`
	Description   string                `json:"description,omitempty"`
	TargetSize    int                   `json:"target_size"`
	MemberCount   int                   `json:"member_count"`
	Ready         bool                  `json:"ready"` // full and matchable
	Locked        bool                  `json:"locked"`
	Distance      string                `json:"distance,omitempty"`
	Members       []users.PublicProfile `json:"members,omitempty"`
}

type InviteRequest struct {
	Ref string `json:"ref"`
}

type Invite struct {
	ID        string    `json:"id"`
	GroupID   string    `json:"group_id"`
	GroupName string    `json:"group_name"`
	Activity  string    `json:"activity"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

type MatchRequest struct {
	TargetGroupID string `json:"target_group_id"`
}

type MatchResult struct {
	Matched      bool   `json:"matched"`
	GroupMatchID string `json:"group_match_id,omitempty"`
}

type GroupMatch struct {
	ID        string    `json:"id"`
	MyGroupID string    `json:"my_group_id"`
	Other     Group     `json:"other_group"`
	CreatedAt time.Time `json:"created_at"`
}

type BrowseFilter struct {
	ForGroup string
	Activity string
	RadiusKm int
	MinSize  int
	MaxSize  int
	Limit    int
	Offset   int
}

type ScheduleRequest struct {
	GroupMatchID string    `json:"group_match_id"`
	Venue        string    `json:"venue"`
	Address      string    `json:"address"`
	StartsAt     time.Time `json:"starts_at"`
}

type DateView struct {
	ID           string    `json:"id"`
	GroupMatchID string    `json:"group_match_id"`
	Venue        string    `json:"venue"`
	Address      string    `json:"address,omitempty"`
	StartsAt     time.Time `json:"starts_at"`
	Status       string    `json:"status"`
	Confirmed    int       `json:"confirmations"`
	IConfirmed   bool      `json:"i_confirmed"`
}

type Suggestions struct {
	Venues      []string `json:"venues,omitempty"`
	Icebreakers []string `json:"icebreakers,omitempty"`
	Plan        []string `json:"plan,omitempty"` // a short outing plan (language-model wingman only)
	Source      string   `json:"source"`         // "llm", "ai" (templates in the AI service) or "fallback"
}
