package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gabrielfareau/translai/internal/translate"
)

// handleReview sert la page d'edition alignement (GET /review?job=ID&file=...).
func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job")
	file := r.URL.Query().Get("file")

	var cues []*CueState
	if jobID != "" && file != "" {
		if job, ok := s.reviewStore.GetReview(jobID); ok {
			job.mu.RLock()
			if fs, ok := job.files[file]; ok {
				cues = make([]*CueState, len(fs.Cues))
				copy(cues, fs.Cues)
			}
			job.mu.RUnlock()
		}
	}

	data := map[string]any{
		"Page":  "review",
		"Title": "Editeur d'alignement",
		"JobID": jobID,
		"File":  file,
		"Cues":  cues,
	}
	if err := s.tmplReview.ExecuteTemplate(w, "layout.html", data); err != nil {
		slog.Error("handleReview: template", "err", err)
		http.Error(w, "erreur template", http.StatusInternalServerError)
	}
}

// handleGetCues retourne les cues alignees + flags en JSON (GET /api/review/cues?job=ID&file=...).
func (s *Server) handleGetCues(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job")
	file := r.URL.Query().Get("file")
	if jobID == "" || file == "" {
		http.Error(w, "parametres job et file requis", http.StatusBadRequest)
		return
	}

	cues, err := s.reviewStore.GetCues(jobID, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cues)
}

// PatchCueRequest est le corps de PATCH /api/review/cue.
type PatchCueRequest struct {
	Job   string   `json:"job"`
	File  string   `json:"file"`
	Index int      `json:"index"`
	Lines []string `json:"lines"` // nouvelles lignes cible
}

// handlePatchCue met a jour une cue en memoire (PATCH /api/review/cue).
func (s *Server) handlePatchCue(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "lecture corps: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req PatchCueRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "JSON invalide: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Job == "" || req.File == "" {
		http.Error(w, "job et file requis", http.StatusBadRequest)
		return
	}

	if err := s.reviewStore.UpdateCue(req.Job, req.File, req.Index, req.Lines); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Notifier le flush manager (debounce).
	if s.flushMgr != nil {
		s.flushMgr.NotifyEdit(req.Job)
	}

	// Retourner la CueState mise a jour.
	cues, err := s.reviewStore.GetCues(req.Job, req.File)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var updated *CueState
	for _, c := range cues {
		if c.Index == req.Index {
			updated = c
			break
		}
	}
	if updated == nil {
		http.Error(w, "cue introuvable apres mise a jour", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

// RetranslateRequest est le corps de POST /api/review/retranslate.
type RetranslateRequest struct {
	Job   string `json:"job"`
	File  string `json:"file"`
	Index int    `json:"index"`
}

// handleRetranslate retraduit une cue (POST /api/review/retranslate).
func (s *Server) handleRetranslate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		http.Error(w, "lecture corps: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req RetranslateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "JSON invalide: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Job == "" || req.File == "" {
		http.Error(w, "job et file requis", http.StatusBadRequest)
		return
	}

	cues, err := s.reviewStore.GetCues(req.Job, req.File)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Trouver la cue cible et construire le contexte (2-3 cues precedentes).
	var targetIdx int = -1
	for i, c := range cues {
		if c.Index == req.Index {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		http.Error(w, "cue introuvable", http.StatusNotFound)
		return
	}

	targetCue := cues[targetIdx]
	sourceText := strings.Join(targetCue.SourceLines, " ⏎ ")

	// Contexte : 2-3 cues precedentes deja traduites.
	ctxStart := targetIdx - 3
	if ctxStart < 0 {
		ctxStart = 0
	}
	var ctxLines []string
	for _, c := range cues[ctxStart:targetIdx] {
		ctxLines = append(ctxLines, strings.Join(c.TargetLines, " ⏎ "))
	}

	// Resoudre le provider.
	_, pcfg := s.cfg.Resolve("", "")
	if pcfg.BaseURL == "" {
		http.Error(w, "provider non configure", http.StatusServiceUnavailable)
		return
	}
	model := pcfg.Model
	if model == "" {
		model = "default"
	}
	tr := translate.NewOpenAICompat("retranslate", pcfg.BaseURL, model, pcfg.APIKey, pcfg.Temperature)

	cfg := s.cfg.Get()
	ctx := r.Context()
	out, err := retranslateCue(ctx, tr, cfg.DefaultTarget, sourceText, ctxLines)
	if err != nil {
		slog.Error("handleRetranslate: translate", "err", err)
		http.Error(w, "traduction echouee: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Mettre a jour en memoire.
	newLines := strings.Split(out, " ⏎ ")
	if err := s.reviewStore.UpdateCue(req.Job, req.File, req.Index, newLines); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.flushMgr != nil {
		s.flushMgr.NotifyEdit(req.Job)
	}

	// Retourner la CueState mise a jour.
	cues, err = s.reviewStore.GetCues(req.Job, req.File)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var updated *CueState
	for _, c := range cues {
		if c.Index == req.Index {
			updated = c
			break
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

// retranslateCue appelle le translator pour une seule cue avec contexte.
func retranslateCue(ctx context.Context, tr translate.Translator, target, sourceText string, ctxLines []string) (string, error) {
	req := translate.Request{
		SourceLang: "auto",
		TargetLang: target,
		Texts:      []string{sourceText},
		Context:    ctxLines,
	}
	out, err := tr.Translate(ctx, req)
	if err != nil {
		return "", err
	}
	if len(out) == 0 {
		return sourceText, nil
	}
	return out[0], nil
}

