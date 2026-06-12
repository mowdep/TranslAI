package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompat parle l'API OpenAI-compatible /v1/chat/completions, ce qui couvre
// Ollama, llama.cpp et OpenAI selon BaseURL/Model/APIKey.
type OpenAICompat struct {
	name        string
	baseURL     string
	model       string
	apiKey      string
	temperature float64
	client      *http.Client
}

// NewOpenAICompat construit un provider. baseURL inclut le préfixe API
// (ex: http://localhost:11434/v1).
func NewOpenAICompat(name, baseURL, model, apiKey string, temperature float64) *OpenAICompat {
	return &OpenAICompat{
		name:        name,
		baseURL:     strings.TrimRight(baseURL, "/"),
		model:       model,
		apiKey:      apiKey,
		temperature: temperature,
		client:      &http.Client{Timeout: 5 * time.Minute},
	}
}

func (o *OpenAICompat) Name() string { return o.name }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Translate envoie le batch et renvoie les traductions ordonnées.
// Contrat : len(out) == len(req.Texts), sinon erreur (gérée par le pipeline).
func (o *OpenAICompat) Translate(ctx context.Context, req Request) ([]string, error) {
	if len(req.Texts) == 0 {
		return []string{}, nil
	}

	system, user := buildPrompt(req)
	body, err := json.Marshal(chatRequest{
		Model: o.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: o.temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("translate: encodage requête: %w", err)
	}

	url := o.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body)) //nolint:gosec // url vient de la config provider
	if err != nil {
		return nil, fmt.Errorf("translate: construction requête: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("translate: appel %s: %w", o.baseURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("translate: lecture réponse: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("translate: statut %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return nil, fmt.Errorf("translate: décodage réponse: %w", err)
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("translate: réponse sans choix")
	}

	return parseIndexed(cr.Choices[0].Message.Content, len(req.Texts))
}
