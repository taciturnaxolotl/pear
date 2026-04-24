package cooklang

import (
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"strconv"
	"strings"

	cooklang "github.com/aquilax/cooklang-go"
)

func Highlight(raw string) template.HTML {
	if strings.TrimSpace(raw) == "" {
		return template.HTML("")
	}

	var buf strings.Builder
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		buf.WriteString(highlightLine(line))
		if i < len(lines)-1 {
			buf.WriteByte('\n')
		}
	}
	return template.HTML(buf.String())
}

var metaDelimRe = regexp.MustCompile(`^---\s*$`)
var metaKVRe = regexp.MustCompile(`^(\w+):\s*(.*)$`)
var sectionRe = regexp.MustCompile(`^==\s+.+\s+==\s*$`)

func highlightLine(line string) string {
	if metaDelimRe.MatchString(line) {
		return `<span class="ck-delim">---</span>`
	}
	if m := metaKVRe.FindStringSubmatch(line); m != nil {
		if m[2] != "" {
			return `<span class="ck-key">` + escHTML(m[1]) + `:</span> <span class="ck-val">` + escHTML(m[2]) + `</span>`
		}
		return `<span class="ck-key">` + escHTML(m[1]) + `:</span>`
	}
	if sectionRe.MatchString(line) {
		return `<span class="ck-section">` + escHTML(line) + `</span>`
	}
	return highlightCookSyntax(line)
}

