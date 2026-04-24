package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"io/fs"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"tangled.org/dunkirk.sh/pare/internal/cache"
	"tangled.org/dunkirk.sh/pare/internal/cooklang"
	"tangled.org/dunkirk.sh/pare/internal/extract"
	"tangled.org/dunkirk.sh/pare/internal/models"
	"tangled.org/dunkirk.sh/pare/ui"
)

var gitHash = "dev"

func main() {
	port := flag.Int("port", 3000, "port to listen on")
	dbPath := flag.String("db", "pare.db", "path to SQLite database")
	baseURL := flag.String("base-url", "", "base URL of this service")
	flag.Parse()

	if *baseURL == "" {
		*baseURL = fmt.Sprintf("http://localhost:%d", *port)
	}

	c, err := cache.New(*dbPath)
	if err != nil {
		log.Fatalf("opening cache: %v", err)
	}
	defer c.Close()

	tmpl, err := template.New("").Funcs(template.FuncMap{
		"fmtDuration": fmtDuration,
		"isoToSeconds": isoToSeconds,
		"cleanSource": cleanSource,
		"renderStep":  renderStep,
	}).ParseFS(ui.Templates, "templates/*.html")
	if err != nil {
		log.Fatalf("parsing templates: %v", err)
	}

	srv := &Server{
		pipeline:  extract.NewPipeline(),
		cache:     c,
		templates: tmpl,
		baseURL:   *baseURL,
		gitHash:   gitHash,
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.CleanPath)

	staticContent, err := fs.Sub(ui.Static, "static")
	if err != nil {
		log.Fatalf("failed to get static fs: %v", err)
	}

	r.Get("/", srv.handleIndex)
	r.Get("/recipe", srv.handleRecipe)
	r.Get("/export.cook", srv.handleCookExport)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))))

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("http://localhost:%d", *port)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}

type Server struct {
	pipeline  *extract.Pipeline
	cache     *cache.Cache
	templates *template.Template
	baseURL   string
	gitHash   string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL != "" {
		http.Redirect(w, r, "/recipe?url="+url.QueryEscape(targetURL), http.StatusFound)
		return
	}
	s.templates.ExecuteTemplate(w, "index_page", map[string]string{"GitHash": s.gitHash, "BaseURL": s.baseURL})
}

func (s *Server) handleRecipe(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	recipe, err := s.cache.Get(targetURL)
	if err != nil {
		log.Printf("cache read error: %v", err)
	}
	if recipe == nil {
		result := s.pipeline.Extract(targetURL)
		if result.Error != nil {
			s.renderError(w, result.Error.Error(), targetURL)
			return
		}
		recipe = result.Recipe
		if err := s.cache.Set(targetURL, recipe); err != nil {
			log.Printf("cache write error: %v", err)
		}
	}

	data := map[string]interface{}{
		"Recipe":     recipe,
		"TargetURL": targetURL,
		"GitHash":    s.gitHash,
		"BaseURL":    s.baseURL,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.templates.ExecuteTemplate(w, "recipe_page", data)
}

func (s *Server) handleCookExport(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}

	recipe, err := s.cache.Get(targetURL)
	if err != nil {
		log.Printf("cache read error: %v", err)
	}
	if recipe == nil {
		result := s.pipeline.Extract(targetURL)
		if result.Error != nil {
			http.Error(w, result.Error.Error(), http.StatusBadGateway)
			return
		}
		recipe = result.Recipe
	}

	cook := cooklang.Export(recipe)
	filename := url.PathEscape(recipe.Name) + ".cook"

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Write([]byte(cook))
}

func (s *Server) renderError(w http.ResponseWriter, errMsg, sourceURL string) {
	data := map[string]interface{}{
		"Error":     errMsg,
		"SourceURL": sourceURL,
		"BaseURL":   s.baseURL,
		"GitHash":   s.gitHash,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	s.templates.ExecuteTemplate(w, "error_page", data)
}

func fmtDuration(iso string) string {
	if !strings.HasPrefix(iso, "PT") {
		return iso
	}
	d := strings.TrimPrefix(iso, "PT")
	var parts []string
	if h := before(d, "H"); h != "" {
		parts = append(parts, h+"h")
		d = after(d, "H")
	}
	if m := before(d, "M"); m != "" {
		parts = append(parts, m+"m")
		d = after(d, "M")
	}
	if s := before(d, "S"); s != "" {
		parts = append(parts, s+"s")
	}
	if len(parts) == 0 {
		return iso
	}
	return strings.Join(parts, " ")
}

func isoToSeconds(iso string) int {
	if !strings.HasPrefix(iso, "PT") {
		return 0
	}
	d := strings.TrimPrefix(iso, "PT")
	secs := 0
	if h := before(d, "H"); h != "" {
		if v, err := strconv.Atoi(h); err == nil {
			secs += v * 3600
		}
		d = after(d, "H")
	}
	if m := before(d, "M"); m != "" {
		if v, err := strconv.Atoi(m); err == nil {
			secs += v * 60
		}
		d = after(d, "M")
	}
	if s := before(d, "S"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			secs += v
		}
	}
	return secs
}

func cleanSource(rawURL string) map[string]string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return map[string]string{"host": rawURL, "path": ""}
	}
	host := strings.TrimPrefix(u.Host, "www.")
	path := u.Path
	if path == "/" {
		path = ""
	}
	if len(path) > 35 {
		path = path[:32] + "…"
	}
	return map[string]string{"host": host, "path": path}
}

func before(s, sep string) string {
	i := strings.Index(s, sep)
	if i < 0 {
		return ""
	}
	return s[:i]
}

func after(s, sep string) string {
	i := strings.Index(s, sep)
	if i < 0 {
		return s
	}
	return s[i+len(sep):]
}

func renderStep(text string, ingredients []models.Ingredient) template.HTML {
	annotated := cooklang.AnnotateStepForDisplay(text, ingredients)
	return cooklang.ParseAndRender(annotated)
}

func recipeToJSONLD(r *models.Recipe) map[string]interface{} {
	ld := map[string]interface{}{
		"@context":           "https://schema.org",
		"@type":              "Recipe",
		"name":               r.Name,
		"description":        r.Description,
		"recipeIngredient":   ingredientStrings(r.Ingredients),
		"recipeInstructions": instructionStrings(r.Instructions),
	}
	if r.ImageURL != "" {
		ld["image"] = r.ImageURL
	}
	if r.PrepTime != "" {
		ld["prepTime"] = r.PrepTime
	}
	if r.CookTime != "" {
		ld["cookTime"] = r.CookTime
	}
	if r.TotalTime != "" {
		ld["totalTime"] = r.TotalTime
	}
	if r.Yield != "" {
		ld["recipeYield"] = r.Yield
	}
	return ld
}

func ingredientStrings(ings []models.Ingredient) []string {
	out := make([]string, len(ings))
	for i, ing := range ings {
		out[i] = ing.RawText
	}
	return out
}

func instructionStrings(steps []models.Instruction) []map[string]string {
	out := make([]map[string]string, len(steps))
	for i, step := range steps {
		out[i] = map[string]string{
			"@type": "HowToStep",
			"text":  step.Text,
		}
	}
	return out
}