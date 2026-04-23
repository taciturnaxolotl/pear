package cooklang

import (
	"regexp"
	"strconv"
	"strings"
)

type AnnotatedStep struct {
	Text string
	Items []AnnotatedItem
}

type AnnotatedItem struct {
	Type     string // "ingredient", "timer", "cookware"
	Name     string
	Quantity string
	Unit     string
}

var parseIngredientRe = regexp.MustCompile(`@([\w\s]+?)\{([^}%]*)(?:%([^}]*))?\}`)
var parseTimerRe = regexp.MustCompile(`~([\w\s]*?)\{([^}%]*)(?:%([^}]*))?\}`)

func ParseSteps(instructions []string) []AnnotatedStep {
	var steps []AnnotatedStep
	for _, text := range instructions {
		step := AnnotatedStep{Text: text}
		step.Items = parseAnnotations(text)
		steps = append(steps, step)
	}
	return steps
}

func parseAnnotations(text string) []AnnotatedItem {
	var items []AnnotatedItem

	matches := parseIngredientRe.FindAllStringSubmatchIndex(text, -1)
	for _, m := range matches {
		name := text[m[2]:m[3]]
		qty := ""
		unit := ""
		if m[4] != -1 {
			qty = text[m[4]:m[5]]
		}
		if m[6] != -1 {
			unit = text[m[6]:m[7]]
		}
		items = append(items, AnnotatedItem{Type: "ingredient", Name: name, Quantity: qty, Unit: unit})
	}

	matches = parseTimerRe.FindAllStringSubmatchIndex(text, -1)
	for _, m := range matches {
		name := ""
		if m[2] != -1 {
			name = text[m[2]:m[3]]
		}
		qty := ""
		unit := ""
		if m[4] != -1 {
			qty = text[m[4]:m[5]]
		}
		if m[6] != -1 {
			unit = text[m[6]:m[7]]
		}
		items = append(items, AnnotatedItem{Type: "timer", Name: name, Quantity: qty, Unit: unit})
	}

	return items
}

func RenderStepHTML(text string) string {
	out := text

	out = parseIngredientRe.ReplaceAllStringFunc(out, func(match string) string {
		parts := parseIngredientRe.FindStringSubmatch(match)
		name := strings.TrimSpace(parts[1])
		qty := parts[2]
		unit := ""
		if len(parts) >= 4 && parts[3] != "" {
			unit = parts[3]
		}
		display := name
		if qty != "" {
			display = qty
			if unit != "" {
				display = qty + " " + unit + " " + name
			}
		}
		return `<span class="ing">` + escHTML(display) + `</span>`
	})

	out = parseTimerRe.ReplaceAllStringFunc(out, func(match string) string {
		parts := parseTimerRe.FindStringSubmatch(match)
		qty := parts[2]
		unit := ""
		if len(parts) >= 4 && parts[3] != "" {
			unit = parts[3]
		}
		display := qty
		if unit != "" {
			display = qty + " " + unit
		}
		secs := timerToSeconds(qty, unit)
		return `<span class="tmr" data-seconds="` + strconv.Itoa(secs) + `">` + escHTML(display) + `</span>`
	})

	return out
}

func escHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func timerToSeconds(qty, unit string) int {
	v, err := strconv.Atoi(qty)
	if err != nil {
		return 0
	}
	switch strings.ToLower(unit) {
	case "second", "seconds":
		return v
	case "minute", "minutes", "min", "mins":
		return v * 60
	case "hour", "hours", "hr", "hrs", "h":
		return v * 3600
	default:
		return v * 60
	}
}