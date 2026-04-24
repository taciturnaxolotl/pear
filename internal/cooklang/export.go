package cooklang

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"tangled.org/dunkirk.sh/pear/internal/models"
)

func Export(recipe *models.Recipe) string {
	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("source: %s\n", recipe.SourceURL))
	if recipe.Yield != "" {
		sb.WriteString(fmt.Sprintf("servings: %s\n", recipe.Yield))
	}
	if recipe.PrepTime != "" {
		sb.WriteString(fmt.Sprintf("prepTime: %s\n", formatDuration(recipe.PrepTime)))
	}
	if recipe.CookTime != "" {
		sb.WriteString(fmt.Sprintf("cookTime: %s\n", formatDuration(recipe.CookTime)))
	}
	sb.WriteString("---\n\n")

	ingredientIndex := buildIngredientIndex(recipe.Ingredients)
	matched := make(map[string]bool)

	if len(recipe.Instructions) > 0 {
		for i, step := range recipe.Instructions {
			annotated, stepMatched := annotateStep(step.Text, ingredientIndex)
			for k := range stepMatched {
				matched[k] = true
			}
			sb.WriteString(annotated)
			if i < len(recipe.Instructions)-1 {
				sb.WriteString("\n\n")
			}
		}
	}

	var unmatched []models.Ingredient
	for _, ing := range recipe.Ingredients {
		key := ingredientKey(ing)
		if !matched[key] {
			unmatched = append(unmatched, ing)
		}
	}

	if len(unmatched) > 0 {
		groups := groupIngredients(unmatched)
		for _, g := range groups {
			sb.WriteString("\n\n== ")
			if g.Name != "" {
				sb.WriteString(g.Name + " ")
			}
			sb.WriteString("Ingredients ==\n")
			for _, ing := range g.Items {
				ref := ingredientCookRef(ing)
				sb.WriteString(fmt.Sprintf("- %s\n", ref))
			}
		}
	}

	return sb.String()
}

type ingredientGroup struct {
	Name  string
	Items []models.Ingredient
}

func groupIngredients(ings []models.Ingredient) []ingredientGroup {
	var groups []ingredientGroup
	var current *ingredientGroup
	for _, ing := range ings {
		if current == nil || ing.Group != current.Name {
			groups = append(groups, ingredientGroup{Name: ing.Group})
			current = &groups[len(groups)-1]
		}
		current.Items = append(current.Items, ing)
	}
	return groups
}

var timeRangeExportRe = regexp.MustCompile(`(?i)\b(\d+-\d+)\s*(seconds?|minutes?|mins?|hours?|hrs?|h)\b`)
var timeRe = regexp.MustCompile(`(?i)(^|[^0-9-])(\d+)\s*(seconds?|minutes?|mins?|hours?|hrs?|h)\b`)

var frTimeRangeExportRe = regexp.MustCompile(`(?i)\b(\d+-\d+)\s*(secondes?|minutes?|mins?|heures?|h)\b`)
var frTimeRe = regexp.MustCompile(`(?i)(^|[^0-9-])(\d+)\s*(secondes?|minutes?|mins?|heures?|h)\b`)

func AnnotateTimersOnly(text string, lang string) string {
	rangeRe, timeReLang := timeRangeExportRe, timeRe
	if strings.HasPrefix(lang, "fr") {
		rangeRe, timeReLang = frTimeRangeExportRe, frTimeRe
	}

	annotated := rangeRe.ReplaceAllStringFunc(text, func(matchStr string) string {
		parts := rangeRe.FindStringSubmatch(matchStr)
		if len(parts) >= 3 {
			qty := parts[1]
			unit := parts[2]
			unit = normalizeTimeUnit(unit)
			return fmt.Sprintf("~{%s%%%s}", qty, unit)
		}
		return matchStr
	})

	annotated = timeReLang.ReplaceAllStringFunc(annotated, func(matchStr string) string {
		parts := timeReLang.FindStringSubmatch(matchStr)
		if len(parts) >= 4 {
			leading := parts[1]
			qty := parts[2]
			unit := parts[3]
			unit = normalizeTimeUnit(unit)
			return leading + fmt.Sprintf("~{%s%%%s}", qty, unit)
		}
		return matchStr
	})

	return annotated
}

func AnnotateStepForDisplay(text string, ingredients []models.Ingredient) string {
	index := buildIngredientIndex(ingredients)
	annotated, _ := annotateStep(text, index)
	return annotated
}

