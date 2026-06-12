// Package server expose un serveur HTTP avec interface HTMX pour translai.
// Assets (templates HTML, JS, CSS) embarqués via //go:embed — aucun fichier
// externe requis à l'exécution.
//
// Aucune logique métier ici : les handlers délèguent à internal/core,
// internal/config, internal/detect, internal/srt.
package server

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/gabrielfareau/translai/internal/config"
)

//go:embed web/static
var staticFS embed.FS

//go:embed web/templates
var templatesFS embed.FS

// JobStore stocke les résultats de conversion en mémoire (thread-safe).
// La persistance disque (write-behind) est réservée à la Phase 8.5.
type JobStore interface {
	Set(id string, job *JobResult)
	Get(id string) (*JobResult, bool)
}

// memJobStore est l'implémentation in-memory de JobStore.
type memJobStore struct {
	mu   sync.RWMutex
	jobs map[string]*JobResult
}

func newMemJobStore() *memJobStore {
	return &memJobStore{jobs: make(map[string]*JobResult)}
}

func (s *memJobStore) Set(id string, job *JobResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[id] = job
}

func (s *memJobStore) Get(id string) (*JobResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

// JobResult contient les résultats d'une conversion.
type JobResult struct {
	ID    string
	Files []FileResult
	mu    sync.RWMutex
}

// addFile ajoute un FileResult de façon thread-safe.
func (j *JobResult) addFile(fr FileResult) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Files = append(j.Files, fr)
}

// getFiles retourne une copie thread-safe des résultats.
func (j *JobResult) getFiles() []FileResult {
	j.mu.RLock()
	defer j.mu.RUnlock()
	out := make([]FileResult, len(j.Files))
	copy(out, j.Files)
	return out
}

// FileResult contient le résultat de la traduction d'un fichier.
type FileResult struct {
	Name   string
	SRTOut []byte // SRT traduit sérialisé
	Err    string // vide si succès
}

// Server est le serveur HTTP de translai.
type Server struct {
	router    *chi.Mux
	cfg       *config.Store
	jobs      JobStore
	addr      string
	srv       *http.Server
	tmpl      *template.Template // template set pour la page convert (/)
	tmplAdmin *template.Template // template set pour la page admin (/admin)
	cfgPath   string
}

// New crée un Server prêt à démarrer.
// cfgPath est le chemin vers config.yaml utilisé pour sauvegarder via POST /api/config.
func New(addr string, store *config.Store, cfgPath string) *Server {
	s := &Server{
		addr:    addr,
		cfg:     store,
		jobs:    newMemJobStore(),
		cfgPath: cfgPath,
	}
	s.loadTemplates()
	s.mountRoutes()
	return s
}

// loadTemplates parse les templates embarqués.
// Chaque page est un template set séparé pour éviter les collisions sur le
// bloc "content" (convert.html et admin.html définissent tous deux "content").
func (s *Server) loadTemplates() {
	// Template set convert : layout + convert.html
	convert := template.Must(template.New("layout.html").ParseFS(templatesFS,
		"web/templates/layout.html",
		"web/templates/convert.html",
	))
	// Template set admin : layout + admin.html
	admin := template.Must(template.New("layout.html").ParseFS(templatesFS,
		"web/templates/layout.html",
		"web/templates/admin.html",
	))
	// On stocke les deux dans le champ tmpl via une convention de nommage :
	// utiliser AddParseTree pour combiner dans un seul template.
	// Approche alternative : stocker les deux sets séparément.
	_ = convert
	_ = admin
	s.tmpl = convert
	s.tmplAdmin = admin
}

// mountRoutes configure le router chi.
func (s *Server) mountRoutes() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Pages
	r.Get("/", s.handleIndex)
	r.Get("/admin", s.handleAdmin)

	// API config
	r.Get("/api/config", s.handleGetConfig)
	r.Post("/api/config", s.handlePostConfig)
	r.Post("/api/test-provider", s.handleTestProvider)

	// API conversion
	r.Post("/api/detect", s.handleDetect)
	r.Post("/api/convert", s.handleConvert)
	r.Get("/api/convert/stream", s.handleConvertStream)
	r.Get("/api/download", s.handleDownload)
	r.Get("/api/download/all", s.handleDownloadAll)

	// Assets statiques embarqués
	staticSub, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		panic("server: embed static sub: " + err.Error())
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	s.router = r
}

// Run démarre le serveur HTTP et bloque jusqu'à ctx.Done().
// Effectue un graceful shutdown avec timeout de 5 s.
func (s *Server) Run(ctx context.Context) error {
	s.srv = &http.Server{
		Addr:    s.addr,
		Handler: s.router,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server: démarrage", "addr", s.addr)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("server: arrêt gracieux…")
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutCtx); err != nil {
			slog.Error("server: shutdown error", "err", err)
		}
		return nil
	}
}
