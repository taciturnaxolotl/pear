package models

import "testing"

func TestTidyTextCollapsesDoubledParens(t *testing.T) {
	cases := []struct{ in, want string }{
		// the real defect: a plugin bracketing an already-bracketed note
		{"7 tbsp white vinegar ((or lemon juice))", "7 tbsp white vinegar (or lemon juice)"},
		{"2 eggs ((room temperature))", "2 eggs (room temperature)"},
		{"salt ((to taste)) and pepper ((optional))", "salt (to taste) and pepper (optional)"},

		// genuine nesting and ordinary brackets must survive untouched
		{"2 cups (packed) flour", "2 cups (packed) flour"},
		{"1 cup sugar (see note (below))", "1 cup sugar (see note (below))"},
		{"chicken (about 2 lb)", "chicken (about 2 lb)"},
		{"no brackets here", "no brackets here"},
		{"unbalanced ((oops)", "unbalanced ((oops)"},

		// escaped entities are resolved before the brackets are inspected
		{"cream &amp; sugar ((optional))", "cream & sugar (optional)"},
	}
	for _, c := range cases {
		if got := tidyText(c.in); got != c.want {
			t.Errorf("tidyText(%q)\n  got  %q\n  want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeReachesEveryField(t *testing.T) {
	r := &Recipe{
		Name:         "Mozzarella ((easy))",
		Description:  "Ready in 30 min ((really))",
		Ingredients:  []Ingredient{{RawText: "7 tbsp vinegar ((or lemon juice))", Name: "vinegar ((or lemon juice))", Group: "Dairy ((cold))"}},
		Instructions: []Instruction{{Text: "Heat milk ((to 115F))"}},
	}
	r.Normalize()
	for label, got := range map[string]string{
		"name":        r.Name,
		"description": r.Description,
		"raw text":    r.Ingredients[0].RawText,
		"ing name":    r.Ingredients[0].Name,
		"group":       r.Ingredients[0].Group,
		"instruction": r.Instructions[0].Text,
	} {
		if containsDoubled(got) {
			t.Errorf("%s still doubled: %q", label, got)
		}
	}
}

func containsDoubled(s string) bool { return doubledParens.MatchString(s) }
