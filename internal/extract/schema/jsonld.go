package schema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
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
			if ogImg := findMetaImage(doc); ogImg != "" {
				recipe.ImageURL = ogImg
			}
			if recipe.ImageURL == "" || looksSmall(recipe.ImageURL) {
				if largeImg := findLargestImage(doc); largeImg != "" {
					recipe.ImageURL = largeImg
				}
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

func isRecipeType(typ interface{}) bool {
	switch v := typ.(type) {
	case string:
		return v == "Recipe"
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s == "Recipe" {
				return true
			}
		}
	}
	return false
}

func findRecipeObject(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		if typ, ok := val["@type"]; ok {
			if isRecipeType(typ) {
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
	var urls []string
	collectImageURLs(m["image"], &urls)
	if best := pickBestImage(urls); best != "" {
		return best
	}
	return ""
}

func collectImageURLs(img interface{}, urls *[]string) {
	switch v := img.(type) {
	case string:
		*urls = append(*urls, v)
	case map[string]interface{}:
		if u := strVal(v, "url"); u != "" {
			*urls = append(*urls, u)
		}
		if u := strVal(v, "contentUrl"); u != "" {
			*urls = append(*urls, u)
		}
	case []interface{}:
		for _, item := range v {
			collectImageURLs(item, urls)
		}
	}
}

func pickBestImage(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	// Prefer URLs that don't look like thumbnails
	for _, u := range urls {
		if !looksSmall(u) {
			return u
		}
	}
	return urls[0]
}

var smallImageRe = regexp.MustCompile(`[-_]sm\b|[-_]thumb(?:nail)?\b|[-_]small\b|[-_]\d{2,3}x\d{2,3}\b|[-_]\d{2,3}w\b`)

func looksSmall(u string) bool {
	return smallImageRe.MatchString(u)
}

func findMetaImage(n *html.Node) string {
	var result string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			prop := ""
			content := ""
			for _, a := range n.Attr {
				if a.Key == "property" {
					prop = a.Val
				}
				if a.Key == "content" {
					content = a.Val
				}
			}
			if prop == "og:image" && content != "" {
				result = content
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
			if result != "" {
				return
			}
		}
	}
	f(n)
	return result
}

type imgCandidate struct {
	src    string
	width  int
	height int
}

func findLargestImage(n *html.Node) string {
	var candidates []imgCandidate
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			var src, dataSrc string
			var w, h int
			for _, a := range n.Attr {
				switch a.Key {
				case "src":
					src = a.Val
				case "data-lazy-src", "data-src":
					dataSrc = a.Val
				case "width":
					w, _ = strconv.Atoi(a.Val)
				case "height":
					h, _ = strconv.Atoi(a.Val)
				}
			}
			u := dataSrc
			if u == "" {
				u = src
			}
			if u == "" || strings.HasPrefix(u, "data:") {
				return
			}
			candidates = append(candidates, imgCandidate{src: u, width: w, height: h})
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	if len(candidates) == 0 {
		return ""
	}
	// Pick the largest by area (width * height), preferring ones with explicit dimensions
	best := candidates[0]
	bestArea := best.width * best.height
	for _, c := range candidates[1:] {
		area := c.width * c.height
		if area > bestArea {
			best = c
			bestArea = area
		}
	}
	// If no candidate had dimensions, pick the first non-small URL
	if bestArea == 0 {
		for _, c := range candidates {
			if !looksSmall(c.src) {
				return c.src
			}
		}
		return candidates[0].src
	}
	return best.src
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

var unitList = `cups?|tablespoons?|teaspoons?|tbsp|tsp|c|oz|lbs?|pounds?|grams?|g|kg|ml|liters?|l|pinch(?:es)?|dash(?:es)?|cloves?|slices?|pieces?|heads?|sprigs?|bunch(?:es)?|cans?|bottles?|packages?|sticks?|quarts?|pints?|gallons?`

var numPat = `(\d+\s+\d/\d+|\d+/\d+|\d+\.?\d*)`

var ingredientRangeRe = regexp.MustCompile(`^(\d+[/\d]*\s*[-–]\s*\d+[/\.\d]*)\s+(` + unitList + `)\s+(.+)$`)
var ingredientFracRe = regexp.MustCompile(`^` + numPat + `\s+(` + unitList + `)\s+(.+)$`)
var ingredientFracAdjUnitRe = regexp.MustCompile(`^` + numPat + `\s+((?:\w+\s+){1,2})(` + unitList + `)\s+(.+)$`)
var ingredientNoUnitRe = regexp.MustCompile(`^` + numPat + `\s+(.+)$`)

func ParseIngredient(text string) models.Ingredient {
	return parseIngredient(text)
}

func parseIngredient(text string) models.Ingredient {
	text = strings.TrimSpace(text)

	if m := ingredientRangeRe.FindStringSubmatch(text); len(m) == 4 {
		return models.Ingredient{RawText: text, Quantity: m[1], Unit: m[2], Name: m[3]}
	}
	if m := ingredientFracAdjUnitRe.FindStringSubmatch(text); len(m) == 5 {
		name := strings.TrimSpace(m[2]) + " " + m[4]
		return models.Ingredient{RawText: text, Quantity: m[1], Unit: m[3], Name: name}
	}
	if m := ingredientFracRe.FindStringSubmatch(text); len(m) == 4 {
		return models.Ingredient{RawText: text, Quantity: m[1], Unit: m[2], Name: m[3]}
	}
	if m := ingredientNoUnitRe.FindStringSubmatch(text); len(m) == 3 {
		return models.Ingredient{RawText: text, Quantity: m[1], Name: m[2]}
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