func highlightCookSyntax(line string) string {
	preprocessed := timerRangeSyntaxRe.ReplaceAllString(line, "$1 $2")
	recipe, err := cooklang.ParseString(preprocessed)
	if err != nil || len(recipe.Steps) == 0 {
		return regexHighlight(line)
	}

	type span struct {
		start int
		end   int
		html  string
	}

	var spans []span
	for _, step := range recipe.Steps {
		dirs := step.Directions
		for _, ing := range step.Ingredients {
			idx := strings.Index(dirs, ing.Name)
			if idx >= 0 {
				name := escHTML(ing.Name)
				if ing.Amount.QuantityRaw != "" {
					qty := escHTML(ing.Amount.QuantityRaw)
					if ing.Amount.Unit != "" {
						unit := escHTML(ing.Amount.Unit)
						spans = append(spans, span{idx, idx + len(ing.Name),
							fmt.Sprintf(`<span class="ck-ing">@%s{<span class="ck-qty">%s</span>%%<span class="ck-unit">%s</span>}</span>`, name, qty, unit)})
					} else {
						spans = append(spans, span{idx, idx + len(ing.Name),
							fmt.Sprintf(`<span class="ck-ing">@%s{<span class="ck-qty">%s</span>}</span>`, name, qty)})
					}
				} else {
					spans = append(spans, span{idx, idx + len(ing.Name),
						fmt.Sprintf(`<span class="ck-ing">@%s</span>`, name)})
				}
			}
		}
		for _, tmr := range step.Timers {
			search := formatTimerSearch(tmr.Duration, tmr.Unit)
			idx := strings.Index(dirs, search)
			if idx >= 0 {
				qty := escHTML(strconv.FormatFloat(tmr.Duration, 'f', -1, 64))
				unit := escHTML(tmr.Unit)
				if unit != "" {
					spans = append(spans, span{idx, idx + len(search),
						fmt.Sprintf(`<span class="ck-tmr">~{<span class="ck-qty">%s</span>%%<span class="ck-unit">%s</span>}</span>`, qty, unit)})
				} else {
					spans = append(spans, span{idx, idx + len(search),
						fmt.Sprintf(`<span class="ck-tmr">~{<span class="ck-qty">%s</span>}</span>`, qty)})
				}
			}
		}
	}

	if len(spans) == 0 {
		return escHTML(line)
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	filtered := []span{spans[0]}
	for _, s := range spans[1:] {
		last := &filtered[len(filtered)-1]
		if s.start >= last.end {
			filtered = append(filtered, s)
		}
	}

	dirs := recipe.Steps[0].Directions
	var buf strings.Builder
	pos := 0
	for _, s := range filtered {
		if s.start > pos {
			buf.WriteString(escHTML(dirs[pos:s.start]))
		}
		buf.WriteString(s.html)
		pos = s.end
	}
	if pos < len(dirs) {
		buf.WriteString(escHTML(dirs[pos:]))
	}
	return buf.String()
}

var hlIngredientRe = regexp.MustCompile(`@([\w\s/]+)\{([^}%]*)(?:%([^}]*))?\}|@([\w/]+)`)
var hlTimerRe = regexp.MustCompile(`~\{([^%]*)(?:%([^}]*))?\}`)

func regexHighlight(line string) string {
	out := escHTML(line)
	out = hlIngredientRe.ReplaceAllStringFunc(out, func(match string) string {
		parts := hlIngredientRe.FindStringSubmatch(match)
		name := parts[1]
		if name == "" {
			name = parts[4]
		}
		name = strings.TrimSpace(name)
		qty := parts[2]
		unit := parts[3]
		if qty != "" && unit != "" {
			return fmt.Sprintf(`<span class="ck-ing">@%s{<span class="ck-qty">%s</span>%%<span class="ck-unit">%s</span>}</span>`, escHTML(name), escHTML(qty), escHTML(unit))
		}
		if qty != "" {
			return fmt.Sprintf(`<span class="ck-ing">@%s{<span class="ck-qty">%s</span>}</span>`, escHTML(name), escHTML(qty))
		}
		return fmt.Sprintf(`<span class="ck-ing">@%s</span>`, escHTML(name))
	})
	out = hlTimerRe.ReplaceAllStringFunc(out, func(match string) string {
		parts := hlTimerRe.FindStringSubmatch(match)
		qty := parts[1]
		unit := parts[2]
		if unit != "" {
			return fmt.Sprintf(`<span class="ck-tmr">~{<span class="ck-qty">%s</span>%%<span class="ck-unit">%s</span>}</span>`, escHTML(qty), escHTML(unit))
		}
		return fmt.Sprintf(`<span class="ck-tmr">~{<span class="ck-qty">%s</span>}</span>`, escHTML(qty))
	})
	return out
}

func ParseAndRender(text string) template.HTML {
	if strings.TrimSpace(text) == "" {
		return template.HTML("")
	}

	// Pre-process: replace timer range syntax ~{N-M%unit} with plain text "N-M unit"
	// since cooklang-go doesn't support range quantities for timers
	preprocessed := timerRangeSyntaxRe.ReplaceAllString(text, "$1 $2")

	recipe, err := cooklang.ParseString(preprocessed)
	if err != nil || len(recipe.Steps) == 0 {
		return regexRender(text)
	}

	var sb strings.Builder
	for i, step := range recipe.Steps {
		renderStep(step, &sb)
		if i < len(recipe.Steps)-1 {
			sb.WriteString("\n")
		}
	}
	return template.HTML(sb.String())
}

var timerRangeSyntaxRe = regexp.MustCompile(`~\{(\d+-\d+)%(\w+)\}`)

var cooklangIngredientRe = regexp.MustCompile(`@([\w\s/]+)\{[^}]*\}|@([\w/]+)`)
var cooklangTimerRe = regexp.MustCompile(`~\{(\d+)%(\w+)\}`)

func regexRender(text string) template.HTML {
	out := text

	// Replace @Name{qty%unit} or @Name{qty} or @Name{} with <span class="ing">
	out = cooklangIngredientRe.ReplaceAllStringFunc(out, func(match string) string {
		parts := cooklangIngredientRe.FindStringSubmatch(match)
		name := parts[1]
		if name == "" {
			name = parts[2]
		}
		name = strings.TrimSpace(name)
		return fmt.Sprintf(`<span class="ing">%s</span>`, escHTML(name))
	})

	// Replace ~{qty%unit} with <span class="tmr">
	out = cooklangTimerRe.ReplaceAllStringFunc(out, func(match string) string {
		parts := cooklangTimerRe.FindStringSubmatch(match)
		qty := parts[1]
		unit := parts[2]
		display := qty + " " + unit
		qtyInt, _ := strconv.Atoi(qty)
		secs := timerSeconds(float64(qtyInt), unit)
		return fmt.Sprintf(`<span class="tmr" data-seconds="%d">%s</span>`, secs, escHTML(display))
	})

	// Replace time ranges like "2-3 minutes"
	out = timeRangeRe.ReplaceAllStringFunc(out, func(match string) string {
		parts := timeRangeRe.FindStringSubmatch(match)
		if len(parts) >= 3 {
			qty := parts[1]
			unit := parts[2]
			display := qty + " " + unit
			secs := timeRangeSeconds(qty, unit)
			return fmt.Sprintf(`<span class="tmr" data-seconds="%d">%s</span>`, secs, escHTML(display))
		}
		return match
	})

	return template.HTML(out)
}

type htmlSpan struct {
	start int
	end   int
	html  string
}

func renderStep(step cooklang.Step, sb *strings.Builder) {
	dirs := step.Directions
	if dirs == "" {
		return
	}

	var spans []htmlSpan

	for _, ing := range step.Ingredients {
		display := ing.Name
		if ing.Amount.QuantityRaw != "" {
			qty := ing.Amount.QuantityRaw
			if ing.Amount.Unit != "" {
				qty = qty + " " + ing.Amount.Unit
			}
			display = qty + " " + ing.Name
		} else if ing.Amount.IsNumeric && ing.Amount.Quantity > 0 && ing.Amount.Quantity != 1 {
			qty := strconv.FormatFloat(ing.Amount.Quantity, 'f', -1, 64)
			if ing.Amount.Unit != "" {
				qty = qty + " " + ing.Amount.Unit
			}
			display = qty + " " + ing.Name
		}
		idx := strings.Index(dirs, ing.Name)
		if idx >= 0 {
			spans = append(spans, htmlSpan{
				start: idx,
				end:   idx + len(ing.Name),
				html:  fmt.Sprintf(`<span class="ing">%s</span>`, escHTML(display)),
			})
		}
	}

	for _, tmr := range step.Timers {
		search := formatTimerSearch(tmr.Duration, tmr.Unit)
		display := search
		secs := timerSeconds(tmr.Duration, tmr.Unit)
		idx := strings.Index(dirs, search)
		if idx >= 0 {
			spans = append(spans, htmlSpan{
				start: idx,
				end:   idx + len(search),
				html:  fmt.Sprintf(`<span class="tmr" data-seconds="%d">%s</span>`, secs, escHTML(display)),
			})
		}
	}

	// Find time patterns not caught by cooklang syntax (e.g. "2-3 minutes", "4-5 seconds")
	for _, m := range timeRangeRe.FindAllStringSubmatchIndex(dirs, -1) {
		fullStart, fullEnd := m[0], m[1]
		qtyStart, qtyEnd := m[2], m[3]
		unitStart, unitEnd := m[4], m[5]
		qty := dirs[qtyStart:qtyEnd]
		unit := dirs[unitStart:unitEnd]
		display := qty + " " + unit
		secs := timeRangeSeconds(qty, unit)
		spans = append(spans, htmlSpan{
			start: fullStart,
			end:   fullEnd,
			html:  fmt.Sprintf(`<span class="tmr" data-seconds="%d">%s</span>`, secs, escHTML(display)),
		})
	}

	if len(spans) == 0 {
		sb.WriteString(escHTML(dirs))
		return
	}

	sort.Slice(spans, func(i, j int) bool {
		return spans[i].start < spans[j].start
	})

	filtered := []htmlSpan{spans[0]}
	for _, s := range spans[1:] {
		last := &filtered[len(filtered)-1]
		if s.start >= last.end {
			filtered = append(filtered, s)
		}
	}

	pos := 0
	for _, s := range filtered {
		if s.start > pos {
			sb.WriteString(escHTML(dirs[pos:s.start]))
		}
		sb.WriteString(s.html)
		pos = s.end
	}
	if pos < len(dirs) {
		sb.WriteString(escHTML(dirs[pos:]))
	}
}

func formatTimerSearch(duration float64, unit string) string {
	if duration == float64(int(duration)) {
		d := int(duration)
		s := strconv.Itoa(d)
		if unit != "" {
			return s + " " + unit
		}
		return s
	}
	s := strconv.FormatFloat(duration, 'f', -1, 64)
	if unit != "" {
		return s + " " + unit
	}
	return s
}

func timerSeconds(duration float64, unit string) int {
	d := int(duration)
	switch strings.ToLower(unit) {
	case "second", "seconds", "sec", "secs":
		return d
	case "minute", "minutes", "min", "mins":
		return d * 60
	case "hour", "hours", "hr", "hrs", "h":
		return d * 3600
	default:
		if d == 0 {
			return 0
		}
		return d * 60
	}
}

func escHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func RenderStepHTML(text string) string {
	return string(ParseAndRender(text))
}

var timeRangeRe = regexp.MustCompile(`(?i)\b(\d+-\d+)\s+(seconds?|minutes?|mins?|hours?|hrs?|h)\b`)

func timeRangeSeconds(qty, unit string) int {
	parts := strings.SplitN(qty, "-", 2)
	if len(parts) != 2 {
		return 0
	}
	hi, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	switch strings.ToLower(unit) {
	case "second", "seconds", "sec", "secs":
		return hi
	case "minute", "minutes", "min", "mins":
		return hi * 60
	case "hour", "hours", "hr", "hrs", "h":
		return hi * 3600
	default:
		return hi * 60
	}
}
