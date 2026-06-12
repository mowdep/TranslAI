package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gabrielfareau/translai/internal/config"
	"github.com/gabrielfareau/translai/internal/core"
	"github.com/gabrielfareau/translai/internal/detect"
	"github.com/gabrielfareau/translai/internal/srt"
	"github.com/gabrielfareau/translai/internal/translate"
)

const maxFilesPerJob = 20

// validLangCodes est la liste blanche des codes langue acceptés.
var validLangCodes = map[string]bool{
	"auto": true,
	"en": true, "fr": true, "es": true, "de": true,
	"it": true, "pt": true, "nl": true, "zh": true,
	"ja": true, "ko": true, "ar": true, "ru": true,
	"pl": true, "sv": true, "da": true, "fi": true,
	"no": true, "tr": true, "cs": true, "hu": true,
}

func isValidLang(s string) bool { return validLangCodes[s] }

// newJobID génère un identifiant opaque de 128 bits via crypto/rand.
func newJobID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// safeFilename retourne le composant terminal du nom de fichier,
// sans caractères pouvant injecter des en-têtes HTTP.
func safeFilename(name string) string {
	base := filepath.Base(name)
	return strings.Map(func(r rune) rune {
		if r == '"' || r == '\r' || r == '\n' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, base)
}

// handleDetect reçoit un fichier SRT, détecte sa langue et renvoie {"detected_lang":"xx"}.
// POST /api/detect
func (s *Server) handleDetect(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "champ 'file' manquant: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer f.Close()

	doc, err := srt.Parse(f)
	if err != nil {
		http.Error(w, "parsing SRT: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	sample := srt.TextSample(doc, 10)
	lang, err := detect.Detect(sample)
	if err != nil {
		http.Error(w, "détection langue: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"detected_lang": lang})
}

// uploadedFile contient un fichier SRT lu en mémoire depuis le multipart.
type uploadedFile struct {
	name string
	data []byte
}

// handleConvert reçoit N fichiers SRT, lance un job en goroutine et renvoie {"job_id":"..."}.
// POST /api/convert
func (s *Server) handleConvert(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	target := r.FormValue("target")
	if !isValidLang(target) {
		http.Error(w, "paramètre 'target' invalide", http.StatusBadRequest)
		return
	}
	source := r.FormValue("source")
	if source == "" {
		source = "auto"
	}
	if !isValidLang(source) {
		http.Error(w, "paramètre 'source' invalide", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "aucun fichier 'files' fourni", http.StatusBadRequest)
		return
	}
	if len(files) > maxFilesPerJob {
		http.Error(w, fmt.Sprintf("maximum %d fichiers par job", maxFilesPerJob), http.StatusBadRequest)
		return
	}

	// Acquérir le sémaphore (max jobs concurrents).
	select {
	case s.jobSem <- struct{}{}:
	default:
		http.Error(w, "serveur occupé, réessayez dans quelques instants", http.StatusServiceUnavailable)
		return
	}

	// Lire les fichiers en mémoire avant de lancer la goroutine.
	uploads := make([]uploadedFile, 0, len(files))
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			<-s.jobSem
			http.Error(w, "ouverture fichier: "+err.Error(), http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(io.LimitReader(f, 10<<20))
		_ = f.Close()
		if err != nil {
			<-s.jobSem
			http.Error(w, "lecture fichier: "+err.Error(), http.StatusBadRequest)
			return
		}
		uploads = append(uploads, uploadedFile{name: safeFilename(fh.Filename), data: data})
	}

	jobID := newJobID()
	job := &JobResult{ID: jobID, createdAt: time.Now()}
	s.jobs.Set(jobID, job)

	_, pcfg := s.cfg.Resolve("", "")
	cfg := s.cfg.Get()
	batchSize := cfg.BatchSize

	go func() {
		defer func() { <-s.jobSem }()
		for _, u := range uploads {
			fr := doTranslateFile(context.Background(), u.name, u.data, source, target, batchSize, pcfg)
			job.addFile(fr)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
}

// doTranslateFile traduit un fichier SRT en mémoire.
func doTranslateFile(ctx context.Context, name string, data []byte, source, target string, batchSize int, pcfg config.ProviderConfig) FileResult {
	doc, err := srt.Parse(bytes.NewReader(data))
	if err != nil {
		return FileResult{Name: name, Err: fmt.Sprintf("parsing SRT: %v", err)}
	}

	if pcfg.BaseURL == "" {
		return FileResult{Name: name, Err: "provider non configuré (base_url vide)"}
	}

	model := pcfg.Model
	if model == "" {
		model = "default"
	}
	tr := translate.NewOpenAICompat("web", pcfg.BaseURL, model, pcfg.APIKey, pcfg.Temperature)

	opts := core.Options{
		Source:     source,
		Target:     target,
		BatchSize:  batchSize,
		Translator: tr,
	}
	if err := core.Translate(ctx, doc, opts, nil); err != nil {
		return FileResult{Name: name, Err: fmt.Sprintf("traduction: %v", err)}
	}

	var buf bytes.Buffer
	if err := srt.Save(&buf, doc); err != nil {
		return FileResult{Name: name, Err: fmt.Sprintf("sérialisation SRT: %v", err)}
	}
	return FileResult{Name: name, SRTOut: buf.Bytes()}
}

// handleConvertStream stream la progression SSE d'un job (GET /api/convert/stream?job_id=).
func (s *Server) handleConvertStream(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		http.Error(w, "job_id requis", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming non supporté", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	sent := make(map[string]bool)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		job, ok := s.jobs.Get(jobID)
		if !ok {
			_ = WriteSSE(w, SSEEvent{Type: "error", Stage: "job introuvable"})
			return
		}

		files := job.getFiles()
		for _, fr := range files {
			if sent[fr.Name] {
				continue
			}
			sent[fr.Name] = true
			if fr.Err != "" {
				_ = WriteSSE(w, SSEEvent{
					Type:  "error",
					Stage: fr.Err,
					File:  fr.Name,
				})
			} else {
				payload := base64.StdEncoding.EncodeToString(fr.SRTOut)
				_ = WriteSSE(w, SSEEvent{
					Type:    "result",
					Stage:   "done",
					Done:    1,
					Total:   1,
					File:    fr.Name,
					Payload: payload,
				})
			}
		}

		_ = WriteSSE(w, SSEEvent{
			Type:  "progress",
			Stage: "processing",
			Done:  len(files),
			Total: len(files),
		})

		if len(files) > 0 {
			return
		}
	}
}

// handleDownload télécharge un fichier SRT traduit (GET /api/download?job_id=&file=).
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	fileName := r.URL.Query().Get("file")
	if jobID == "" || fileName == "" {
		http.Error(w, "job_id et file requis", http.StatusBadRequest)
		return
	}

	job, ok := s.jobs.Get(jobID)
	if !ok {
		http.Error(w, "job introuvable", http.StatusNotFound)
		return
	}

	for _, fr := range job.getFiles() {
		if fr.Name == fileName {
			if fr.Err != "" {
				http.Error(w, "fichier en erreur: "+fr.Err, http.StatusUnprocessableEntity)
				return
			}
			w.Header().Set("Content-Type", "application/x-subrip")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeFilename(fr.Name)))
			w.Header().Set("Content-Length", strconv.Itoa(len(fr.SRTOut)))
			_, _ = w.Write(fr.SRTOut)
			return
		}
	}
	http.Error(w, "fichier non trouvé dans ce job", http.StatusNotFound)
}

// handleDownloadAll télécharge tous les SRT du job dans un .zip (GET /api/download/all?job_id=).
func (s *Server) handleDownloadAll(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		http.Error(w, "job_id requis", http.StatusBadRequest)
		return
	}

	job, ok := s.jobs.Get(jobID)
	if !ok {
		http.Error(w, "job introuvable", http.StatusNotFound)
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, fr := range job.getFiles() {
		if fr.Err != "" || len(fr.SRTOut) == 0 {
			continue
		}
		f, err := zw.Create(safeFilename(fr.Name))
		if err != nil {
			slog.Error("handleDownloadAll: zip create", "file", fr.Name, "err", err)
			continue
		}
		if _, err := f.Write(fr.SRTOut); err != nil {
			slog.Error("handleDownloadAll: zip write", "file", fr.Name, "err", err)
		}
	}
	if err := zw.Close(); err != nil {
		http.Error(w, "création zip: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="translai-%s.zip"`, jobID))
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}
