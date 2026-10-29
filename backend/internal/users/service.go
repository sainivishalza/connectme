package users

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"connectme/internal/entitlements"
	"connectme/pkg/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	genders     = map[string]bool{"man": true, "woman": true, "nonbinary": true, "other": true}
	lookingFors = map[string]bool{"men": true, "women": true, "everyone": true}
	tagRE       = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} \-]{0,23}$`)
)

type Service struct {
	db    *pgxpool.Pool
	media MediaStore
	scan  MediaScanner
	now   func() time.Time

	checker ContentChecker // optional AI moderation
	devBypass bool // development only: screening failures let content through
}

func NewService(db *pgxpool.Pool, media MediaStore, scan MediaScanner) *Service {
	return &Service{db: db, media: media, scan: scan, now: time.Now}
}

// GetMe returns the caller's own profile.
func (s *Service) GetMe(ctx context.Context, userID string) (*OwnProfile, error) {
	var (
		p                          OwnProfile
		name, bio, intro           *string
		gender, lookingFor, mood   *string
		photosRaw                  []byte
		hasLoc, emailOK, idChecked bool
	)
	err := s.db.QueryRow(ctx, `
		SELECT p.public_ref::text, p.mode, p.display_name, p.bio, p.intro, p.gender, p.looking_for,
		       p.mood, p.tags, p.photos, p.location IS NOT NULL, p.invisible,
		       date_part('year', age(u.date_of_birth))::int, u.is_verified, u.id_verified_at IS NOT NULL,
		       COALESCE(p.voice_seconds, 0), p.onboarded_at IS NOT NULL
		  FROM profiles p JOIN users u ON u.id = p.user_id
		 WHERE p.user_id = $1`, userID).
		Scan(&p.Ref, &p.Mode, &name, &bio, &intro, &gender, &lookingFor, &mood,
			&p.Tags, &photosRaw, &hasLoc, &p.Invisible, &p.Age, &emailOK, &idChecked, &p.VoiceIntroS, &p.Onboarded)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.DisplayName, p.Bio, p.Intro = deref(name), deref(bio), deref(intro)
	p.Gender, p.LookingFor, p.Mood = deref(gender), deref(lookingFor), deref(mood)
	p.HasLocation, p.EmailVerified, p.IDVerified = hasLoc, emailOK, idChecked
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if p.Photos, err = parsePhotos(photosRaw); err != nil {
		return nil, err
	}
	if p.Details, err = s.GetDetails(ctx, userID); err != nil {
		return nil, err
	}
	if p.Prompts, err = s.GetPrompts(ctx, userID); err != nil {
		return nil, err
	}
	p.Completeness = ComputeCompleteness(p)
	return &p, nil
}

// MarkOnboarded records that the member finished (or skipped) the welcome
// steps. It is idempotent.
func (s *Service) MarkOnboarded(ctx context.Context, userID string) error {
	_, err := s.db.Exec(ctx, `UPDATE profiles SET onboarded_at = COALESCE(onboarded_at, NOW()) WHERE user_id = $1::uuid`, userID)
	return err
}

// UpdateProfile applies a partial update after validating every field.
func (s *Service) UpdateProfile(ctx context.Context, userID string, req UpdateProfileRequest) error {
	set := map[string]any{}
	if req.DisplayName != nil {
		v := utils.StripTags(*req.DisplayName)
		if v == "" || len([]rune(v)) > 50 {
			return ValidationError{"display_name must be 1-50 characters"}
		}
		if err := s.VetText(ctx, userID, "name", v); err != nil {
			return err
		}
		set["display_name"] = v
	}
	if req.Bio != nil {
		v := utils.StripTags(*req.Bio)
		if len([]rune(v)) > 500 {
			return ValidationError{"bio must be at most 500 characters"}
		}
		if err := s.VetText(ctx, userID, "bio", v); err != nil {
			return err
		}
		set["bio"] = v
	}
	if req.Intro != nil {
		v := utils.StripTags(*req.Intro)
		if len([]rune(v)) > 140 || strings.ContainsAny(v, "\r\n") {
			return ValidationError{"intro must be a single line of at most 140 characters"}
		}
		if err := s.VetText(ctx, userID, "intro", v); err != nil {
			return err
		}
		set["intro"] = v
	}
	if req.Mood != nil {
		v := utils.StripTags(*req.Mood)
		if len([]rune(v)) > 50 {
			return ValidationError{"mood must be at most 50 characters"}
		}
		if err := s.VetText(ctx, userID, "mood", v); err != nil {
			return err
		}
		set["mood"] = v
	}
	if req.Gender != nil {
		if !genders[*req.Gender] {
			return ValidationError{"gender must be one of: man, woman, nonbinary, other"}
		}
		set["gender"] = *req.Gender
	}
	if req.LookingFor != nil {
		if !lookingFors[*req.LookingFor] {
			return ValidationError{"looking_for must be one of: men, women, everyone"}
		}
		set["looking_for"] = *req.LookingFor
	}
	if req.Tags != nil {
		tags, err := normalizeTags(*req.Tags)
		if err != nil {
			return err
		}
		set["tags"] = tags
	}
	if len(set) == 0 {
		return nil
	}
	// Column names come from the fixed set above, never from client input.
	q, args := "UPDATE profiles SET ", []any{}
	for col, v := range set {
		args = append(args, v)
		q += col + " = $" + itoa(len(args)) + ", "
	}
	args = append(args, userID)
	q += "last_active = NOW() WHERE user_id = $" + itoa(len(args))
	_, err := s.db.Exec(ctx, q, args...)
	return err
}

func normalizeTags(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if !tagRE.MatchString(t) {
			return nil, ValidationError{"tags must be 1-24 letters, numbers, spaces or hyphens"}
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if len(out) > MaxTags {
		return nil, ValidationError{"at most 10 tags"}
	}
	return out, nil
}

// SetMode changes the caller's mode.
//
//   - Only open and incognito can be requested; hotel is set by check-in.
//   - The public_ref is rotated on every real change so anonymous cards cannot
//     be linked to the earlier identity.
//   - Existing matches are unaffected: their visibility is fixed by the mode
//     they were created in (see ResolveVisibility), so switching cannot
//     unmask anyone mid-chat, and going incognito does not hide you from
//     someone who matched with you openly.
//   - While in hotel mode, requesting incognito is a no-op (incognito is
//     forced); requesting open is the user's explicit opt-in.
func (s *Service) SetMode(ctx context.Context, userID string, want Mode) (*OwnProfile, error) {
	if want != ModeOpen && want != ModeIncognito {
		return nil, ValidationError{"mode must be open or incognito"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var cur Mode
	if err := tx.QueryRow(ctx, `SELECT mode FROM profiles WHERE user_id=$1 FOR UPDATE`, userID).Scan(&cur); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	switch {
	case cur == want, cur == ModeHotel && want == ModeIncognito:
		// nothing to change
	default:
		if _, err := tx.Exec(ctx,
			`UPDATE profiles SET mode=$1, public_ref=$2, last_active=NOW() WHERE user_id=$3`,
			string(want), uuid.New(), userID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetMe(ctx, userID)
}

// SetLocation stores the exact point server-side. It is never serialized.
func (s *Service) SetLocation(ctx context.Context, userID string, lat, lng float64) error {
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 || (lat == 0 && lng == 0) {
		return ValidationError{"invalid coordinates"}
	}
	_, err := s.db.Exec(ctx, `
		UPDATE profiles
		   SET location = ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
		       location_updated_at = NOW(), last_active = NOW()
		 WHERE user_id = $3`, lng, lat, userID)
	return err
}

// GetPublic returns another user's profile, by public ref, as the viewer is
// allowed to see it. Blocked, banned, unverified and unknown profiles are all
// "not found".
func (s *Service) GetPublic(ctx context.Context, viewerID, ref string) (*PublicProfile, error) {
	if _, err := uuid.Parse(ref); err != nil {
		return nil, ErrNotFound
	}
	return s.card(ctx, viewerID, "p.public_ref = $1::uuid", ref, "")
}

// Card is GetPublic keyed by internal user id, for server-side callers
// (matches, hotel feeds). A non-empty hotelName marks a hotel-context card:
// distance is omitted and the hotel name is attached (hotel-mode owners only).
func (s *Service) Card(ctx context.Context, viewerID, ownerID, hotelName string) (*PublicProfile, error) {
	return s.card(ctx, viewerID, "p.user_id = $1::uuid", ownerID, hotelName)
}

func (s *Service) card(ctx context.Context, viewerID, where string, arg any, hotelName string) (*PublicProfile, error) {
	var (
		ownerID                    string
		row                        ProfileRow
		photosRaw                  []byte
		nameP, bioP, introP, moodP *string
		refStr                     string
		dist                       *float64
	)
	err := s.db.QueryRow(ctx, `
		SELECT p.user_id::text, p.public_ref::text, p.display_name, p.bio, p.intro, p.mood, p.mode, p.tags, p.photos,
		       date_part('year', age(u.date_of_birth))::int,
		       ST_Distance(ST_SnapToGrid(p.location::geometry, 0.01)::geography,
		                   ST_SnapToGrid(me.location::geometry, 0.01)::geography),
		       COALESCE(`+entitlements.ActiveTierSQL("p.user_id")+` = 'platinum', FALSE)
		  FROM profiles p
		  JOIN users u ON u.id = p.user_id
		  LEFT JOIN profiles me ON me.user_id = $2
		 WHERE `+where+` AND u.is_verified AND NOT u.is_banned
		   AND `+notBlocked("p.user_id", "$2"), arg, viewerID).
		Scan(&ownerID, &refStr, &nameP, &bioP, &introP, &moodP, &row.Mode, &row.Tags, &photosRaw, &row.Age, &dist, &row.VIP)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	row.Ref = refStr
	row.DisplayName, row.Bio, row.Intro, row.Mood = deref(nameP), deref(bioP), deref(introP), deref(moodP)
	row.SnappedDistanceM = -1
	if dist != nil && hotelName == "" {
		row.SnappedDistanceM = *dist
	}
	row.HotelName = hotelName
	if row.Photos, err = parsePhotos(photosRaw); err != nil {
		return nil, err
	}
	rel, err := s.relation(ctx, viewerID, ownerID)
	if err != nil {
		return nil, err
	}
	ps := []PublicProfile{Project(row, ResolveVisibility(row.Mode, rel))}
	s.attachVoice(ctx, ps)
	s.attachDetails(ctx, viewerID, ps)
	return &ps[0], nil
}
