// Package users owns profiles, the dual-mode identity rules and nearby search.
package users

import (
	"errors"
	"time"
)

type Mode string

const (
	ModeOpen      Mode = "open"
	ModeIncognito Mode = "incognito"
	ModeHotel     Mode = "hotel" // set only by the geo service on check-in
)

const (
	MaxPhotos = 6
	MaxTags   = 10
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
)

// ValidationError carries a message that is safe to show to clients.
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

// PhotoMeta is the stored record for an uploaded photo (the bytes live in MediaStore).
type PhotoMeta struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// OwnProfile is what a user sees about themselves. It never contains
// coordinates, only whether a location is set.
type OwnProfile struct {
	Ref           string      `json:"ref"`
	Mode          Mode        `json:"mode"`
	DisplayName   string      `json:"display_name"`
	Bio           string      `json:"bio"`
	Intro         string      `json:"intro"`
	Age           int         `json:"age"`
	Gender        string      `json:"gender,omitempty"`
	LookingFor    string      `json:"looking_for,omitempty"`
	Mood          string      `json:"mood,omitempty"`
	Tags          []string    `json:"tags"`
	Photos        []PhotoMeta `json:"photos"`
	HasLocation   bool        `json:"has_location"`
	Invisible     bool        `json:"invisible"`
	EmailVerified bool        `json:"email_verified"`
	IDVerified    bool        `json:"id_verified"`
	VoiceIntroS   int         `json:"voice_intro_s"`
}

// UpdateProfileRequest is a partial update: nil fields are left unchanged.
type UpdateProfileRequest struct {
	DisplayName *string   `json:"display_name"`
	Bio         *string   `json:"bio"`
	Intro       *string   `json:"intro"`
	Gender      *string   `json:"gender"`
	LookingFor  *string   `json:"looking_for"`
	Mood        *string   `json:"mood"`
	Tags        *[]string `json:"tags"`
}

type SetModeRequest struct {
	Mode Mode `json:"mode"`
}

type LocationRequest struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// NearbyFilter is parsed from the query string.
type NearbyFilter struct {
	RadiusKm int
	AgeMin   int
	AgeMax   int
	Gender   string
	Tags     []string
	Mode     Mode
	Limit    int
	Offset   int
}
