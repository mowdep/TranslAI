package config_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gabrielfareau/translai/internal/config"
)

// helpers

func sampleConfig() *config.Config {
	return &config.Config{
		ActiveProvider: "ollama",
		DefaultTarget:  "fr",
		BatchSize:      50,
		Concurrency:    4,
		Providers: map[string]config.ProviderConfig{
			"ollama": {
				Type:        "openai_compat",
				BaseURL:     "http://localhost:11434/v1",
				Model:       "llama3.2",
				APIKey:      "",
				Temperature: 0.3,
			},
			"openai": {
				Type:        "openai_compat",
				BaseURL:     "https://api.openai.com/v1",
				Model:       "gpt-4o-mini",
				APIKey:      "sk-secret",
				Temperature: 0.2,
			},
		},
	}
}

// TestLoadSaveRoundTrip vérifie que Load(Save(c)) == c avec défauts appliqués.
func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := sampleConfig()
	if err := config.Save(path, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ActiveProvider != original.ActiveProvider {
		t.Errorf("ActiveProvider: got %q, want %q", loaded.ActiveProvider, original.ActiveProvider)
	}
	if loaded.DefaultTarget != original.DefaultTarget {
		t.Errorf("DefaultTarget: got %q, want %q", loaded.DefaultTarget, original.DefaultTarget)
	}
	if loaded.BatchSize != original.BatchSize {
		t.Errorf("BatchSize: got %d, want %d", loaded.BatchSize, original.BatchSize)
	}
	if loaded.Concurrency != original.Concurrency {
		t.Errorf("Concurrency: got %d, want %d", loaded.Concurrency, original.Concurrency)
	}
	if len(loaded.Providers) != len(original.Providers) {
		t.Errorf("Providers count: got %d, want %d", len(loaded.Providers), len(original.Providers))
	}
	ollama := loaded.Providers["ollama"]
	if ollama.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("ollama BaseURL: got %q", ollama.BaseURL)
	}
	if ollama.Temperature != 0.3 {
		t.Errorf("ollama Temperature: got %v, want 0.3", ollama.Temperature)
	}
}

// TestLoadDefaults vérifie que les valeurs par défaut sont appliquées quand les champs sont vides.
func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "minimal.yaml")

	// Config minimale : pas de BatchSize ni Concurrency ni Temperature.
	minimal := &config.Config{
		ActiveProvider: "ollama",
		Providers: map[string]config.ProviderConfig{
			"ollama": {
				Type:    "openai_compat",
				BaseURL: "http://localhost:11434/v1",
				Model:   "llama3.2",
			},
		},
	}
	if err := config.Save(path, minimal); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.BatchSize != 25 {
		t.Errorf("BatchSize défaut: got %d, want 25", loaded.BatchSize)
	}
	if loaded.Concurrency != 2 {
		t.Errorf("Concurrency défaut: got %d, want 2", loaded.Concurrency)
	}
	if loaded.Providers["ollama"].Temperature != 0.2 {
		t.Errorf("Temperature défaut: got %v, want 0.2", loaded.Providers["ollama"].Temperature)
	}
}

// TestLoadMissingFile vérifie que Load retourne une Config avec défauts si le fichier est absent.
func TestLoadMissingFile(t *testing.T) {
	c, err := config.Load("/tmp/translai_does_not_exist_xyz.yaml")
	if err != nil {
		t.Fatalf("Load fichier absent: %v", err)
	}
	if c.BatchSize != 25 {
		t.Errorf("BatchSize défaut: got %d, want 25", c.BatchSize)
	}
	if c.Concurrency != 2 {
		t.Errorf("Concurrency défaut: got %d, want 2", c.Concurrency)
	}
}

