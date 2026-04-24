package wprm

import (
	"encoding/json"
	"regexp"
	"strings"

	"tangled.org/dunkirk.sh/pear/internal/models"

	"golang.org/x/net/html"
)

func Extract(body string) (*models.Recipe, bool) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, false
	}

	data := findWPRMData(doc)
	if data == nil {
		return nil, false
	}

	for _, recipe := range data {
		if r := parseWPRMRecipe(recipe); r != nil {
			return r, true
		}
	}
	return nil, false
}

var wprmRe = regexp.MustCompile(`window\.wprm_recipes\s*=\s*(\{.+?\})\s*;`)

func findWPRMData(n *html.Node) map[string]json.RawMessage {
	var f func(*html.Node) map[string]json.RawMessage
	f = func(n *html.Node) map[string]json.RawMessage {
		if n.Type == html.ElementNode && n.Data == "script" {
			text := collectText(n)
			if m := wprmRe.FindStringSubmatch(text); len(m) == 2 {
				var data map[string]json.RawMessage
				if json.Unmarshal([]byte(m[1]), &data) == nil {
					return data
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if result := f(c); result != nil {
				return result
			}
		}
		return nil
	}
	return f(n)
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

func parseWPRMRecipe(raw json.RawMessage) *models.Recipe {
	var r struct {
		Name             string `json:"name"`
		Description      string `json:"summary"`
		ImageURL         string `json:"image_url"`
		OriginalServings string `json:"originalServings"`
		PrepTime         string `json:"prep_time"`
		CookTime         string `json:"cook_time"`
		TotalTime        string `json:"total_time"`
		Ingredients      []struct {
			Amount string `json:"amount"`
			Unit   string `json:"unit"`
			Name   string `json:"name"`
			Notes  string `json:"notes"`
			UID    int    `json:"uid"`
		} `json:"ingredients"`
		Instructions []struct {
			Text string `json:"text"`
		} `json:"instructions"`
		IngredientGroups []struct {
			Name        string `json:"name"`
			Ingredients []int  `json:"ingredients"`
		} `json:"ingredient_groups"`
	}

	if err := json.Unmarshal(raw, &r); err != nil {
		return nil
	}
	if r.Name == "" {
		return nil
	}

	recipe := &models.Recipe{
		Name:            r.Name,
		Description:     r.Description,
		ImageURL:        r.ImageURL,
		Yield:           r.OriginalServings,
		ExtractionMethod: "wprm",
	}

	if r.PrepTime != "" {
		recipe.PrepTime = "PT" + r.PrepTime
	}
	if r.CookTime != "" {
		recipe.CookTime = "PT" + r.CookTime
	}
	if r.TotalTime != "" {
		recipe.TotalTime = "PT" + r.TotalTime
	}

	uidToIng := make(map[int]models.Ingredient)
	for _, ing := range r.Ingredients {
		uidToIng[ing.UID] = buildWPRMIngredient(ing, "")
	}

	if len(r.IngredientGroups) > 0 {
		assigned := make(map[int]bool)
		for _, group := range r.IngredientGroups {
			for _, uid := range group.Ingredients {
				if ing, ok := uidToIng[uid]; ok {
					ing.Group = group.Name
					recipe.Ingredients = append(recipe.Ingredients, ing)
					assigned[uid] = true
				}
			}
		}
		for _, ing := range r.Ingredients {
			if !assigned[ing.UID] {
				recipe.Ingredients = append(recipe.Ingredients, uidToIng[ing.UID])
			}
		}
	} else {
		for _, ing := range r.Ingredients {
			recipe.Ingredients = append(recipe.Ingredients, uidToIng[ing.UID])
		}
	}

	for _, instr := range r.Instructions {
		if instr.Text != "" {
			recipe.Instructions = append(recipe.Instructions, models.Instruction{Text: instr.Text})
		}
	}

	return recipe
}

func buildWPRMIngredient(ing struct {
	Amount string `json:"amount"`
	Unit   string `json:"unit"`
	Name   string `json:"name"`
	Notes  string `json:"notes"`
	UID    int    `json:"uid"`
}, group string) models.Ingredient {
	var rawParts []string
	if ing.Amount != "" {
		rawParts = append(rawParts, ing.Amount)
	}
	if ing.Unit != "" {
		rawParts = append(rawParts, ing.Unit)
	}
	rawParts = append(rawParts, ing.Name)
	if ing.Notes != "" {
		rawParts = append(rawParts, ing.Notes)
	}
	rawText := strings.Join(rawParts, " ")

	return models.Ingredient{
		RawText:  rawText,
		Quantity: ing.Amount,
		Unit:     ing.Unit,
		Name:     ing.Name,
		Group:    group,
	}
}
