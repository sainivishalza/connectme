package users

import "fmt"

// Visibility is how much of a profile a particular viewer may see.
type Visibility int

const (
	VisAnonymous Visibility = iota // tags, intro, mood, distance only
	VisFull                        // whole profile
)

// Relation describes how a viewer is connected to the profile owner.
type Relation struct {
	Self           bool
	Matched        bool
	MatchMode      Mode // mode the match was created in; immutable
	ViewerRevealed bool
	TargetRevealed bool
}

// ResolveVisibility is the single source of truth for who may see what.
//
//   - Your own profile is always full.
//   - If you are matched, the mode the match was created in governs: an open
//     match is full; an incognito/hotel match stays anonymous until BOTH
//     people have revealed. Because it is keyed on the match's mode, one side
//     switching their global mode cannot unmask themselves mid-chat.
//   - Otherwise the owner's current mode decides.
func ResolveVisibility(targetMode Mode, rel Relation) Visibility {
	switch {
	case rel.Self:
		return VisFull
	case rel.Matched:
		if rel.MatchMode == ModeOpen || (rel.ViewerRevealed && rel.TargetRevealed) {
			return VisFull
		}
		return VisAnonymous
	case targetMode == ModeOpen:
		return VisFull
	default:
		return VisAnonymous
	}
}

// ProfileRow is the internal, complete view of a profile as loaded from the
// database. It must never be serialized to a client; use Project.
type ProfileRow struct {
	Ref         string
	DisplayName string
	Bio         string
	Intro       string
	Mood        string
	Mode        Mode
	Age         int
	Tags        []string
	Photos      []PhotoMeta
	// SnappedDistanceM is the distance between grid-snapped locations
	// (negative = unknown). Exact coordinates never reach this layer's output.
	SnappedDistanceM float64
	// HotelName is set by the geo layer only when the viewer is checked in at
	// the same hotel.
	HotelName string
	// VIP is the Platinum badge, shown on cards (it identifies nobody).
	VIP bool
}

// PublicProfile is the ONLY shape in which another user's profile is ever
// serialized. Fields not allowed at the viewer's visibility are left zero and
// omitted from JSON.
type PublicProfile struct {
	Ref         string   `json:"ref"`
	Anonymous   bool     `json:"anonymous"`
	Mode        Mode     `json:"mode"`
	DisplayName string   `json:"display_name,omitempty"`
	Bio         string   `json:"bio,omitempty"`
	Age         int      `json:"age,omitempty"`
	Photos      []string `json:"photos,omitempty"`
	Tags        []string `json:"tags"`
	Intro       string   `json:"intro,omitempty"`
	Mood        string   `json:"mood,omitempty"`
	Distance    string   `json:"distance,omitempty"`
	HotelName   string   `json:"hotel_name,omitempty"`
	// TravelMood is the guest's chosen mood for this stay (business, company,
	// guide); set by the geo layer on hotel-feed cards only.
	TravelMood string `json:"travel_mood,omitempty"`
	VIP        bool   `json:"vip,omitempty"`
	// VoiceIntroS is the length of the person's voice intro in seconds (0 = none).
	// It is only ever set for profiles shown in full, never for anonymous ones.
	VoiceIntroS int `json:"voice_intro_s,omitempty"`
	// ReelS is the length of the person's profile video in seconds (0 = none).
	// Like the voice intro it is only ever set on non-anonymous profiles.
	ReelS int `json:"reel_s,omitempty"`
	// Chapters is how many narrative chapters the profile has (non-anonymous only).
	Chapters int `json:"chapters,omitempty"`
}

// PhotoURL is the authenticated path a photo is served from. Direct storage
// paths are never exposed, so visibility is re-checked on every fetch.
func PhotoURL(id string) string { return "/media/photos/" + id }

// Project builds the client-facing profile for a viewer.
func Project(row ProfileRow, vis Visibility) PublicProfile {
	p := PublicProfile{
		Ref:       row.Ref,
		Anonymous: vis != VisFull,
		Mode:      row.Mode,
		Tags:      append([]string{}, row.Tags...),
		Intro:     row.Intro,
		Mood:      row.Mood,
		Distance:  DistanceLabel(row.SnappedDistanceM),
		HotelName: row.HotelName,
		VIP:       row.VIP,
	}
	if row.Mode != ModeHotel {
		p.HotelName = ""
	}
	if vis == VisFull {
		p.DisplayName = row.DisplayName
		p.Bio = row.Bio
		p.Age = row.Age
		for _, ph := range row.Photos {
			p.Photos = append(p.Photos, PhotoURL(ph.ID))
		}
	}
	return p
}

// distanceBuckets are the only distances ever shown ("within N km").
var distanceBuckets = []int{1, 2, 5, 10, 25, 50, 100}

// RadiusOptions are the radii a client may filter by; restricting them
// prevents narrowing a radius filter to pinpoint someone.
func RadiusOptions() []int { return distanceBuckets }

// DistanceLabel converts a (grid-snapped) distance in metres to a coarse label.
func DistanceLabel(meters float64) string {
	if meters < 0 {
		return ""
	}
	km := meters / 1000
	for _, b := range distanceBuckets {
		if km <= float64(b) {
			return fmt.Sprintf("within %d km", b)
		}
	}
	return "100+ km"
}
