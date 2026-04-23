package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"tangled.org/dunkirk.sh/pare/internal/models"
)

type Extractor struct {
	apiKey  string
	model   string
	baseURL string
}

func NewExtractor(apiKey, model, baseURL string) *Extractor {
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1/messages"
	}
	return &Extractor{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
	}
}

func (e *Extractor) Extract(pageText, sourceURL string) (*models.Recipe, error) {
	if e.apiKey == "" {
		return nil, fmt.Errorf("AI extraction not configured: no API key set")
	}

	prompt := buildPrompt(pageText, sourceURL)

	reqBody := map[string]interface{}{
		"model":      e.model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest("POST", e.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", e.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(result.Content) == 0 {
		return nil, fmt.Errorf("empty response from AI")
	}

	return parseAIResponse(result.Content[0].Text, sourceURL)
}

func buildPrompt(pageText, sourceURL string) string {
	return fmt.Sprintf(`Extract the recipe from the following page text. Return ONLY valid JSON matching this schema, no markdown fences:

{
  "name": "string",
  "description": "string",
  "image_url": "string",
  "prep_time": "string (ISO 8601 duration like PT20M)",
  "cook_time": "string (ISO 8601 duration like PT40M)",
  "total_time": "string (ISO 8601 duration)",
  "yield": "string",
  "servings": 4,
  "ingredients": ["2 cups flour", "1 tsp salt"],
  "instructions": ["Step one text", "Step two text"]
}

Page text:
%s`, truncate(pageText, 8000))
}

func parseAIResponse(text, sourceURL string) (*models.Recipe, error) {
	cleaned := strings.TrimSpace(text)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var raw struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		ImageURL     string   `json:"image_url"`
		PrepTime     string   `json:"prep_time"`
		CookTime     string   `json:"cook_time"`
		TotalTime    string   `json:"total_time"`
		Yield        string   `json:"yield"`
		Servings     int      `json:"servings"`
		Ingredients  []string `json:"ingredients"`
		Instructions []string `json:"instructions"`
	}

	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		return nil, fmt.Errorf("parsing AI JSON response: %w", err)
	}

	recipe := &models.Recipe{
		Name:             raw.Name,
		Description:      raw.Description,
		ImageURL:         raw.ImageURL,
		SourceURL:        sourceURL,
		PrepTime:         raw.PrepTime,
		CookTime:         raw.CookTime,
		TotalTime:        raw.TotalTime,
		Yield:            raw.Yield,
		Servings:         raw.Servings,
		ExtractionMethod: "ai",
	}

	for _, ing := range raw.Ingredients {
		recipe.Ingredients = append(recipe.Ingredients, models.Ingredient{RawText: ing})
	}

	for _, step := range raw.Instructions {
		recipe.Instructions = append(recipe.Instructions, models.Instruction{Text: step})
	}

	if recipe.Name == "" {
		return nil, fmt.Errorf("AI extraction returned empty recipe name")
	}

	return recipe, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}