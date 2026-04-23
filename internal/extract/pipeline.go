package extract

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"tangled.org/dunkirk.sh/pare/internal/extract/hrecipe"
	"tangled.org/dunkirk.sh/pare/internal/extract/schema"
	"tangled.org/dunkirk.sh/pare/internal/models"
)

type Pipeline struct {
	client *http.Client
}

func NewPipeline() *Pipeline {
	return &Pipeline{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

type Result struct {
	Recipe *models.Recipe
	Error  error
}

func (p *Pipeline) Extract(targetURL string) *Result {
	body, err := p.fetch(targetURL)
	if err != nil {
		return &Result{Error: fmt.Errorf("fetching page: %w", err)}
	}

	if recipe, ok := schema.Extract(body); ok {
		recipe.SourceURL = targetURL
		recipe.SourceDomain = domainOf(targetURL)
		return &Result{Recipe: recipe}
	}

	if recipe, ok := schema.ExtractMicrodata(body); ok {
		recipe.SourceURL = targetURL
		recipe.SourceDomain = domainOf(targetURL)
		return &Result{Recipe: recipe}
	}

	if recipe, ok := hrecipe.Extract(body); ok {
		recipe.SourceURL = targetURL
		recipe.SourceDomain = domainOf(targetURL)
		return &Result{Recipe: recipe}
	}

	return &Result{Error: fmt.Errorf("no recipe found on page — tried JSON-LD, microdata, and h-recipe extraction")}
}

func (p *Pipeline) fetch(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "Pare/1.0 (recipe extractor; like a read-it-later service)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func domainOf(url string) string {
	parts := strings.SplitAfter(url, "://")
	if len(parts) < 2 {
		return url
	}
	host := strings.Split(parts[1], "/")[0]
	return host
}
