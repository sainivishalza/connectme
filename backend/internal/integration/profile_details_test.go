package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func (e *env) putDetails(uid string, body map[string]any) (int, string) {
	e.t.Helper()
	c, b := e.call("PUT", "/users/me/details", body, uid)
	return c, string(b)
}

func (e *env) fillProfile(uid string) {
	e.t.Helper()
	if c, b := e.putDetails(uid, map[string]any{
		"height_cm": 178, "job_title": "Nurse", "education": "bachelor", "hometown": "Porto", "zodiac": "leo",
		"goal": "long_term", "kids": "want", "drinking": "social", "smoking": "never", "exercise": "often", "pets": "dog",
		"languages": []string{"en", "pt"},
	}); c != 200 {
		e.t.Fatalf("details: %d %s", c, b)
	}
	if c, b := e.call("PUT", "/users/me/prompts", map[string]any{"prompts": []map[string]string{
		{"key": "perfect_sunday", "answer": "Market, long lunch, a nap."},
		{"key": "geek_out", "answer": "Bread. Sourdough, mostly."},
	}}, uid); c != 200 {
		e.t.Fatalf("prompts: %d %s", c, b)
	}
}

func TestRichProfilesShowLifestyleToEveryoneButIdentifyingDetailsOnlyInFull(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	owner := e.located("Rich Owner", lat, lng)
	viewer := e.located("Viewer", lat+0.003, lng)
	e.fillProfile(owner)
	ref := e.refOf(owner)

	// Open mode: everything is visible, including prompts and the lifestyle chips.
	_, b := e.call("GET", "/users/"+ref, nil, viewer)
	for _, want := range []string{`"job_title":"Nurse"`, `"hometown":"Porto"`, `"height_cm":178`, `"education":"bachelor"`, `"goal":"long_term"`, `"prompt":"My perfect Sunday is"`, "Market, long lunch"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("open profile lacks %s: %s", want, b)
		}
	}

	// Incognito: lifestyle chips stay (they say what someone wants, not who they are);
	// everything that could identify them, and all free text, disappears.
	if c, b := e.call("POST", "/users/me/mode", map[string]string{"mode": "incognito"}, owner); c != 200 {
		t.Fatalf("mode: %d %s", c, b)
	}
	if c, _ := e.call("GET", "/users/"+ref, nil, viewer); c != 404 {
		t.Errorf("an incognito stranger can be opened directly: %d", c)
	}
	for _, path := range []string{"/users/nearby?radius_km=25"} {
		_, b := e.call("GET", path, nil, viewer)
		body := string(b)
		for _, want := range []string{`"goal":"long_term"`, `"kids":"want"`, `"smoking":"never"`, `"languages":["en","pt"]`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: anonymous card lacks %s: %s", path, want, body)
			}
		}
		for _, leak := range []string{"Nurse", "Porto", "bachelor", "leo", "job_title", "hometown", "height_cm", "Market, long lunch", "sourdough", "Sourdough", `"prompts"`, "Rich Owner"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s: anonymous profile leaks %q: %s", path, leak, body)
			}
		}
	}
}

func TestProfileDetailsAreValidatedScreenedAndAreOptional(t *testing.T) {
	e := setup(t)
	u := e.user("Detail Tester")
	bad := map[string]map[string]any{
		"height low":    {"height_cm": 90},
		"height high":   {"height_cm": 300},
		"goal":          {"goal": "marry_a_millionaire"},
		"kids":          {"kids": "lots"},
		"education":     {"education": "wizard"},
		"zodiac":        {"zodiac": "ophiuchus"},
		"language":      {"languages": []string{"en", "klingon"}},
		"six languages": {"languages": []string{"en", "es", "fr", "de", "it", "pt"}},
		"long job":      {"job_title": strings.Repeat("x", 61)},
		"multi-line":    {"hometown": "Porto\nPortugal"},
	}
	for name, body := range bad {
		if c, _ := e.putDetails(u, body); c != 422 {
			t.Errorf("%s accepted: %d", name, c)
		}
	}
	if c, _ := e.call("PUT", "/users/me/details", map[string]any{"religion": "x"}, u); c != 400 {
		t.Errorf("unknown field accepted: %d", c)
	}
	// Partial updates leave the rest alone; empty values clear.
	e.putDetails(u, map[string]any{"goal": "casual", "smoking": "often"})
	e.putDetails(u, map[string]any{"goal": "friends"})
	_, b := e.call("GET", "/users/me", nil, u)
	if !strings.Contains(string(b), `"goal":"friends"`) || !strings.Contains(string(b), `"smoking":"often"`) {
		t.Errorf("partial update: %s", b)
	}
	e.putDetails(u, map[string]any{"smoking": "", "languages": []string{}})
	_, b = e.call("GET", "/users/me", nil, u)
	if strings.Contains(string(b), `"smoking"`) {
		t.Errorf("clearing did not clear: %s", b)
	}
	// Prompts: known questions only, once each, three at most, one-line answers.
	prompts := func(p ...map[string]string) int {
		c, _ := e.call("PUT", "/users/me/prompts", map[string]any{"prompts": p}, u)
		return c
	}
	if c := prompts(map[string]string{"key": "nonsense", "answer": "hello there"}); c != 422 {
		t.Errorf("unknown prompt: %d", c)
	}
	if c := prompts(map[string]string{"key": "geek_out", "answer": "a"}, map[string]string{"key": "geek_out", "answer": "again"}); c != 422 {
		t.Errorf("duplicate/short: %d", c)
	}
	if c := prompts(map[string]string{"key": "geek_out", "answer": "two\nlines"}); c != 422 {
		t.Errorf("multi-line answer: %d", c)
	}
	four := []map[string]string{{"key": "geek_out", "answer": "Maps"}, {"key": "fun_fact", "answer": "I juggle"}, {"key": "best_trip", "answer": "Lisbon"}, {"key": "date_idea", "answer": "A walk"}}
	if c := prompts(four...); c != 422 {
		t.Errorf("four prompts: %d", c)
	}
	if c := prompts(four[:3]...); c != 200 {
		t.Errorf("three prompts: %d", c)
	}
	if c := prompts(); c != 200 {
		t.Errorf("clearing prompts: %d", c)
	}
	// The question list is available to the app.
	if c, b := e.call("GET", "/users/prompts", nil, u); c != 200 || !strings.Contains(string(b), "perfect_sunday") {
		t.Errorf("prompt bank: %d %s", c, b)
	}
}

