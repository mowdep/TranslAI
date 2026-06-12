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
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"
	geminiMaxTok  = 4096
)

// GeminiClient implémente Translator via l'API Google Gemini generateContent.
type GeminiClient struct {
	model       string
	apiKey      string
	temperature float64
	baseURL     string // injectable pour tests
	httpClient  *http.Client
}

// NewGeminiClient construit un client Gemini à partir d'une ProviderConfig.
func NewGeminiClient(cfg config.ProviderConfig) *GeminiClient {
	baseURL := geminiBaseURL
	if cfg.BaseURL != "" {
		baseURL = cfg.BaseURL
	}
	return &GeminiClient{
		model:       cfg.Model,
		apiKey:      cfg.APIKey,
		temperature: cfg.Temperature,
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *GeminiClient) Name() string { return "gemini" }

// types internes pour l'API Gemini generateContent

type geminiRequest struct {
	Contents         []geminiContent `json:"contents"`
	GenerationConfig geminiGenConfig `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Translate envoie le batch à l'API Gemini et retourne les traductions.
// Contrat : len(out) == len(req.Texts).
func (c *GeminiClient) Translate(ctx context.Context, req Request) ([]string, error) {
	if len(req.Texts) == 0 {
		return []string{}, nil
	}

	system, user := buildPrompt(req)
	fullPrompt := system + "\n\n" + user

	body, err := json.Marshal(geminiRequest{
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: fullPrompt}}},
		},
		GenerationConfig: geminiGenConfig{
			Temperature:     c.temperature,
			MaxOutputTokens: geminiMaxTok,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("translate(gemini): encodage requête: %w", err)
	}

	// La clé passe dans le header x-goog-api-key, jamais dans l'URL (évite la fuite dans *url.Error).
	url := fmt.Sprintf("%s/%s:generateContent", c.baseURL, c.model) //nolint:gosec
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("translate(gemini): construction requête: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("translate(gemini): appel API: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("translate(gemini): lecture réponse: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp geminiResponse
		if jsonErr := json.Unmarshal(data, &errResp); jsonErr == nil && errResp.Error != nil {
			return nil, fmt.Errorf("translate(gemini): statut %d: %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("translate(gemini): statut %d", resp.StatusCode)
	}

	var gr geminiResponse
	if err := json.Unmarshal(data, &gr); err != nil {
		return nil, fmt.Errorf("translate(gemini): décodage réponse: %w", err)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("translate(gemini): réponse sans candidat")
	}

	return parseIndexed(gr.Candidates[0].Content.Parts[0].Text, len(req.Texts))
}
