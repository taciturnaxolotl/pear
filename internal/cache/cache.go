package cache

import (
	"database/sql"
	"encoding/json"
	"time"

	"tangled.org/dunkirk.sh/pear/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

type Cache struct {
	db *sql.DB
}

func New(dbPath string) (*Cache, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Cache{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS recipes (
			url TEXT PRIMARY KEY,
			recipe JSON NOT NULL,
			extraction_method TEXT NOT NULL,
			fetched_at DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS flagged_recipes (
			url TEXT PRIMARY KEY,
			recipe JSON NOT NULL,
			flagged_at DATETIME NOT NULL
		)
	`)
	return err
}

func (c *Cache) Get(url string) (*models.Recipe, error) {
	var recipeJSON []byte
	var method string
	var fetchedAt time.Time

	err := c.db.QueryRow(
		"SELECT recipe, extraction_method, fetched_at FROM recipes WHERE url = ?",
		url,
	).Scan(&recipeJSON, &method, &fetchedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var recipe models.Recipe
	if err := json.Unmarshal(recipeJSON, &recipe); err != nil {
		return nil, err
	}

	if time.Since(fetchedAt) > 24*time.Hour {
		return nil, nil
	}

	return &recipe, nil
}

func (c *Cache) Set(url string, recipe *models.Recipe) error {
	recipeJSON, err := json.Marshal(recipe)
	if err != nil {
		return err
	}

	_, err = c.db.Exec(
		`INSERT INTO recipes (url, recipe, extraction_method, fetched_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(url) DO UPDATE SET recipe=excluded.recipe, extraction_method=excluded.extraction_method, fetched_at=excluded.fetched_at`,
		url, recipeJSON, recipe.ExtractionMethod, time.Now(),
	)
	return err
}

func (c *Cache) Recent(limit int) ([]models.CachedRecipe, error) {
	rows, err := c.db.Query(
		"SELECT url, recipe, extraction_method, fetched_at FROM recipes ORDER BY fetched_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.CachedRecipe
	for rows.Next() {
		var cr models.CachedRecipe
		if err := rows.Scan(&cr.URL, &cr.Recipe, &cr.ExtractionMethod, &cr.FetchedAt); err != nil {
			return nil, err
		}
		results = append(results, cr)
	}
	return results, rows.Err()
}

func (c *Cache) Flag(url string, recipe *models.Recipe) error {
	recipeJSON, err := json.Marshal(recipe)
	if err != nil {
		return err
	}
	_, err = c.db.Exec(
		`INSERT INTO flagged_recipes (url, recipe, flagged_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(url) DO UPDATE SET recipe=excluded.recipe, flagged_at=excluded.flagged_at`,
		url, recipeJSON, time.Now(),
	)
	return err
}

func (c *Cache) IsFlagged(url string) bool {
	var count int
	c.db.QueryRow("SELECT COUNT(*) FROM flagged_recipes WHERE url = ?", url).Scan(&count)
	return count > 0
}

func (c *Cache) Unflag(url string) error {
	_, err := c.db.Exec("DELETE FROM flagged_recipes WHERE url = ?", url)
	return err
}

func (c *Cache) Invalidate(url string) error {
	_, err := c.db.Exec("DELETE FROM recipes WHERE url = ?", url)
	return err
}

func (c *Cache) ListFlagged() ([]models.CachedRecipe, error) {
	rows, err := c.db.Query(
		"SELECT url, recipe, '', flagged_at FROM flagged_recipes ORDER BY flagged_at DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.CachedRecipe
	for rows.Next() {
		var cr models.CachedRecipe
		if err := rows.Scan(&cr.URL, &cr.Recipe, &cr.ExtractionMethod, &cr.FetchedAt); err != nil {
			return nil, err
		}
		results = append(results, cr)
	}
	return results, rows.Err()
}

func (c *Cache) Close() error {
	return c.db.Close()
}