func TestDiscoveryCanBeFilteredByWhatPeopleWantAndByVerification(t *testing.T) {
	e := setup(t)
	lat, lng := randomBase()
	me := e.located("Filterer", lat, lng)
	serious := e.located("Serious", lat+0.002, lng)
	casual := e.located("Casual", lat+0.003, lng)
	blank := e.located("Blank", lat+0.004, lng)
	e.putDetails(serious, map[string]any{"goal": "long_term", "smoking": "never", "languages": []string{"de"}})
	e.putDetails(casual, map[string]any{"goal": "casual", "smoking": "often"})
	e.exec(`UPDATE users SET id_verified_at = NOW() WHERE id = $1::uuid`, casual)
	_ = blank

	names := func(q string) string { return strings.Join(e.nearbyNames(me, "radius_km=25&"+q), ",") }
	if got := names(""); !strings.Contains(got, "Blank") || !strings.Contains(got, "Serious") || !strings.Contains(got, "Casual") {
		t.Errorf("no filter: %s", got)
	}
	if got := names("goal=long_term"); got != "Serious" {
		t.Errorf("goal filter: %q", got)
	}
	if got := names("smoking=never"); got != "Serious" {
		t.Errorf("smoking filter: %q", got)
	}
	if got := names("language=de"); got != "Serious" {
		t.Errorf("language filter: %q", got)
	}
	if got := names("verified=true"); got != "Casual" {
		t.Errorf("verified filter: %q", got)
	}
	if got := names("goal=long_term&smoking=often"); got != "" {
		t.Errorf("combined filters should match nobody: %q", got)
	}
	if c, _ := e.call("GET", "/users/nearby?goal=bogus", nil, me); c != 422 {
		t.Errorf("invalid filter value: %d", c)
	}
	// The verified badge is on the card.
	_, b := e.call("GET", "/users/nearby?radius_km=25&verified=true", nil, me)
	if !strings.Contains(string(b), `"id_verified":true`) {
		t.Errorf("badge missing: %s", b)
	}
}

func TestProfileCompletenessGuidesTheNextStep(t *testing.T) {
	e := setup(t)
	u := e.user("Newcomer")
	type comp struct {
		Completeness struct {
			Percent   int      `json:"percent"`
			NextSteps []string `json:"next_steps"`
		} `json:"completeness"`
	}
	get := func() comp {
		_, b := e.call("GET", "/users/me", nil, u)
		var c comp
		_ = json.Unmarshal(b, &c)
		return c
	}
	before := get()
	if before.Completeness.Percent >= 50 || len(before.Completeness.NextSteps) < 5 {
		t.Fatalf("a bare profile: %+v", before)
	}
	has := func(c comp, step string) bool {
		for _, s := range c.Completeness.NextSteps {
			if s == step {
				return true
			}
		}
		return false
	}
	if !has(before, "set_goal") || !has(before, "answer_prompts") || !has(before, "add_photo") {
		t.Errorf("expected nudges: %+v", before)
	}
	e.fillProfile(u)
	after := get()
	if after.Completeness.Percent <= before.Completeness.Percent || has(after, "set_goal") || has(after, "answer_prompts") || has(after, "fill_lifestyle") {
		t.Errorf("filling the profile did not move the meter: %+v -> %+v", before, after)
	}
}
