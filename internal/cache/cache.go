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

func (c *Cache) Close() error {
	return c.db.Close()
}