func annotateStep(text string, ingredients map[string]models.Ingredient) (string, map[string]bool) {
	annotated := text
	matched := make(map[string]bool)

	type match struct {
		start int
		end   int
		repl  string
		key   string
	}

	var matches []match

	for key, ing := range ingredients {
		searchNames := searchNamesFor(key)
		for _, searchName := range searchNames {
			lowerText := strings.ToLower(annotated)
			lowerSearch := strings.ToLower(searchName)

			idx := 0
			for {
				pos := strings.Index(lowerText[idx:], lowerSearch)
				if pos < 0 {
					break
				}
				pos += idx

				if isWordBoundary(annotated, pos, pos+len(searchName)) {
					cookRef := ingredientCookRefInStep(ing, key)
					matches = append(matches, match{start: pos, end: pos + len(searchName), repl: cookRef, key: key})
					matched[key] = true
					break
				}
				idx = pos + 1
			}
			if matched[key] {
				break
			}
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].start > matches[j].start
	})

	for _, m := range matches {
		annotated = annotated[:m.start] + m.repl + annotated[m.end:]
	}

	annotated = timeRangeExportRe.ReplaceAllStringFunc(annotated, func(matchStr string) string {
		parts := timeRangeExportRe.FindStringSubmatch(matchStr)
		if len(parts) >= 3 {
			qty := parts[1]
			unit := parts[2]
			unit = normalizeTimeUnit(unit)
			return fmt.Sprintf("~{%s%%%s}", qty, unit)
		}
		return matchStr
	})

	annotated = timeRe.ReplaceAllStringFunc(annotated, func(matchStr string) string {
		parts := timeRe.FindStringSubmatch(matchStr)
		if len(parts) >= 4 {
			leading := parts[1]
			qty := parts[2]
			unit := parts[3]
			unit = normalizeTimeUnit(unit)
			return leading + fmt.Sprintf("~{%s%%%s}", qty, unit)
		}
		return matchStr
	})

	return annotated, matched
}

func normalizeTimeUnit(unit string) string {
	unit = strings.ToLower(unit)
	switch unit {
	case "sec", "secs":
		return "second"
	case "min", "mins":
		return "minute"
	case "hr", "hrs", "h":
		return "hour"
	case "seconde", "secondes":
		return "second"
	case "heure", "heures":
		return "hour"
	default:
		return unit
	}
}

func isWordBoundary(s string, start, end int) bool {
	if start > 0 {
		c := s[start-1]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return false
		}
	}
	if end < len(s) {
		c := s[end]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

func searchNamesFor(key string) []string {
	names := []string{key}

	stripPrefixes := []string{"ground ", "dried ", "fresh ", "frozen ", "canned ", "cooked ", "raw ", "minced ", "chopped ", "crushed ", "grated ", "sliced ", "diced ", "powdered ", "granulated "}
	lower := strings.ToLower(key)
	for _, prefix := range stripPrefixes {
		if strings.HasPrefix(lower, prefix) {
			short := key[len(prefix):]
			names = append(names, short)
		}
	}

	return names
}

func ingredientKey(ing models.Ingredient) string {
	if ing.Name != "" {
		return ing.Name
	}
	return extractIngredientName(ing.RawText)
}

func ingredientCookRef(ing models.Ingredient) string {
	name := ingredientKey(ing)
	needsBraces := strings.Contains(name, " ")

	if ing.Quantity != "" && ing.Unit != "" {
		if needsBraces {
			return fmt.Sprintf("@%s{%s%%%s}", name, ing.Quantity, ing.Unit)
		}
		return fmt.Sprintf("@%s{%s%%%s}", name, ing.Quantity, ing.Unit)
	}
	if ing.Quantity != "" {
		if needsBraces {
			return fmt.Sprintf("@%s{%s}", name, ing.Quantity)
		}
		return fmt.Sprintf("@%s{%s}", name, ing.Quantity)
	}
	if needsBraces {
		return fmt.Sprintf("@%s{}", name)
	}
	return fmt.Sprintf("@%s", name)
}

func ingredientCookRefInStep(ing models.Ingredient, key string) string {
	needsBraces := strings.Contains(key, " ")

	if ing.Quantity != "" && ing.Unit != "" {
		if needsBraces {
			return fmt.Sprintf("@%s{%s%%%s}", key, ing.Quantity, ing.Unit)
		}
		return fmt.Sprintf("@%s{%s%%%s}", key, ing.Quantity, ing.Unit)
	}
	if ing.Quantity != "" {
		if needsBraces {
			return fmt.Sprintf("@%s{%s}", key, ing.Quantity)
		}
		return fmt.Sprintf("@%s{%s}", key, ing.Quantity)
	}
	if needsBraces {
		return fmt.Sprintf("@%s{}", key)
	}
	return fmt.Sprintf("@%s", key)
}

func buildIngredientIndex(ingredients []models.Ingredient) map[string]models.Ingredient {
	index := make(map[string]models.Ingredient)
	for _, ing := range ingredients {
		key := ingredientKey(ing)
		if key != "" {
			index[key] = ing
		}
	}
	return index
}

var ingredientPrefixRe = regexp.MustCompile(`^(?i)(\d+\s*\d*/?\d*\s+)?(?:of\s+)?(.+)$`)

func extractIngredientName(raw string) string {
	raw = strings.TrimSpace(raw)
	m := ingredientPrefixRe.FindStringSubmatch(raw)
	if len(m) >= 3 && m[2] != "" {
		name := m[2]
		name = strings.TrimPrefix(name, "of ")
		name = strings.TrimSpace(name)
		name = strings.TrimSuffix(name, ",")
		name = strings.TrimSpace(name)
		return name
	}
	return raw
}

func formatDuration(iso string) string {
	if strings.HasPrefix(iso, "PT") {
		d := strings.TrimPrefix(iso, "PT")
		d = strings.Replace(d, "H", " hours", 1)
		d = strings.Replace(d, "M", " minutes", 1)
		d = strings.Replace(d, "S", " seconds", 1)
		return strings.TrimSpace(d)
	}
	return iso
}