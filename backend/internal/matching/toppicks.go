package matching

import (
	"context"
	"sort"
	"strings"

	"connectme/internal/entitlements"
	"connectme/internal/users"
)

const (
	topPicksFree = 3
	topPicksGold = 10
)

// Pick is one of today's best people for the caller and the plain-words reason.
type Pick struct {
	Profile users.PublicProfile `json:"profile"`
	Why     string              `json:"why"`
}

var goalWords = map[string]string{
	"long_term": "a long-term relationship", "short_term": "something short-term", "casual": "something casual",
	"friends": "new friends", "figuring_out": "to see where things go",
}

// TopPicks ranks the people the caller could meet right now (the same set as
// discovery: nearby, visible, not yet swiped, not blocked) by how much they
// have in common: shared interests and agreement on what they want. Gold sees
// more picks. The ranking is simple on purpose and is explained to the member
// in a sentence; it does not call the AI service, so it works when that is down.
func (s *Service) TopPicks(ctx context.Context, userID string) ([]Pick, error) {
	tier, err := entitlements.TierOf(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	n := topPicksFree
	if tier.AtLeast(entitlements.Gold) {
		n = topPicksGold
	}
	me, err := s.users.GetMe(ctx, userID)
	if err != nil {
		return nil, err
	}
	cands, err := s.users.Nearby(ctx, userID, users.NearbyFilter{RadiusKm: 25, AgeMin: 18, AgeMax: 120, Limit: 50})
	if err != nil {
		return nil, err
	}
	type scored struct {
		pick  Pick
		score float64
		order int
	}
	var all []scored
	for i, c := range cands {
		sc, why := pickScore(me, c)
		all = append(all, scored{Pick{Profile: c, Why: why}, sc, i})
	}
	// Best first; on a tie, the nearer person (discovery already orders by distance).
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].order < all[j].order
	})
	out := []Pick{}
	for _, a := range all {
		if len(out) >= n {
			break
		}
		out = append(out, a.pick)
	}
	return out, nil
}

// pickScore weighs shared interests and agreement on what each wants, and
// returns a sentence for the strongest reason.
func pickScore(me *users.OwnProfile, c users.PublicProfile) (float64, string) {
	mine := map[string]bool{}
	for _, t := range me.Tags {
		mine[strings.ToLower(t)] = true
	}
	var shared []string
	for _, t := range c.Tags {
		if mine[strings.ToLower(t)] {
			shared = append(shared, t)
		}
	}
	score := 3 * float64(min(len(shared), 4))
	why := ""
	if len(shared) > 0 {
		if len(shared) > 3 {
			shared = shared[:3]
		}
		why = "You both like " + strings.Join(shared, ", ")
	}
	if d := c.Details; d != nil {
		agree := func(a, b string, w float64) {
			if a != "" && a == b {
				score += w
			}
		}
		if me.Details.Goal != "" && d.Goal != "" {
			switch {
			case me.Details.Goal == d.Goal:
				score += 6
				if why == "" || me.Details.Goal != "figuring_out" {
					why = "You are both looking for " + goalWords[d.Goal]
				}
			case pairIs(me.Details.Goal, d.Goal, "long_term", "short_term"), pairIs(me.Details.Goal, d.Goal, "short_term", "casual"):
				score += 2.5
			}
		}
		agree(me.Details.Kids, d.Kids, 3)
		agree(me.Details.Smoking, d.Smoking, 2)
		agree(me.Details.Drinking, d.Drinking, 1.5)
		agree(me.Details.Exercise, d.Exercise, 1)
	}
	if c.IDVerified {
		score++
	}
	if why == "" {
		why = "Someone new near you"
	}
	return score, why
}

func pairIs(a, b, x, y string) bool { return (a == x && b == y) || (a == y && b == x) }
