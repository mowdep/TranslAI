package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gabrielfareau/translai/internal/config"
)

const (
	anthropicEndpoint = "https://api.anthropic.com/v1/messages"
	anthropicVersion  = "2023-06-01"
	anthropicMaxTok   = 4096
)

// AnthropicClient implémente Translator via l'API Anthropic Messages.
type AnthropicClient struct {
	model       string
	apiKey      string
	temperature float64
	endpoint    string // injectable pour tests
	httpClient  *http.Client
}

// NewAnthropicClient construit un client Anthropic à partir d'une ProviderConfig.
func NewAnthropicClient(cfg config.ProviderConfig) *AnthropicClient {
	endpoint := anthropicEndpoint
	if cfg.BaseURL != "" {
		endpoint = cfg.BaseURL
	}
	return &AnthropicClient{
		model:       cfg.Model,
		apiKey:      cfg.APIKey,
		temperature: cfg.Temperature,
		endpoint:    endpoint,
		httpClient:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *AnthropicClient) Name() string { return "anthropic" }

// types internes pour l'API Anthropic Messages

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	System      string             `json:"system"`
	Messages    []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Translate envoie le batch à l'API Anthropic et retourne les traductions.
// Contrat : len(out) == len(req.Texts).
func (c *AnthropicClient) Translate(ctx context.Context, req Request) ([]string, error) {
	if len(req.Texts) == 0 {
		return []string{}, nil
	}

	system, user := buildPrompt(req)

	body, err := json.Marshal(anthropicRequest{
		Model:       c.model,
		MaxTokens:   anthropicMaxTok,
		Temperature: c.temperature,
		System:      system,
		Messages:    []anthropicMessage{{Role: "user", Content: user}},
	})
	if err != nil {
		return nil, fmt.Errorf("translate(anthropic): encodage requête: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body)) //nolint:gosec // endpoint vient de la config provider
	if err != nil {
		return nil, fmt.Errorf("translate(anthropic): construction requête: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("translate(anthropic): appel API: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("translate(anthropic): lecture réponse: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Essayer d'extraire le message d'erreur JSON
		var errResp anthropicResponse
		if jsonErr := json.Unmarshal(data, &errResp); jsonErr == nil && errResp.Error != nil {
			return nil, fmt.Errorf("translate(anthropic): statut %d: %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("translate(anthropic): statut %d: %s", resp.StatusCode, string(data))
	}

	var ar anthropicResponse
	if err := json.Unmarshal(data, &ar); err != nil {
		return nil, fmt.Errorf("translate(anthropic): décodage réponse: %w", err)
	}
	if len(ar.Content) == 0 {
		return nil, fmt.Errorf("translate(anthropic): réponse sans contenu")
	}

	return parseIndexed(ar.Content[0].Text, len(req.Texts))
}
