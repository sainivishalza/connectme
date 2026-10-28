package users

import (
	"context"
	"errors"
	"strings"

	"connectme/internal/levels"
	"connectme/pkg/utils"

	"github.com/jackc/pgx/v5"
)

// Allowed values. Keys are stored and sent as-is; the app owns the wording.
var (
	educations = set("high_school", "trade", "college", "bachelor", "master", "doctorate", "other")
	zodiacs    = set("aries", "taurus", "gemini", "cancer", "leo", "virgo", "libra", "scorpio", "sagittarius", "capricorn", "aquarius", "pisces")
	goals      = set("long_term", "short_term", "casual", "friends", "figuring_out")
	kidsOpts   = set("want", "dont_want", "open", "have", "unsure")
	drinkOpts  = set("never", "social", "often")
	exerOpts   = set("never", "sometimes", "often")
	petOpts    = set("dog", "cat", "both", "none", "other")
	langCodes  = set("en", "es", "fr", "de", "it", "pt", "nl", "sv", "pl", "ru", "tr", "ar", "hi", "bn", "ur", "zh", "ja", "ko", "th", "vi", "id", "el", "he", "uk", "ro", "cs", "hu", "fi", "da", "no")
)

func set(v ...string) map[string]bool {
	m := make(map[string]bool, len(v))
	for _, s := range v {
		m[s] = true
	}
	return m
}

// Details is the member's own view of their details (all of it, always).
type Details struct {
	HeightCM  int      `json:"height_cm,omitempty"`
	JobTitle  string   `json:"job_title,omitempty"`
	Education string   `json:"education,omitempty"`
	Hometown  string   `json:"hometown,omitempty"`
	Zodiac    string   `json:"zodiac,omitempty"`
	Goal      string   `json:"goal,omitempty"`
	Kids      string   `json:"kids,omitempty"`
	Drinking  string   `json:"drinking,omitempty"`
	Smoking   string   `json:"smoking,omitempty"`
	Exercise  string   `json:"exercise,omitempty"`
	Pets      string   `json:"pets,omitempty"`
	Languages []string `json:"languages,omitempty"`
}

// PublicDetails is what other people may see. Lifestyle answers appear on
// every card; identifying ones only when the profile is shown in full.
type PublicDetails struct {
	Goal      string   `json:"goal,omitempty"`
	Kids      string   `json:"kids,omitempty"`
	Drinking  string   `json:"drinking,omitempty"`
	Smoking   string   `json:"smoking,omitempty"`
	Exercise  string   `json:"exercise,omitempty"`
	Pets      string   `json:"pets,omitempty"`
	Languages []string `json:"languages,omitempty"`
	// Identifying (omitted on anonymous profiles):
	HeightCM  int    `json:"height_cm,omitempty"`
	JobTitle  string `json:"job_title,omitempty"`
	Education string `json:"education,omitempty"`
	Hometown  string `json:"hometown,omitempty"`
	Zodiac    string `json:"zodiac,omitempty"`
}

// PublicFor returns the part of d that a viewer may see.
func (d Details) PublicFor(anonymous bool) *PublicDetails {
	out := &PublicDetails{Goal: d.Goal, Kids: d.Kids, Drinking: d.Drinking, Smoking: d.Smoking,
		Exercise: d.Exercise, Pets: d.Pets, Languages: d.Languages}
	if !anonymous {
		out.HeightCM, out.JobTitle, out.Education, out.Hometown, out.Zodiac = d.HeightCM, d.JobTitle, d.Education, d.Hometown, d.Zodiac
	}
	if out.empty() {
		return nil
	}
	return out
}

func (o *PublicDetails) empty() bool {
	return o.Goal == "" && o.Kids == "" && o.Drinking == "" && o.Smoking == "" && o.Exercise == "" && o.Pets == "" &&
		len(o.Languages) == 0 && o.HeightCM == 0 && o.JobTitle == "" && o.Education == "" && o.Hometown == "" && o.Zodiac == ""
}

