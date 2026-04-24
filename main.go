package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"sync"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"

	"tangled.org/dunkirk.sh/pear/internal/cache"
	"tangled.org/dunkirk.sh/pear/internal/cooklang"
	"tangled.org/dunkirk.sh/pear/internal/extract"
	"tangled.org/dunkirk.sh/pear/internal/models"
	"tangled.org/dunkirk.sh/pear/ui"
)

var gitHash = "dev"

func main() {
	godotenv.Load()
	port := flag.Int("port", 3000, "port to listen on")
	dbPath := flag.String("db", "pear.db", "path to SQLite database")
	baseURL := flag.String("base-url", "", "base URL of this service")
	flag.Parse()

	if *baseURL == "" {
		*baseURL = os.Getenv("BASE_URL")
	}
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
		"cookHighlight": cookHighlight,
		"groupIngredients": groupIngredients,
		"json":        func(v string) string { b, _ := json.Marshal(v); return string(b) },
		"trimProto":   func(s string) string { return strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://") },
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
		pending:   make(map[string]chan extractResult),
		failed:    make(map[string]failedEntry),
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
	r.Get("/cook", srv.handleCookView)
	r.Get("/export.cook", srv.handleCookExport)
	r.Get("/recipe", srv.handleRecipeQuery)
	r.Get("/userscript", srv.handleUserscript)
	r.Get("/status", srv.handleStatus)
	r.Get("/*", srv.handleRecipePath)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))))

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("http://localhost:%d", *port)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}

type Server struct {
	pipeline   *extract.Pipeline
	cache      *cache.Cache
	templates  *template.Template
	baseURL    string
	gitHash    string
	pendingMu  sync.Mutex
	pending    map[string]chan extractResult
	failedMu   sync.Mutex
	failed    map[string]failedEntry
}

type failedEntry struct {
	msg       string
	failedAt  time.Time
}

type extractResult struct {
	recipe *models.Recipe
	err    error
}

type indexRecentRecipe struct {
	Name      string
	ImageURL  string
	SourceURL string
	Domain    string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL != "" {
		if strings.HasPrefix(targetURL, "https://") {
			targetURL = targetURL[8:]
		} else if strings.HasPrefix(targetURL, "http://") {
			targetURL = targetURL[7:]
		}
		http.Redirect(w, r, "/"+targetURL, http.StatusFound)
		return
	}

	var recentRecipes []indexRecentRecipe
	cached, err := s.cache.Recent(6)
	if err != nil {
		log.Printf("cache recent error: %v", err)
	}
	for _, cr := range cached {
		var recipe models.Recipe
		if err := json.Unmarshal(cr.Recipe, &recipe); err != nil {
			continue
		}
		recentRecipes = append(recentRecipes, indexRecentRecipe{
			Name:      recipe.Name,
			ImageURL:  recipe.ImageURL,
			SourceURL: cr.URL,
			Domain:    recipe.SourceDomain,
		})
	}

	data := map[string]interface{}{
		"GitHash":  s.gitHash,
		"BaseURL":  s.baseURL,
		"Recent":   recentRecipes,
	}
	s.templates.ExecuteTemplate(w, "index_page", data)
}

func (s *Server) handleRecipeQuery(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if strings.HasPrefix(targetURL, "https://") {
		targetURL = targetURL[8:]
	} else if strings.HasPrefix(targetURL, "http://") {
		targetURL = targetURL[7:]
	}
	http.Redirect(w, r, "/"+targetURL, http.StatusMovedPermanently)
}

