// Package config gère la configuration YAML de translai.
// Chargement, sauvegarde, validation et application des valeurs par défaut.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config est la structure principale de configuration.
type Config struct {
	ActiveProvider string                    `yaml:"active_provider"` // ollama|llamacpp|openai
	DefaultTarget  string                    `yaml:"default_target"`  // ex: "fr"
	Providers      map[string]ProviderConfig `yaml:"providers"`
	BatchSize      int                       `yaml:"batch_size"`  // défaut 25
	Concurrency    int                       `yaml:"concurrency"` // défaut 2
}

// ProviderConfig contient les paramètres d'un provider LLM.
type ProviderConfig struct {
	Type        string  `yaml:"type"`        // openai_compat (anthropic/gemini post-v0)
	BaseURL     string  `yaml:"base_url"`    // ex: http://localhost:11434/v1
	Model       string  `yaml:"model"`
	APIKey      string  `yaml:"api_key"`     // vide en local
	Temperature float64 `yaml:"temperature"` // défaut 0.2
}

// defaults applique les valeurs par défaut sur une Config.
func defaults(c *Config) {
	if c.BatchSize == 0 {
		c.BatchSize = 25
	}
	if c.Concurrency == 0 {
		c.Concurrency = 2
	}
	for name, p := range c.Providers {
		if p.Temperature == 0 {
			p.Temperature = 0.2
			c.Providers[name] = p
		}
	}
}

// Load charge la configuration depuis le fichier YAML path.
// Si le fichier est absent, renvoie une Config avec les défauts.
// Les clés API ne sont jamais masquées ici (c'est le Store qui masque à la lecture).
func Load(path string) (*Config, error) {
	c := &Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			defaults(c)
			return c, nil
		}
		return nil, fmt.Errorf("config: lecture %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("config: parse YAML %s: %w", path, err)
	}
	defaults(c)
	return c, nil
}

// Save sérialise la Config en YAML et l'écrit dans path.
func Save(path string, c *Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: sérialisation YAML: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("config: écriture %s: %w", path, err)
	}
	return nil
}

// Validate vérifie la cohérence de la Config.
// Retourne une erreur si le provider actif est absent, ou si BaseURL est vide.
func Validate(c *Config) error {
	if c.ActiveProvider == "" {
		return fmt.Errorf("config: active_provider vide")
	}
	p, ok := c.Providers[c.ActiveProvider]
	if !ok {
		return fmt.Errorf("config: provider %q absent de la section providers", c.ActiveProvider)
	}
	if p.BaseURL == "" {
		return fmt.Errorf("config: provider %q: base_url vide", c.ActiveProvider)
	}
	return nil
}
