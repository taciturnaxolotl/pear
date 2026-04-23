package hrecipe

import (
	"strings"

	"tangled.org/dunkirk.sh/pare/internal/models"

	"golang.org/x/net/html"
)

func Extract(body string) (*models.Recipe, bool) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, false
	}

	recipeNode := findByClass(doc, "h-recipe")
	if recipeNode == nil {
		return nil, false
	}

	recipe := &models.Recipe{}
	recipe.ExtractionMethod = "h-recipe"

	if name := findTextByClass(recipeNode, "p-name"); name != "" {
		recipe.Name = name
	}
	if summary := findTextByClass(recipeNode, "p-summary"); summary != "" {
		recipe.Description = summary
	} else if desc := findDescriptionFallback(recipeNode); desc != "" {
		recipe.Description = desc
	}
	if yield := findTextByClass(recipeNode, "p-yield"); yield != "" {
		recipe.Yield = yield
	}
	if duration := findTextByClass(recipeNode, "dt-duration"); duration != "" {
		recipe.TotalTime = duration
	}
	if photo := findAttrByClass(recipeNode, "u-photo", "src"); photo != "" {
		recipe.ImageURL = photo
	}

	ingredients := findAllTextByClass(recipeNode, "p-ingredient")
	for _, ing := range ingredients {
		recipe.Ingredients = append(recipe.Ingredients, models.Ingredient{RawText: ing})
	}

	instructions := findInnerHTMLByClass(recipeNode, "e-instructions")
	if instructions != "" {
		for _, line := range strings.Split(instructions, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				recipe.Instructions = append(recipe.Instructions, models.Instruction{Text: line})
			}
		}
	}

	if recipe.Name == "" {
		return nil, false
	}

	return recipe, true
}

func findDescriptionFallback(n *html.Node) string {
	var f func(*html.Node) string
	f = func(n *html.Node) string {
		if n.Type == html.ElementNode {
			if n.Data == "aside" || n.Data == "p" {
				if !hasClass(n, "p-ingredient") && !hasClass(n, "e-instructions") && !hasClass(n, "p-name") && !hasClass(n, "p-yield") && !hasClass(n, "dt-duration") {
					text := textContent(n)
					if len(text) > 20 {
						return text
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if result := f(c); result != "" {
				return result
			}
		}
		return ""
	}
	return f(n)
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

func findAllTextByClass(n *html.Node, class string) []string {
	var results []string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if hasClass(n, class) {
			results = append(results, textContent(n))
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return results
}

func findAttrByClass(n *html.Node, class, attr string) string {
	found := findByClass(n, class)
	if found == nil {
		return ""
	}
	for _, a := range found.Attr {
		if a.Key == attr {
			return a.Val
		}
	}
	return ""
}

func findInnerHTMLByClass(n *html.Node, class string) string {
	found := findByClass(n, class)
	if found == nil {
		return ""
	}
	return innerText(found)
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

func innerText(n *html.Node) string {
	var sb strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		} else if n.Type == html.ElementNode && n.Data == "br" {
			sb.WriteString("\n")
		} else if n.Type == html.ElementNode && (n.Data == "li" || n.Data == "p") {
			sb.WriteString("\n")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				f(c)
			}
		} else {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				f(c)
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		f(c)
	}
	return strings.TrimSpace(sb.String())
}