package integration

import "testing"

// Display names end up in email subjects, push text and staff screens, so they
// are single-line text: no line breaks or other control characters.
func TestDisplayNameRejectsControlCharacters(t *testing.T) {
	e := setup(t)
	u := e.user("Name Rules")
	for _, bad := range []string{
		"Anna\r\nBcc: victim@example.com",
		"Anna\nSecond line",
		"Anna\x00",
		"Anna Bcc: x",
		"Anna\x1b[31m",
	} {
		if c, b := e.call("PATCH", "/users/me", map[string]string{"display_name": bad}, u); c != 422 {
			t.Errorf("display_name %q: want 422, got %d %s", bad, c, b)
		}
	}
	for _, good := range []string{"Zoë", "李雷", "Anna-Marie O'Neil", "Priya  K."} {
		if c, b := e.call("PATCH", "/users/me", map[string]string{"display_name": good}, u); c != 200 {
			t.Errorf("display_name %q: want 200, got %d %s", good, c, b)
		}
	}
}
