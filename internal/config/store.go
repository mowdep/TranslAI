package config

import (
	"sync"
)

// Store enveloppe un *Config derrière un sync.RWMutex pour un accès thread-safe.
// La méthode Get masque les clés API non vides par "***" afin de ne jamais
// exposer de secrets en dehors du package (invariant CLAUDE.md).
type Store struct {
	mu  sync.RWMutex
	cfg *Config
}

// NewStore crée un Store à partir d'une Config existante.
func NewStore(c *Config) *Store {
	return &Store{cfg: c}
}

// Get retourne une copie superficielle de la Config avec les clés API masquées.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return masked(s.cfg)
}

// Update remplace la Config courante par la valeur fournie (copie).
func (s *Store) Update(c Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := c
	s.cfg = &cp
}

// Resolve retourne le ProviderConfig actif en appliquant les overrides CLI.
// providerOverride et modelOverride peuvent être vides (pas d'override).
// La copie retournée a sa clé API intacte (usage interne pipeline uniquement).
func (s *Store) Resolve(providerOverride, modelOverride string) (string, ProviderConfig) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	providerName := s.cfg.ActiveProvider
	if providerOverride != "" {
		providerName = providerOverride
	}

	p := s.cfg.Providers[providerName]
	if modelOverride != "" {
		p.Model = modelOverride
	}
	return providerName, p
}

// masked retourne une copie de la Config avec les APIKey non vides remplacées par "***".
func masked(c *Config) Config {
	cp := Config{
		ActiveProvider: c.ActiveProvider,
		DefaultTarget:  c.DefaultTarget,
		BatchSize:      c.BatchSize,
		Concurrency:    c.Concurrency,
	}
	if len(c.Providers) > 0 {
		cp.Providers = make(map[string]ProviderConfig, len(c.Providers))
		for k, p := range c.Providers {
			if p.APIKey != "" {
				p.APIKey = "***"
			}
			cp.Providers[k] = p
		}
	}
	return cp
}
