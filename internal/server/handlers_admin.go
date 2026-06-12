package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gabrielfareau/translai/internal/config"
	"github.com/gabrielfareau/translai/internal/translate"
)

// handleIndex sert la page de conversion (GET /).
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Get()
	data := map[string]any{
		"Page":          "convert",
		"Title":         "Conversion",
		"DefaultTarget": cfg.DefaultTarget,
	}
	if err := s.tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		slog.Error("handleIndex: template", "err", err)
		http.Error(w, "erreur template", http.StatusInternalServerError)
	}
}

// handleAdmin sert la page d'administration (GET /admin).
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Get() // clés API déjà masquées par Store.Get
	data := map[string]any{
		"Page":           "admin",
		"Title":          "Administration",
		"ActiveProvider": cfg.ActiveProvider,
		"DefaultTarget":  cfg.DefaultTarget,
		"BatchSize":      cfg.BatchSize,
		"Concurrency":    cfg.Concurrency,
		"Providers":      cfg.Providers,
	}
	if err := s.tmplAdmin.ExecuteTemplate(w, "layout.html", data); err != nil {
		slog.Error("handleAdmin: template", "err", err)
		http.Error(w, "erreur template", http.StatusInternalServerError)
	}
}

// handleGetConfig retourne la config courante en JSON, APIKey masquée (GET /api/config).
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Get() // Store.Get masque déjà les clés API
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		slog.Error("handleGetConfig: encode", "err", err)
	}
}

// handlePostConfig met à jour la config et la sauvegarde (POST /api/config).
// Corps JSON : même structure que Config.
func (s *Server) handlePostConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "lecture corps: "+err.Error(), http.StatusBadRequest)
		return
	}
	var incoming config.Config
	if err := json.Unmarshal(body, &incoming); err != nil {
		http.Error(w, "JSON invalide: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Pour les clés API masquées ("***"), conserver la valeur courante.
	current := s.cfg.Get()
	mergeMaskedKeys(&incoming, &current)

	s.cfg.Update(incoming)

	if s.cfgPath != "" {
		if err := config.Save(s.cfgPath, &incoming); err != nil {
			slog.Error("handlePostConfig: save", "err", err)
			http.Error(w, "sauvegarde config: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// mergeMaskedKeys conserve les vraies clés API quand le client renvoie "***".
func mergeMaskedKeys(incoming *config.Config, current *config.Config) {
	if incoming.Providers == nil || current.Providers == nil {
		return
	}
	for name, p := range incoming.Providers {
		if p.APIKey == "***" {
			if cur, ok := current.Providers[name]; ok {
				p.APIKey = cur.APIKey
				incoming.Providers[name] = p
			}
		}
	}
}

// testProviderRequest est le corps de POST /api/test-provider.
type testProviderRequest struct {
	Provider string `json:"provider"`
}

// handleTestProvider tente une connexion au provider sélectionné (POST /api/test-provider).
// Envoie une requête triviale (liste de modèles ou traduction d'un mot).
func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		http.Error(w, "lecture corps: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req testProviderRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "JSON invalide: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Résoudre le provider (sans masquage des clés API via Resolve).
	_, pcfg := s.cfg.Resolve(req.Provider, "")
	if pcfg.BaseURL == "" {
		jsonErr(w, "provider introuvable ou base_url vide", http.StatusBadRequest)
		return
	}

	model := pcfg.Model
	if model == "" {
		model = "test"
	}
	tr := translate.NewOpenAICompat(req.Provider, pcfg.BaseURL, model, pcfg.APIKey, pcfg.Temperature)

	// Envoi d'une traduction triviale pour tester la connexion.
	ctx := r.Context()
	out, err := tr.Translate(ctx, translate.Request{
		SourceLang: "en",
		TargetLang: "fr",
		Texts:      []string{"test"},
	})
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		resp := map[string]any{"ok": false, "error": fmt.Sprintf("connexion KO: %v", err)}
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	msg := "connexion OK"
	if len(out) > 0 {
		msg += " — réponse: " + out[0]
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": msg})
}

// jsonErr écrit une réponse d'erreur JSON.
func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
