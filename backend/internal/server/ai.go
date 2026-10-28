package server

import (
	"context"

	"connectme/internal/aiclient"
	"connectme/internal/matching"
	"connectme/internal/users"
)

// aiAdapter lets the AI client satisfy the small interfaces the feature
// packages define, so those packages never import the HTTP client.
type aiAdapter struct{ c *aiclient.Client }

var (
	_ users.ContentChecker = aiAdapter{}
	_ matching.Scorer      = aiAdapter{}
)

func (a aiAdapter) CheckText(ctx context.Context, kind, text string) (users.Decision, error) {
	v, err := a.c.ModerateText(ctx, kind, text)
	if err != nil {
		return users.Decision{}, err
	}
	return users.Decision{Action: v.Action, Reason: v.Reason, Categories: v.Categories}, nil
}

func (a aiAdapter) CheckImage(ctx context.Context, data []byte) (users.Decision, error) {
	v, err := a.c.ModerateImage(ctx, data)
	if err != nil {
		return users.Decision{}, err
	}
	return users.Decision{Action: v.Action, Reason: v.Reason, Categories: v.Categories}, nil
}

func (a aiAdapter) Score(ctx context.Context, in matching.ScoreInput) (*matching.ScoreResult, error) {
	person := func(p matching.ScorePerson) aiclient.Person {
		tags := p.Tags
		if tags == nil {
			tags = []string{}
		}
		return aiclient.Person{Tags: tags, Age: p.Age, ActiveHours: p.ActiveHours, Lifestyle: p.Lifestyle}
	}
	r, err := a.c.Score(ctx, aiclient.ScoreRequest{
		A: person(in.A), B: person(in.B), History: aiclient.History{AToB: in.AToB, BToA: in.BToA},
	})
	if err != nil {
		return nil, err
	}
	out := &matching.ScoreResult{Score: r.Score}
	for _, x := range r.Reasons {
		out.Reasons = append(out.Reasons, matching.ScoreReason{Code: x.Code, Text: x.Text})
	}
	return out, nil
}