func (s *Server) handleRecipePath(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" || path == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	targetURL := "https://" + path[1:]

	recipe, err := s.cache.Get(targetURL)
	if err != nil {
		log.Printf("cache read error: %v", err)
	}
	if recipe != nil {
		s.renderRecipe(w, recipe, targetURL)
		return
	}

	// Check if extraction already failed (5min TTL)
	s.failedMu.Lock()
	entry, alreadyFailed := s.failed[targetURL]
	s.failedMu.Unlock()
	if alreadyFailed && time.Since(entry.failedAt) < 5*time.Minute {
		s.renderError(w, entry.msg, targetURL)
		return
	}
	if alreadyFailed {
		s.failedMu.Lock()
		delete(s.failed, targetURL)
		s.failedMu.Unlock()
	}

	// Not cached — start extraction if not already in flight
	s.startExtraction(targetURL)

	// Render loading interstitial
	data := map[string]interface{}{
		"TargetURL": targetURL,
		"GitHash":   s.gitHash,
		"BaseURL":   s.baseURL,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.templates.ExecuteTemplate(w, "loading_page", data)
}

func (s *Server) startExtraction(targetURL string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	if _, ok := s.pending[targetURL]; ok {
		return
	}

	ch := make(chan extractResult, 1)
	s.pending[targetURL] = ch

	go func() {
		result := s.pipeline.Extract(targetURL)
		if result.Error != nil {
			s.failedMu.Lock()
			s.failed[targetURL] = failedEntry{msg: result.Error.Error(), failedAt: time.Now()}
			s.failedMu.Unlock()

			ch <- extractResult{err: result.Error}
		} else {
			if err := s.cache.Set(targetURL, result.Recipe); err != nil {
				log.Printf("cache write error: %v", err)
			}
			ch <- extractResult{recipe: result.Recipe}
		}
		close(ch)

		s.pendingMu.Lock()
		delete(s.pending, targetURL)
		s.pendingMu.Unlock()
	}()
}

func (s *Server) handleUserscript(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"GitHash": s.gitHash,
		"BaseURL": s.baseURL,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.templates.ExecuteTemplate(w, "userscript_page", data)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ready":false,"error":"missing url"}`))
		return
	}

	recipe, err := s.cache.Get(targetURL)
	if err != nil {
		log.Printf("cache read error: %v", err)
	}
	if recipe != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ready":true}`))
		return
	}

	s.pendingMu.Lock()
	ch, pending := s.pending[targetURL]
	s.pendingMu.Unlock()
	if !pending {
		// Check if it already failed (5min TTL)
		s.failedMu.Lock()
		entry, failed := s.failed[targetURL]
		s.failedMu.Unlock()
		if failed {
			if time.Since(entry.failedAt) < 5*time.Minute {
				errMsg := strings.ReplaceAll(entry.msg, `"`, `\\"`)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(fmt.Sprintf(`{"ready":false,"error":"%s"}`, errMsg)))
				return
			}
			s.failedMu.Lock()
			delete(s.failed, targetURL)
			s.failedMu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ready":false,"error":"extraction not started"}`))
		return
	}

	// Non-blocking read from channel
	select {
	case res := <-ch:
		w.Header().Set("Content-Type", "application/json")
		if res.err != nil {
			errMsg := strings.ReplaceAll(res.err.Error(), `"`, `\\"`)
			w.Write([]byte(fmt.Sprintf(`{"ready":false,"error":"%s"}`, errMsg)))
		} else {
			w.Write([]byte(`{"ready":true}`))
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ready":false}`))
	}
}

func (s *Server) renderRecipe(w http.ResponseWriter, recipe *models.Recipe, targetURL string) {
	filename := strings.ReplaceAll(recipe.Name, " ", "-") + ".cook"
	data := map[string]interface{}{
		"Recipe":     recipe,
		"TargetURL": targetURL,
		"Filename":  filename,
		"GitHash":    s.gitHash,
		"BaseURL":    s.baseURL,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.templates.ExecuteTemplate(w, "recipe_page", data)
}

func (s *Server) handleCookView(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "missing url parameter", http.StatusBadRequest)
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
	}

	cook := cooklang.Export(recipe)
	filename := strings.ReplaceAll(recipe.Name, " ", "-") + ".cook"

	data := map[string]interface{}{
		"Recipe":     recipe,
		"TargetURL":  targetURL,
		"CookFile":   cook,
		"Filename":  filename,
		"GitHash":    s.gitHash,
		"BaseURL":   s.baseURL,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.templates.ExecuteTemplate(w, "cook_page", data)
}

func (s *Server) handleCookExport(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "missing url parameter", http.StatusBadRequest)
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
			http.Error(w, result.Error.Error(), http.StatusBadGateway)
			return
		}
		recipe = result.Recipe
	}

	cook := cooklang.Export(recipe)
	filename := strings.ReplaceAll(recipe.Name, " ", "-") + ".cook"

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

func renderStep(text string, ingredients []models.Ingredient, lang string) template.HTML {
	if lang == "" || lang == "en" || strings.HasPrefix(lang, "en-") {
		annotated := cooklang.AnnotateStepForDisplay(text, ingredients)
		return cooklang.ParseAndRender(annotated)
	}
	return cooklang.ParseAndRender(cooklang.AnnotateTimersOnly(text, lang))
}

func groupIngredients(ings []models.Ingredient) []ingredientGroup {
	var groups []ingredientGroup
	var current *ingredientGroup
	for i, ing := range ings {
		if current == nil || ing.Group != current.Name {
			groups = append(groups, ingredientGroup{Name: ing.Group, StartIdx: i})
			current = &groups[len(groups)-1]
		}
		current.Items = append(current.Items, ing)
	}
	return groups
}

type ingredientGroup struct {
	Name     string
	Items    []models.Ingredient
	StartIdx int
}

func cookHighlight(raw string) template.HTML {
	return cooklang.Highlight(raw)
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