# Spec — internal/config (Phase 7)

Config YAML éditable à la main, accès thread-safe.

## Fichiers
- `config.go` — types, load/save YAML, validation
- `store.go` — accès thread-safe (mutex)

## Types
```go
type Config struct {
    ActiveProvider string                    `yaml:"active_provider"` // ollama|llamacpp|openai
    DefaultTarget  string                    `yaml:"default_target"`  // ex: "fr"
    Providers      map[string]ProviderConfig `yaml:"providers"`
    BatchSize      int                       `yaml:"batch_size"`      // défaut 25
    Concurrency    int                       `yaml:"concurrency"`     // défaut 2
}

type ProviderConfig struct {
    Type        string  `yaml:"type"`        // openai_compat (anthropic/gemini post-v0)
    BaseURL     string  `yaml:"base_url"`    // ex: http://localhost:11434/v1
    Model       string  `yaml:"model"`
    APIKey      string  `yaml:"api_key"`     // vide en local
    Temperature float64 `yaml:"temperature"` // défaut 0.2
}
```

## API
- `Load(path string) (*Config, error)` — défauts appliqués si champs vides
  (BatchSize=25, Concurrency=2, Temperature=0.2).
- `Save(path string, c *Config) error`.
- `Validate(c *Config) error` — provider actif existe, BaseURL non vide, etc.
- `store.go` : `Store` enveloppe `*Config` derrière un `sync.RWMutex`
  (`Get`/`Update`/`Resolve(provider, model string)`).
- **Résolution** : overrides CLI (`--provider`/`--model`) priment sur le YAML.
- **Masquage** : tout chemin de lecture exposant la config remplace `APIKey` non
  vide par `"***"` (préparation phase 8 ; ne jamais renvoyer la clé en clair).

## Tests (`config_test.go`)
- Load/Save round-trip YAML (défauts appliqués).
- Accès concurrent `Get`/`Update` sous `go test -race`.
- Masquage : clé non vide → `***`, clé vide → reste vide.
- Résolution provider actif + override CLI.

## Gate + commit
`make check` vert → `✨ feat(config): store YAML thread-safe + résolution provider`