// TestValidate vérifie les cas d'erreur de Validate.
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name: "valide openai_compat",
			cfg: &config.Config{
				ActiveProvider: "ollama",
				Providers: map[string]config.ProviderConfig{
					"ollama": {Type: "openai_compat", BaseURL: "http://localhost:11434/v1"},
				},
			},
			wantErr: false,
		},
		{
			name: "valide anthropic sans base_url",
			cfg: &config.Config{
				ActiveProvider: "ant",
				Providers: map[string]config.ProviderConfig{
					"ant": {Type: "anthropic", Model: "claude-3-5-haiku-20241022", APIKey: "sk-ant-xxx"},
				},
			},
			wantErr: false,
		},
		{
			name: "valide gemini sans base_url",
			cfg: &config.Config{
				ActiveProvider: "gem",
				Providers: map[string]config.ProviderConfig{
					"gem": {Type: "gemini", Model: "gemini-1.5-flash", APIKey: "AIza-xxx"},
				},
			},
			wantErr: false,
		},
		{
			name: "type inconnu",
			cfg: &config.Config{
				ActiveProvider: "bad",
				Providers: map[string]config.ProviderConfig{
					"bad": {Type: "unknown", BaseURL: "http://example.com"},
				},
			},
			wantErr: true,
		},
		{
			name:    "active_provider vide",
			cfg:     &config.Config{},
			wantErr: true,
		},
		{
			name: "provider absent",
			cfg: &config.Config{
				ActiveProvider: "missing",
				Providers:      map[string]config.ProviderConfig{},
			},
			wantErr: true,
		},
		{
			name: "base_url vide pour openai_compat",
			cfg: &config.Config{
				ActiveProvider: "ollama",
				Providers: map[string]config.ProviderConfig{
					"ollama": {Type: "openai_compat", BaseURL: ""},
				},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := config.Validate(tc.cfg)
			if tc.wantErr && err == nil {
				t.Error("attendait une erreur, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("erreur inattendue: %v", err)
			}
		})
	}
}

// TestStoreMasking vérifie que Get masque les clés API non vides.
func TestStoreMasking(t *testing.T) {
	c := sampleConfig()
	store := config.NewStore(c)
	got := store.Get()

	ollama := got.Providers["ollama"]
	if ollama.APIKey != "" {
		t.Errorf("APIKey vide devrait rester vide, got %q", ollama.APIKey)
	}
	openai := got.Providers["openai"]
	if openai.APIKey != "***" {
		t.Errorf("APIKey non vide devrait être ***, got %q", openai.APIKey)
	}
}

// TestStoreConcurrent vérifie la sécurité concurrent (à lancer avec -race).
func TestStoreConcurrent(t *testing.T) {
	c := sampleConfig()
	store := config.NewStore(c)

	var wg sync.WaitGroup
	const goroutines = 50

	// Moitié lecteurs, moitié écrivains.
	for i := range goroutines {
		wg.Add(1)
		if i%2 == 0 {
			go func() {
				defer wg.Done()
				_ = store.Get()
			}()
		} else {
			go func() {
				defer wg.Done()
				store.Update(config.Config{
					ActiveProvider: "ollama",
					BatchSize:      25,
					Concurrency:    2,
					Providers: map[string]config.ProviderConfig{
						"ollama": {BaseURL: "http://localhost:11434/v1", Temperature: 0.2},
					},
				})
			}()
		}
	}
	wg.Wait()
}

// TestStoreResolve vérifie la résolution du provider actif et les overrides CLI.
func TestStoreResolve(t *testing.T) {
	c := sampleConfig()
	store := config.NewStore(c)

	// Résolution sans override : provider actif = "ollama".
	name, p := store.Resolve("", "")
	if name != "ollama" {
		t.Errorf("provider attendu ollama, got %q", name)
	}
	if p.Model != "llama3.2" {
		t.Errorf("model attendu llama3.2, got %q", p.Model)
	}

	// Override provider.
	name, p = store.Resolve("openai", "")
	if name != "openai" {
		t.Errorf("provider attendu openai, got %q", name)
	}
	if p.Model != "gpt-4o-mini" {
		t.Errorf("model attendu gpt-4o-mini, got %q", p.Model)
	}

	// Override model seulement.
	name, p = store.Resolve("", "mistral")
	if name != "ollama" {
		t.Errorf("provider attendu ollama, got %q", name)
	}
	if p.Model != "mistral" {
		t.Errorf("model attendu mistral, got %q", p.Model)
	}

	// Override provider ET model.
	name, p = store.Resolve("openai", "gpt-4o")
	if name != "openai" {
		t.Errorf("provider attendu openai, got %q", name)
	}
	if p.Model != "gpt-4o" {
		t.Errorf("model attendu gpt-4o, got %q", p.Model)
	}

	// La clé API doit être en clair dans Resolve (usage pipeline interne).
	if p.APIKey != "sk-secret" {
		t.Errorf("APIKey devrait être en clair dans Resolve, got %q", p.APIKey)
	}
}

// TestSavePermissions vérifie que le fichier est créé avec les droits 0600.
func TestSavePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	c := sampleConfig()
	if err := config.Save(path, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissions: got %o, want 600", info.Mode().Perm())
	}
}
