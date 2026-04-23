package schema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"tangled.org/dunkirk.sh/pare/internal/models"

	"golang.org/x/net/html"
)

func Extract(body string) (*models.Recipe, bool) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, false
	}

	ldNodes := findJSONLDScripts(doc)
	for _, node := range ldNodes {
		recipe := parseJSONLD(node)
		if recipe != nil {
			recipe.ExtractionMethod = "schema.org"
			if recipe.Description == "" {
				recipe.Description = findMetaDescription(doc)
			}
			return recipe, true
		}
	}

	return nil, false
}

func findMetaDescription(n *html.Node) string {
	var f func(*html.Node) string
	f = func(n *html.Node) string {
		if n.Type == html.ElementNode && n.Data == "meta" {
			name := ""
			prop := ""
			content := ""
			for _, a := range n.Attr {
				if a.Key == "name" {
					name = a.Val
				}
				if a.Key == "property" {
					prop = a.Val
				}
				if a.Key == "content" {
					content = a.Val
				}
			}
			if name == "description" || prop == "og:description" {
				return content
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if desc := f(c); desc != "" {
				return desc
			}
		}
		return ""
	}
	return f(n)
}

func findJSONLDScripts(n *html.Node) []string {
	var results []string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" {
			for _, attr := range n.Attr {
				if attr.Key == "type" && attr.Val == "application/ld+json" {
					text := collectText(n)
					results = append(results, text)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return results
}

func collectText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(collectText(c))
	}
	return sb.String()
}

func parseJSONLD(content string) *models.Recipe {
	var raw interface{}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil
	}

	obj := findRecipeObject(raw)
	if obj == nil {
		return nil
	}

	m, ok := obj.(map[string]interface{})
	if !ok {
		return nil
	}

	recipe := &models.Recipe{}
	recipe.Name = strVal(m, "name")
	recipe.Description = strVal(m, "description")
	recipe.PrepTime = strVal(m, "prepTime")
	recipe.CookTime = strVal(m, "cookTime")
	recipe.TotalTime = strVal(m, "totalTime")
	recipe.Yield = cleanYield(strVal(m, "recipeYield"))

	if img := extractImage(m); img != "" {
		recipe.ImageURL = img
	}

	recipe.Ingredients = extractIngredients(m)
	recipe.Instructions = extractInstructions(m)

	if recipe.Yield != "" {
		fmt.Sscanf(recipe.Yield, "%d", &recipe.Servings)
	}

	if recipe.Name == "" {
		return nil
	}

	return recipe
}

func findRecipeObject(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		if typ, ok := val["@type"]; ok {
			typeStr := fmt.Sprintf("%v", typ)
			if typeStr == "Recipe" {
				return val
			}
		}
		if graph, ok := val["@graph"]; ok {
			if arr, ok := graph.([]interface{}); ok {
				for _, item := range arr {
					if r := findRecipeObject(item); r != nil {
						return r
					}
				}
			}
		}
	case []interface{}:
		for _, item := range val {
			if r := findRecipeObject(item); r != nil {
				return r
			}
		}
	}
	return nil
}

func extractImage(m map[string]interface{}) string {
	img := m["image"]
	switch v := img.(type) {
	case string:
		return v
	case map[string]interface{}:
		return strVal(v, "url")
	case []interface{}:
		if len(v) > 0 {
			switch first := v[0].(type) {
			case string:
				return first
			case map[string]interface{}:
				return strVal(first, "url")
			}
		}
	}
	return ""
}

func extractIngredients(m map[string]interface{}) []models.Ingredient {
	raw, ok := m["recipeIngredient"]
	if !ok {
		raw, ok = m["ingredients"]
	}
	if !ok {
		return nil
	}

	var items []interface{}
	switch v := raw.(type) {
	case []interface{}:
		items = v
	case string:
		items = []interface{}{v}
	default:
		return nil
	}

	var ingredients []models.Ingredient
	for _, item := range items {
		text := fmt.Sprintf("%v", item)
		ingredients = append(ingredients, parseIngredient(text))
	}
	return ingredients
}

var ingredientRe = regexp.MustCompile(`^(\d+\s*\d*/\d*|\d+\.?\d*)\s+(cups?|tablespoons?|teaspoons?|tbsp|tsp|c|oz|lbs?|pounds?|grams?|g|kg|ml|liters?|l|pinch|dash|cloves?|slices?|pieces?|heads?|sprigs?|bunches?|cans?|bottles?|packages?|sticks?|quarts?|pints?|gallons?)\s+(.+)$`)

var ingredientFracRe = regexp.MustCompile(`^(\d+\s+\d/\d+)\s+(cups?|tablespoons?|teaspoons?|tbsp|tsp|c|oz|lbs?|pounds?|grams?|g|kg|ml|liters?|l|pinch|dash|cloves?|slices?|pieces?|heads?|sprigs?|bunches?|cans?|bottles?|packages?|sticks?|quarts?|pints?|gallons?)\s+(.+)$`)

func parseIngredient(text string) models.Ingredient {
	text = strings.TrimSpace(text)

	if m := ingredientFracRe.FindStringSubmatch(text); len(m) == 4 {
		return models.Ingredient{RawText: text, Quantity: m[1], Unit: m[2], Name: m[3]}
	}
	if m := ingredientRe.FindStringSubmatch(text); len(m) == 4 {
		return models.Ingredient{RawText: text, Quantity: m[1], Unit: m[2], Name: m[3]}
	}
	return models.Ingredient{RawText: text}
}

func extractInstructions(m map[string]interface{}) []models.Instruction {
	raw, ok := m["recipeInstructions"]
	if !ok {
		return nil
	}

	var steps []models.Instruction
	walkInstructions(raw, &steps)
	return steps
}

func walkInstructions(v interface{}, steps *[]models.Instruction) {
	switch val := v.(type) {
	case []interface{}:
		for _, item := range val {
			walkInstructions(item, steps)
		}
	case map[string]interface{}:
		typ := fmt.Sprintf("%v", val["@type"])
		switch typ {
		case "HowToStep":
			text := strVal(val, "text")
			if text != "" {
				*steps = append(*steps, models.Instruction{Text: text})
			}
		case "HowToSection":
			if items, ok := val["itemListElement"].([]interface{}); ok {
				for _, item := range items {
					walkInstructions(item, steps)
				}
			}
		default:
			text := strVal(val, "text")
			if text != "" {
				*steps = append(*steps, models.Instruction{Text: text})
			}
		}
	case string:
		lines := strings.Split(val, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				*steps = append(*steps, models.Instruction{Text: line})
			}
		}
	}
}

func cleanYield(yield string) string {
	yield = strings.TrimSpace(yield)
	yield = strings.TrimSuffix(yield, " servings")
	yield = strings.TrimSuffix(yield, " serving")
	return yield
}

func strVal(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []interface{}:
		if len(val) > 0 {
			return fmt.Sprintf("%v", val[0])
		}
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}