// DetailsRequest is a partial update. A nil field is left alone; an empty
// string (or 0 for height, an empty list for languages) clears the answer.
type DetailsRequest struct {
	HeightCM  *int      `json:"height_cm"`
	JobTitle  *string   `json:"job_title"`
	Education *string   `json:"education"`
	Hometown  *string   `json:"hometown"`
	Zodiac    *string   `json:"zodiac"`
	Goal      *string   `json:"goal"`
	Kids      *string   `json:"kids"`
	Drinking  *string   `json:"drinking"`
	Smoking   *string   `json:"smoking"`
	Exercise  *string   `json:"exercise"`
	Pets      *string   `json:"pets"`
	Languages *[]string `json:"languages"`
}

func oneOf(name string, v *string, allowed map[string]bool, col string, set map[string]any) error {
	if v == nil {
		return nil
	}
	if *v != "" && !allowed[*v] {
		return ValidationError{name + " is not a recognised option"}
	}
	if *v == "" {
		set[col] = nil
	} else {
		set[col] = *v
	}
	return nil
}

func (s *Service) freeText(ctx context.Context, userID, name string, v *string, max int, col string, set map[string]any) error {
	if v == nil {
		return nil
	}
	t := utils.StripTags(*v)
	if n := len([]rune(t)); n > max || strings.ContainsAny(t, "\r\n") {
		return ValidationError{name + " must be a single line of at most " + itoa(max) + " characters"}
	}
	if t == "" {
		set[col] = nil
		return nil
	}
	if err := s.VetText(ctx, userID, "bio", t); err != nil {
		return err
	}
	set[col] = t
	return nil
}

// GetDetails returns the member's own details.
func (s *Service) GetDetails(ctx context.Context, userID string) (Details, error) {
	var d Details
	var h *int16
	var job, edu, home, zod, goal, kids, drink, smoke, ex, pets *string
	err := s.db.QueryRow(ctx, `
		SELECT height_cm, job_title, education, hometown, zodiac, goal, kids, drinking, smoking, exercise, pets, languages
		  FROM profile_details WHERE user_id=$1::uuid`, userID).
		Scan(&h, &job, &edu, &home, &zod, &goal, &kids, &drink, &smoke, &ex, &pets, &d.Languages)
	if errors.Is(err, pgx.ErrNoRows) {
		return Details{}, nil
	}
	if err != nil {
		return Details{}, err
	}
	if h != nil {
		d.HeightCM = int(*h)
	}
	d.JobTitle, d.Education, d.Hometown, d.Zodiac = deref(job), deref(edu), deref(home), deref(zod)
	d.Goal, d.Kids, d.Drinking, d.Smoking, d.Exercise, d.Pets = deref(goal), deref(kids), deref(drink), deref(smoke), deref(ex), deref(pets)
	return d, nil
}

// SetDetails validates and stores a partial update.
func (s *Service) SetDetails(ctx context.Context, userID string, r DetailsRequest) (Details, error) {
	cols := map[string]any{}
	if r.HeightCM != nil {
		switch h := *r.HeightCM; {
		case h == 0:
			cols["height_cm"] = nil
		case h < 120 || h > 230:
			return Details{}, ValidationError{"height must be between 120 and 230 cm"}
		default:
			cols["height_cm"] = h
		}
	}
	for _, f := range []struct {
		name string
		v    *string
		ok   map[string]bool
		col  string
	}{
		{"education", r.Education, educations, "education"}, {"zodiac", r.Zodiac, zodiacs, "zodiac"},
		{"goal", r.Goal, goals, "goal"}, {"kids", r.Kids, kidsOpts, "kids"}, {"drinking", r.Drinking, drinkOpts, "drinking"},
		{"smoking", r.Smoking, drinkOpts, "smoking"}, {"exercise", r.Exercise, exerOpts, "exercise"}, {"pets", r.Pets, petOpts, "pets"},
	} {
		if err := oneOf(f.name, f.v, f.ok, f.col, cols); err != nil {
			return Details{}, err
		}
	}
	if err := s.freeText(ctx, userID, "job title", r.JobTitle, 60, "job_title", cols); err != nil {
		return Details{}, err
	}
	if err := s.freeText(ctx, userID, "hometown", r.Hometown, 60, "hometown", cols); err != nil {
		return Details{}, err
	}
	if r.Languages != nil {
		seen := map[string]bool{}
		langs := []string{}
		for _, l := range *r.Languages {
			if !langCodes[l] {
				return Details{}, ValidationError{"unknown language code " + l}
			}
			if !seen[l] {
				seen[l] = true
				langs = append(langs, l)
			}
		}
		if len(langs) > 5 {
			return Details{}, ValidationError{"choose at most 5 languages"}
		}
		cols["languages"] = langs
	}
	if len(cols) > 0 {
		names, ph, args := []string{"user_id"}, []string{"$1::uuid"}, []any{userID}
		upd := []string{"updated_at = NOW()"}
		for _, c := range []string{"height_cm", "job_title", "education", "hometown", "zodiac", "goal", "kids", "drinking", "smoking", "exercise", "pets", "languages"} {
			v, ok := cols[c]
			if !ok {
				continue
			}
			args = append(args, v)
			names = append(names, c)
			ph = append(ph, "$"+itoa(len(args)))
			upd = append(upd, c+" = EXCLUDED."+c)
		}
		q := "INSERT INTO profile_details (" + strings.Join(names, ",") + ") VALUES (" + strings.Join(ph, ",") +
			") ON CONFLICT (user_id) DO UPDATE SET " + strings.Join(upd, ", ")
		if _, err := s.db.Exec(ctx, q, args...); err != nil {
			return Details{}, err
		}
	}
	return s.GetDetails(ctx, userID)
}

