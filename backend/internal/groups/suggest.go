package groups

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Suggester proposes venue ideas and icebreakers. Implementations receive
// only coarse, non-identifying input: activity, group size and the members'
// public interest tags. Never names, locations or chat content.
type Suggester interface {
	Suggest(ctx context.Context, in SuggestInput) (Suggestions, error)
}

type SuggestInput struct {
	Activity   string   `json:"activity"`
	GroupSize  int      `json:"group_size"`
	SharedTags []string `json:"shared_tags"`
}

// FallbackSuggester is deterministic and offline; it is used when the AI
// service is down so the feature degrades instead of failing.
type FallbackSuggester struct{}

var venueIdeas = map[string][]string{
	"dinner":    {"A casual restaurant with shareable plates", "A rooftop or terrace dining spot", "A food hall with many options"},
	"hiking":    {"A well-marked trail close to town", "A scenic viewpoint with a short walk", "A park loop with a café at the end"},
	"concert":   {"A small live-music venue", "An open-air show or festival", "A bar with a local band night"},
	"bar_crawl": {"A cluster of bars within walking distance", "A cocktail bar followed by a pub", "A district known for nightlife"},
	"custom":    {"A neutral public place both groups can reach easily", "A café with plenty of seating", "A popular public venue with good transport"},
}

func (FallbackSuggester) Suggest(_ context.Context, in SuggestInput) (Suggestions, error) {
	v, ok := venueIdeas[in.Activity]
	if !ok {
		v = venueIdeas["custom"]
	}
	ice := []string{
		"What's the best trip any of you have taken together?",
		"Two truths and a lie, groups edition",
		"What would your group's dream weekend look like?",
		"Which of you is the most likely to get lost, and why?",
		"What's a small thing that always makes your day better?",
	}
	for _, t := range in.SharedTags {
		if len(ice) >= 7 {
			break
		}
		ice = append(ice, fmt.Sprintf("You both seem into %s: what got you started?", t))
	}
	return Suggestions{Venues: v, Icebreakers: ice, Source: "fallback"}, nil
}

// HTTPSuggester calls the AI service (Step 9) and falls back on any failure.
type HTTPSuggester struct {
	BaseURL  string
	Key      string // shared secret for the AI service
	Client   *http.Client
	Fallback Suggester
}

func NewHTTPSuggester(baseURL, key string) *HTTPSuggester {
	return &HTTPSuggester{BaseURL: baseURL, Key: key, Client: &http.Client{Timeout: 3 * time.Second}, Fallback: FallbackSuggester{}}
}

func (h *HTTPSuggester) Suggest(ctx context.Context, in SuggestInput) (Suggestions, error) {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.BaseURL+"/wingman/suggest", bytes.NewReader(body))
	if err != nil {
		return h.Fallback.Suggest(ctx, in)
	}
	req.Header.Set("Content-Type", "application/json")
	if h.Key != "" {
		req.Header.Set("X-Internal-Key", h.Key)
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return h.Fallback.Suggest(ctx, in)
	}
	defer resp.Body.Close()
	var out Suggestions
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out) != nil ||
		len(out.Venues)+len(out.Icebreakers) == 0 {
		return h.Fallback.Suggest(ctx, in)
	}
	out.Source = "ai"
	if len(out.Venues) > 5 {
		out.Venues = out.Venues[:5]
	}
	if len(out.Icebreakers) > 7 {
		out.Icebreakers = out.Icebreakers[:7]
	}
	return out, nil
}

// Suggest gathers privacy-safe inputs for a group the caller belongs to.
func (s *Service) Suggest(ctx context.Context, userID, groupID string) (*Suggestions, error) {
	ok, err := isMember(ctx, s.db, userID, groupID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	var in SuggestInput
	err = s.db.QueryRow(ctx, `
		SELECT g.activity, (SELECT COUNT(*) FROM group_members WHERE group_id=g.id),
		       COALESCE((SELECT array_agg(t) FROM (
		           SELECT t FROM group_members m JOIN profiles p ON p.user_id = m.user_id, unnest(p.tags) t
		            WHERE m.group_id = g.id GROUP BY t ORDER BY COUNT(*) DESC, t LIMIT 5) x), '{}')
		  FROM groups g WHERE g.id=$1::uuid`, groupID).Scan(&in.Activity, &in.GroupSize, &in.SharedTags)
	if err != nil {
		return nil, err
	}
	out, err := s.suggester.Suggest(ctx, in)
	if err != nil {
		out, _ = FallbackSuggester{}.Suggest(ctx, in)
	}
	return &out, nil
}
