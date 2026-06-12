package translate

import (
	"testing"

	"github.com/gabrielfareau/translai/internal/config"
)

func TestFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.ProviderConfig
		wantName string
		wantErr  bool
	}{
		{
			name: "openai_compat",
			cfg: config.ProviderConfig{
				Type:    "openai_compat",
				BaseURL: "http://localhost:11434/v1",
				Model:   "qwen2.5",
			},
			wantName: "openai_compat",
		},
		{
			name: "anthropic",
			cfg: config.ProviderConfig{
				Type:   "anthropic",
				Model:  "claude-3-5-haiku-20241022",
				APIKey: "sk-ant-xxx",
			},
			wantName: "anthropic",
		},
		{
			name: "gemini",
			cfg: config.ProviderConfig{
				Type:   "gemini",
				Model:  "gemini-1.5-flash",
				APIKey: "AIza-xxx",
			},
			wantName: "gemini",
		},
		{
			name:    "inconnu",
			cfg:     config.ProviderConfig{Type: "unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := FromConfig(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("attendu une erreur, aucune reçue")
				}
				return
			}
			if err != nil {
				t.Fatalf("FromConfig: %v", err)
			}
			if tr.Name() != tt.wantName {
				t.Errorf("Name() = %q, attendu %q", tr.Name(), tt.wantName)
			}
		})
	}
}