// attachDetails fills Details (and the ID-verified badge) on a list of cards.
// Anonymous cards get the lifestyle subset only; prompts appear on full
// profiles only. A failure here must never break a profile list.
func (s *Service) attachDetails(ctx context.Context, viewerID string, ps []PublicProfile) {
	var refs []string
	idx := map[string][]int{}
	for i, p := range ps {
		refs = append(refs, p.Ref)
		idx[p.Ref] = append(idx[p.Ref], i)
	}
	if len(refs) == 0 {
		return
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.public_ref::text, u.id_verified_at IS NOT NULL,
		       d.height_cm, d.job_title, d.education, d.hometown, d.zodiac, d.goal, d.kids, d.drinking, d.smoking, d.exercise, d.pets, d.languages
		  FROM profiles p JOIN users u ON u.id = p.user_id LEFT JOIN profile_details d ON d.user_id = p.user_id
		 WHERE p.public_ref = ANY($1::uuid[])`, refs)
	if err != nil {
		return
	}
	for rows.Next() {
		var ref string
		var ver bool
		var d Details
		var h *int16
		var job, edu, home, zod, goal, kids, drink, smoke, ex, pets *string
		if rows.Scan(&ref, &ver, &h, &job, &edu, &home, &zod, &goal, &kids, &drink, &smoke, &ex, &pets, &d.Languages) != nil {
			continue
		}
		if h != nil {
			d.HeightCM = int(*h)
		}
		d.JobTitle, d.Education, d.Hometown, d.Zodiac = deref(job), deref(edu), deref(home), deref(zod)
		d.Goal, d.Kids, d.Drinking, d.Smoking, d.Exercise, d.Pets = deref(goal), deref(kids), deref(drink), deref(smoke), deref(ex), deref(pets)
		for _, i := range idx[ref] {
			ps[i].IDVerified = ver
			ps[i].Details = d.PublicFor(ps[i].Anonymous)
		}
	}
	rows.Close()
	s.attachPrompts(ctx, ps, idx)
	s.attachCharm(ctx, ps, idx)
	s.attachFollows(ctx, viewerID, ps, idx)
	s.attachQuiz(ctx, viewerID, ps, idx)
}

// attachCharm adds the Charm level badge to profiles shown in full.
func (s *Service) attachCharm(ctx context.Context, ps []PublicProfile, idx map[string][]int) {
	var refs []string
	for ref, is := range idx {
		for _, i := range is {
			if !ps[i].Anonymous {
				refs = append(refs, ref)
				break
			}
		}
	}
	lv, err := levels.CharmLevels(ctx, s.db, refs)
	if err != nil {
		return
	}
	for ref, l := range lv {
		for _, i := range idx[ref] {
			if !ps[i].Anonymous {
				ps[i].CharmLevel = l
			}
		}
	}
}
