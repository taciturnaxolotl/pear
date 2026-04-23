package schema

import (
	"fmt"
	"strings"

	"tangled.org/dunkirk.sh/pare/internal/models"

	"golang.org/x/net/html"
)

func ExtractMicrodata(body string) (*models.Recipe, bool) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, false
	}

	recipeNode := findMicrodataRecipe(doc)
	if recipeNode == nil {
		return nil, false
	}

	recipe := &models.Recipe{ExtractionMethod: "schema.org"}

	if name := getMicrodataProp(recipeNode, "name"); name != "" {
		recipe.Name = name
	}
	if desc := getMicrodataProp(recipeNode, "description"); desc != "" {
		recipe.Description = desc
	}
	if img := getMicrodataImageProp(recipeNode); img != "" {
		recipe.ImageURL = img
	}
	if y := getMicrodataProp(recipeNode, "recipeYield"); y != "" {
		recipe.Yield = cleanYield(y)
	}
	if pt := getMicrodataProp(recipeNode, "prepTime"); pt != "" {
		recipe.PrepTime = pt
	}
	if ct := getMicrodataProp(recipeNode, "cookTime"); ct != "" {
		recipe.CookTime = ct
	}
	if tt := getMicrodataProp(recipeNode, "totalTime"); tt != "" {
		recipe.TotalTime = tt
	}

	for _, ing := range getAllMicrodataProps(recipeNode, "ingredients") {
		recipe.Ingredients = append(recipe.Ingredients, parseIngredient(ing))
	}
	// Also check "recipeIngredient" since some pages use that
	for _, ing := range getAllMicrodataProps(recipeNode, "recipeIngredient") {
		recipe.Ingredients = append(recipe.Ingredients, parseIngredient(ing))
	}

	for _, instr := range getAllMicrodataProps(recipeNode, "recipeInstructions") {
		instr = strings.TrimSpace(instr)
		if instr != "" {
			recipe.Instructions = append(recipe.Instructions, models.Instruction{Text: instr})
		}
	}

	if recipe.Yield != "" {
		fmt.Sscanf(recipe.Yield, "%d", &recipe.Servings)
	}

	if recipe.Name == "" {
		return nil, false
	}

	return recipe, true
}

func findMicrodataRecipe(n *html.Node) *html.Node {
	if n.Type == html.ElementNode {
		for _, attr := range n.Attr {
			if attr.Key == "itemtype" {
				typ := strings.TrimSpace(attr.Val)
				if typ == "http://schema.org/Recipe" || typ == "https://schema.org/Recipe" {
					return n
				}
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findMicrodataRecipe(c); found != nil {
			return found
		}
	}
	return nil
}

func getMicrodataProp(n *html.Node, prop string) string {
	var result string
	var f func(*html.Node)
	f = func(node *html.Node) {
		if result != "" {
			return
		}
		if node.Type == html.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "itemprop" && attr.Val == prop {
					// For img elements, use src/content
					if node.Data == "img" {
						result = getAttrVal(node, "src")
						if result == "" {
							result = getAttrVal(node, "content")
						}
					} else if node.Data == "meta" {
						result = getAttrVal(node, "content")
					} else if node.Data == "time" {
						result = getAttrVal(node, "datetime")
						if result == "" {
							result = textContent(node)
						}
					} else if node.Data == "link" {
						result = getAttrVal(node, "href")
					} else {
						result = textContent(node)
					}
					return
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return strings.TrimSpace(result)
}

func getMicrodataImageProp(n *html.Node) string {
	var f func(*html.Node) string
	f = func(node *html.Node) string {
		if node.Type == html.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "itemprop" && (attr.Val == "image") {
					if node.Data == "img" {
						if src := getAttrVal(node, "src"); src != "" {
							return src
						}
					}
					if href := getAttrVal(node, "href"); href != "" {
						return href
					}
					if content := getAttrVal(node, "content"); content != "" {
						return content
					}
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if found := f(c); found != "" {
				return found
			}
		}
		return ""
	}
	return f(n)
}

func getAllMicrodataProps(n *html.Node, prop string) []string {
	var results []string
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "itemprop" && attr.Val == prop {
					text := ""
					if node.Data == "img" {
						text = getAttrVal(node, "src")
					} else if node.Data == "meta" {
						text = getAttrVal(node, "content")
					} else {
						text = textContent(node)
					}
					text = strings.TrimSpace(text)
					if text != "" {
						results = append(results, text)
					}
					return
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return results
}

func getAttrVal(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
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
