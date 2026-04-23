package cooklang

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"tangled.org/dunkirk.sh/pare/internal/models"
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
		sb.WriteString("\n\n== Ingredients ==\n")
		for _, ing := range unmatched {
			ref := ingredientCookRef(ing)
			sb.WriteString(fmt.Sprintf("- %s\n", ref))
		}
	}

	return sb.String()
}

var timeRe = regexp.MustCompile(`(?i)\b(\d+)\s*(seconds?|minutes?|mins?|hours?|hrs?|h)\b`)
var cookwareRe = regexp.MustCompile(`(?i)\b(a|the)\s+(large|small|medium|big|heavy|deep|shallow|cast-iron|nonstick)\s+([\w-]+(?:\s+[\w-]+)?)\b`)
var bareCookwareRe = regexp.MustCompile(`(?i)\b(saucepan|skillet|frying pan|baking sheet|baking dish|roasting pan|stockpot|dutch oven|slow cooker|instant pot|air fryer|pressure cooker|food processor|stand mixer|hand mixer|blender|grill|oven|stove|microwave|pot|pan|wok|bowl|whisk|spatula|tongs|colander|strainer|sieve|rolling pin|cutting board|knife|peeler|grater|mandoline|thermometer)\b`)

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

	annotated = timeRe.ReplaceAllStringFunc(annotated, func(matchStr string) string {
		parts := timeRe.FindStringSubmatch(matchStr)
		if len(parts) >= 3 {
			qty := parts[1]
			unit := parts[2]
			unit = normalizeTimeUnit(unit)
			return fmt.Sprintf("~{%s%%%s}", qty, unit)
		}
		return matchStr
	})

	annotated = cookwareRe.ReplaceAllStringFunc(annotated, func(matchStr string) string {
		parts := cookwareRe.FindStringSubmatch(matchStr)
		if len(parts) >= 4 {
			ware := parts[2] + " " + parts[3]
			return fmt.Sprintf("#%s{}", ware)
		}
		return matchStr
	})

	annotated = bareCookwareRe.ReplaceAllStringFunc(annotated, func(matchStr string) string {
		ware := strings.ToLower(matchStr)
		if strings.Contains(ware, " ") {
			return fmt.Sprintf("#%s{}", ware)
		}
		return fmt.Sprintf("#%s", ware)
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

var ingredientPrefixRe = regexp.MustCompile(`^(?i)(\d+\s*\S*\s+)?(?:of\s+)?(.+)$`)

func extractIngredientName(raw string) string {
	raw = strings.TrimSpace(raw)
	parts := ingredientPrefixRe.FindStringSubmatch(raw)
	if len(parts) >= 3 && parts[2] != "" {
		name := parts[2]
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