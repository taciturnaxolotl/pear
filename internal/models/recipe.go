package models

import "time"

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