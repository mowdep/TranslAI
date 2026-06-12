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

const (
	maxConcurrentJobs = 10
	jobTTL            = time.Hour
)

// JobStore stocke les résultats de conversion en mémoire (thread-safe).
type JobStore interface {
	Set(id string, job *JobResult)
	Get(id string) (*JobResult, bool)
	Purge(olderThan time.Duration)
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

// Purge supprime les jobs dont createdAt est plus vieux que olderThan.
func (s *memJobStore) Purge(olderThan time.Duration) {
	cutoff := time.Now().Add(-olderThan)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, j := range s.jobs {
		if j.createdAt.Before(cutoff) {
			delete(s.jobs, id)
		}
	}
}

// JobResult contient les résultats d'une conversion.
type JobResult struct {
	ID        string
	Files     []FileResult
	createdAt time.Time
	mu        sync.RWMutex
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
	SRTOut []byte
	Err    string
}

// Server est le serveur HTTP de translai.
type Server struct {
	router      *chi.Mux
	cfg         *config.Store
	jobs        JobStore
	reviewStore ReviewStore
	flushMgr    *FlushManager
	workDir     string
	addr        string
	srv         *http.Server
	tmpl        *template.Template
	tmplAdmin   *template.Template
	tmplReview  *template.Template
	cfgPath     string
	jobSem      chan struct{} // sémaphore : max jobs concurrents
}

// New crée un Server prêt à démarrer.
func New(addr string, store *config.Store, cfgPath string) *Server {
	return NewWithWorkDir(addr, store, cfgPath, "")
}

// NewWithWorkDir crée un Server avec un répertoire de travail pour le write-behind.
func NewWithWorkDir(addr string, store *config.Store, cfgPath, workDir string) *Server {
	rs := NewReviewStore()
	var fm *FlushManager
	if workDir != "" {
		fm = NewFlushManager(rs, workDir)
		fm.Start()
	}
	s := &Server{
		addr:        addr,
		cfg:         store,
		jobs:        newMemJobStore(),
		reviewStore: rs,
		flushMgr:    fm,
		workDir:     workDir,
		cfgPath:     cfgPath,
		jobSem:      make(chan struct{}, maxConcurrentJobs),
	}
	s.loadTemplates()
	s.mountRoutes()
	return s
}

// reviewFuncMap contient les fonctions disponibles dans review.html.
var reviewFuncMap = template.FuncMap{
	"flagIcon": func(f Flag) string {
		switch f {
		case FlagEcho:
			return "echo"
		case FlagEmpty:
			return "vide"
		case FlagRatioLow:
			return "ratio-"
		case FlagRatioHigh:
			return "ratio+"
		case FlagLineMismatch:
			return "lignes"
		case FlagFallback:
			return "fallback"
		case FlagCPSHigh:
			return "cps!"
		case FlagLongLine:
			return "long"
		default:
			return string(f)
		}
	},
}

// loadTemplates parse les templates embarqués.
func (s *Server) loadTemplates() {
	convert := template.Must(template.New("layout.html").ParseFS(templatesFS,
		"web/templates/layout.html",
		"web/templates/convert.html",
	))
	admin := template.Must(template.New("layout.html").ParseFS(templatesFS,
		"web/templates/layout.html",
		"web/templates/admin.html",
	))
	review := template.Must(template.New("layout.html").Funcs(reviewFuncMap).ParseFS(templatesFS,
		"web/templates/layout.html",
		"web/templates/review.html",
	))
	s.tmpl = convert
	s.tmplAdmin = admin
	s.tmplReview = review
}

// securityHeaders ajoute les en-têtes de sécurité HTTP sur toutes les réponses.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// mountRoutes configure le router chi.
func (s *Server) mountRoutes() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(securityHeaders)

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

	// Review
	r.Get("/review", s.handleReview)
	r.Get("/api/review/cues", s.handleGetCues)
	r.Patch("/api/review/cue", s.handlePatchCue)
	r.Post("/api/review/retranslate", s.handleRetranslate)

	// Assets statiques embarqués
	staticSub, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		panic("server: embed static sub: " + err.Error())
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	s.router = r
}

// Run démarre le serveur HTTP et bloque jusqu'à ctx.Done().
func (s *Server) Run(ctx context.Context) error {
	s.srv = &http.Server{
		Addr:        s.addr,
		Handler:     s.router,
		ReadTimeout: 10 * time.Second,
		// WriteTimeout désactivé au niveau serveur : les SSE nécessitent
		// des réponses longues ; le timeout est géré par ctx des handlers.
		IdleTimeout: 60 * time.Second,
	}

	// Purge périodique des jobs expirés (toutes les 15 min).
	go func() {
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.jobs.Purge(jobTTL)
			}
		}
	}()

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
		if s.flushMgr != nil {
			s.flushMgr.FlushAllSync()
			s.flushMgr.Stop()
		}
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutCtx); err != nil {
			slog.Error("server: shutdown error", "err", err)
		}
		return nil
	}
}
