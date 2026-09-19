package models

import (
	"html"
	"regexp"
	"time"
)

type Recipe struct {
	Name         string
	Description  string
	ImageURL     string
	SourceURL    string
	SourceDomain string
	PrepTime     string
	CookTime     string
	TotalTime    string
	Yield        string
	Servings     int
	Ingredients  []Ingredient
	Instructions []Instruction
	Language         string
	ExtractionMethod string
}

type Ingredient struct {
	RawText string
	Quantity string
	Unit     string
	Name     string
	Group    string
}

type Instruction struct {
	Text string
}

type CachedRecipe struct {
	URL        string
	Recipe     []byte
	ExtractionMethod string
	FetchedAt  time.Time
}

// doubledParens matches a parenthesised note wrapped in a second, redundant
// pair, like "((or lemon juice))". Recipe plugins produce these when an author
// already bracketed a note and the plugin adds its own brackets while building
// the machine-readable copy of the recipe; the page itself renders the note
// correctly, so only the data we consume is affected. The inner group must be
// bracket-free, which leaves real nesting such as "(2 cups (packed))" alone.
var doubledParens = regexp.MustCompile(`\(\(([^()]*)\)\)`)

func tidyText(s string) string {
	s = html.UnescapeString(s)
	return doubledParens.ReplaceAllString(s, "($1)")
}

func (r *Recipe) Normalize() {
	r.Name = tidyText(r.Name)
	r.Description = tidyText(r.Description)
	for i := range r.Ingredients {
		r.Ingredients[i].RawText = tidyText(r.Ingredients[i].RawText)
		r.Ingredients[i].Name = tidyText(r.Ingredients[i].Name)
		r.Ingredients[i].Group = tidyText(r.Ingredients[i].Group)
	}
	for i := range r.Instructions {
		r.Instructions[i].Text = tidyText(r.Instructions[i].Text)
	}
}