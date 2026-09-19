package cooklang

import "strings"
import "testing"

func TestTimerSuggestionCap(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		timer bool
	}{
		{"cooklang 20 min", "Simmer for ~{20%minutes}.", true},
		{"cooklang 4 hours", "Braise for ~{4%hours}.", true},
		{"cooklang 5 hours is the boundary", "Brine for ~{5%hours}.", true},
		{"cooklang 6 hours", "Brine for ~{6%hours}.", false},
		{"cooklang 24 hours", "Ferment for ~{24%hours}.", false},
		{"cooklang zero", "Rest for ~{0%minutes}.", false},
		{"range 2-3 minutes", "Knead for 2-3 minutes until smooth.", true},
		{"range 4-5 hours takes upper bound", "Smoke for 4-5 hours.", true},
		{"range 5-6 hours", "Smoke for 5-6 hours.", false},
		{"range 1-2 hours", "Chill for 1-2 hours.", true},
	}
	for _, c := range cases {
		got := string(ParseAndRender(c.text))
		has := strings.Contains(got, `class="tmr"`)
		if has != c.timer {
			t.Errorf("%s: timer=%v want %v\n  in:  %s\n  out: %s", c.name, has, c.timer, c.text, got)
		}
	}
}

func TestCappedTimerKeepsItsText(t *testing.T) {
	out := string(ParseAndRender("Ferment for ~{24%hours} at room temperature."))
	for _, want := range []string{"24 hours", "room temperature"} {
		if !strings.Contains(out, want) {
			t.Errorf("lost %q from output: %s", want, out)
		}
	}
	if strings.Contains(out, "tmr") {
		t.Errorf("should not be clickable: %s", out)
	}
}
