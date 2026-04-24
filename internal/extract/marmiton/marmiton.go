package marmiton

import (
	"fmt"
	"strings"

	"tangled.org/dunkirk.sh/pear/internal/models"

	"golang.org/x/net/html"
)

func Extract(body string) (*models.Recipe, bool) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, false
	}

	container := findByClass(doc, "recipeV2-container")
	if container == nil {
		return nil, false
	}

	recipe := &models.Recipe{ExtractionMethod: "marmiton"}

	if h1 := findFirstByTag(container, "h1"); h1 != "" {
		recipe.Name = h1
	}

	if img := findRecipeImage(container); img != "" {
		recipe.ImageURL = img
	}

	if yield, unit := findServings(container); yield != "" {
		if unit != "" {
			recipe.Yield = yield + " " + unit
		} else {
			recipe.Yield = yield
		}
		fmt.Sscanf(yield, "%d", &recipe.Servings)
	}

	recipe.PrepTime = findPrepTime(container)
	recipe.CookTime = findCookTime(container)

	recipe.Ingredients = extractIngredients(container)
	recipe.Instructions = extractInstructions(container)

	if recipe.Name == "" {
		return nil, false
	}

	return recipe, true
}

func extractIngredients(n *html.Node) []models.Ingredient {
	var ingredients []models.Ingredient
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && hasClass(n, "card-ingredient") {
			ing := parseCardIngredient(n)
			if ing.Name != "" {
				ingredients = append(ingredients, ing)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return ingredients
}

func parseCardIngredient(n *html.Node) models.Ingredient {
	var quantity, unit, name, complement string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, attr := range n.Attr {
				if attr.Key == "data-ingredientquantity" && quantity == "" {
					quantity = attr.Val
				}
				if attr.Key == "data-unitsingular" && unit == "" {
					unit = attr.Val
				}
				if attr.Key == "data-ingredientnamesingular" && name == "" {
					name = attr.Val
				}
				if attr.Key == "data-ingredientcomplementsingular" && complement == "" {
					complement = attr.Val
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)

	rawText := quantity
	if unit != "" {
		rawText += " " + unit
	}
	if name != "" {
		rawText += " " + name
	}
	if complement != "" {
		rawText += " " + complement
	}

	return models.Ingredient{
		RawText: rawText,
		Quantity: quantity,
		Unit:     unit,
		Name:     buildIngredientName(name, complement),
	}
}

func buildIngredientName(name, complement string) string {
	if complement != "" {
		return name + " " + complement
	}
	return name
}

func extractInstructions(n *html.Node) []models.Instruction {
	var steps []models.Instruction
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && hasClass(n, "recipe-step-list__container") {
			p := findFirst(n, "p")
			if p != nil {
				text := textContent(p)
				if text != "" {
					steps = append(steps, models.Instruction{Text: text})
				}
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return steps
}

func findServings(n *html.Node) (string, string) {
	counter := findByClass(n, "mrtn-recette_ingredients-counter")
	if counter == nil {
		return "", ""
	}
	var nb, unit string
	for _, attr := range counter.Attr {
		if attr.Key == "data-servingsnb" {
			nb = attr.Val
		}
		if attr.Key == "data-servingsunit" {
			unit = attr.Val
		}
	}
	return nb, unit
}

func findPrepTime(n *html.Node) string {
	_, _, prep, cook := findTimes(n)
	_ = cook
	return prep
}

func findCookTime(n *html.Node) string {
	_, _, _, cook := findTimes(n)
	return cook
}

func findTimes(n *html.Node) (total, rest, prep, cook string) {
	container := findByClass(n, "recipe-preparation__time")
	if container == nil {
		return
	}
	if t := findByClass(container, "time__total"); t != nil {
		total = textContent(t)
	}
	details := findByClass(container, "time__details")
	if details == nil {
		return
	}
	for c := details.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Data != "div" {
			continue
		}
		children := getChildElements(c)
		if len(children) == 2 {
			label := strings.ToLower(textContent(children[0]))
			value := textContent(children[1])
			if strings.Contains(label, "préparation") || strings.Contains(label, "prep") {
				prep = value
			} else if strings.Contains(label, "cuisson") || strings.Contains(label, "cook") {
				cook = value
			} else if strings.Contains(label, "repos") || strings.Contains(label, "rest") {
				rest = value
			}
		}
	}
	return
}

func getChildElements(n *html.Node) []*html.Node {
	var children []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			children = append(children, c)
		}
	}
	return children
}

func findRecipeImage(n *html.Node) string {
	viewer := findByClass(n, "recipe-media-viewer")
	if viewer == nil {
		return ""
	}
	var best string
	bestScore := 0
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			src := getAttr(n, "data-src")
			if src == "" {
				src = getAttr(n, "src")
			}
			if src == "" || strings.Contains(src, "lazyload") || strings.Contains(src, "w40h40") || strings.Contains(src, "w79h79") || strings.Contains(src, "w157h157") || strings.Contains(src, "w75h75") || strings.Contains(src, "w150h150") {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					f(c)
				}
				return
			}
			score := 1
			if strings.Contains(src, "_origin") {
				score = 100
			} else if strings.Contains(src, "_w1024") {
				score = 90
			} else if strings.Contains(src, "_w648") {
				score = 80
			} else if strings.Contains(src, "_w324") {
				score = 70
			} else if strings.Contains(src, "_w300") {
				score = 60
			}
			if score > bestScore {
				bestScore = score
				best = src
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(viewer)
	return best
}

func findByClass(n *html.Node, class string) *html.Node {
	if hasClass(n, class) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findByClass(c, class); found != nil {
			return found
		}
	}
	return nil
}

func hasClass(n *html.Node, class string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			for _, c := range strings.Fields(attr.Val) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

func findTextByClass(n *html.Node, class string) string {
	found := findByClass(n, class)
	if found == nil {
		return ""
	}
	return textContent(found)
}

func findFirstByTag(n *html.Node, tag string) string {
	found := findFirst(n, tag)
	if found == nil {
		return ""
	}
	return textContent(found)
}

func findFirst(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirst(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func textContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(textContent(c))
	}
	return strings.TrimSpace(sb.String())
}

func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}
