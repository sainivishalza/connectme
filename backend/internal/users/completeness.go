package users

// Completeness tells a member how finished their profile is and what to add
// next. Each step is worth a share of 100; the app turns step keys into words.
type Completeness struct {
	Percent   int      `json:"percent"`
	NextSteps []string `json:"next_steps"`
}

type step struct {
	key    string
	points int
	done   func(p OwnProfile) bool
}

func lifestyleCount(d Details) int {
	n := 0
	for _, v := range []string{d.Goal, d.Kids, d.Drinking, d.Smoking, d.Exercise, d.Pets} {
		if v != "" {
			n++
		}
	}
	return n
}

var completenessSteps = []step{
	{"add_photo", 20, func(p OwnProfile) bool { return len(p.Photos) >= 1 }},
	{"add_more_photos", 10, func(p OwnProfile) bool { return len(p.Photos) >= 3 }},
	{"write_intro", 10, func(p OwnProfile) bool { return p.Intro != "" }},
	{"write_bio", 10, func(p OwnProfile) bool { return len(p.Bio) >= 40 }},
	{"pick_interests", 10, func(p OwnProfile) bool { return len(p.Tags) >= 3 }},
	{"set_goal", 10, func(p OwnProfile) bool { return p.Details.Goal != "" }},
	{"fill_lifestyle", 10, func(p OwnProfile) bool { return lifestyleCount(p.Details) >= 4 }},
	{"answer_prompts", 10, func(p OwnProfile) bool { return len(p.Prompts) >= 2 }},
	{"share_location", 5, func(p OwnProfile) bool { return p.HasLocation }},
	{"verify_id", 5, func(p OwnProfile) bool { return p.IDVerified }},
}

// ComputeCompleteness scores a profile against the steps above.
func ComputeCompleteness(p OwnProfile) Completeness {
	c := Completeness{NextSteps: []string{}}
	for _, s := range completenessSteps {
		if s.done(p) {
			c.Percent += s.points
		} else {
			c.NextSteps = append(c.NextSteps, s.key)
		}
	}
	return c
}
