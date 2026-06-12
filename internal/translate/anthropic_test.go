package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gabrielfareau/translai/internal/config"
)

// writeAnthropic écrit une réponse Anthropic Messages valide.
func writeAnthropic(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	resp := anthropicResponse{
		Content: []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{
			{Type: "text", Text: content},
		},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("encode réponse anthropic: %v", err)
	}
}

// echoAnthropicResponse produit une réponse indexée à partir du prompt user.
func echoAnthropicResponse(prompt string) string {
	var b strings.Builder
	for _, m := range reIndexed.FindAllStringSubmatch(prompt, -1) {
		fmt.Fprintf(&b, "[%s] FR:%s\n", m[1], m[2])
	}
	return b.String()
}

func TestAnthropicRequestShape(t *testing.T) {
	var gotKey, gotVersion, gotContentType string
	var gotBody anthropicRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("décodage corps: %v", err)
		}
		// extraire le prompt user pour construire la réponse echo
		userContent := ""
		for _, msg := range gotBody.Messages {
			if msg.Role == "user" {
				userContent = msg.Content
			}
		}
		writeAnthropic(t, w, echoAnthropicResponse(userContent))
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Type:        "anthropic",
		Model:       "claude-3-5-haiku-20241022",
		APIKey:      "sk-ant-secret",
		Temperature: 0.2,
		BaseURL:     srv.URL, // rediriger vers le mock
	}
	client := NewAnthropicClient(cfg)

	out, err := client.Translate(context.Background(), Request{
		SourceLang: "en",
		TargetLang: "fr",
		Texts:      []string{"Hello", "World"},
	})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	// Vérifier les headers
	if gotKey != "sk-ant-secret" {
		t.Errorf("x-api-key = %q, attendu %q", gotKey, "sk-ant-secret")
	}
	if gotVersion != anthropicVersion {
		t.Errorf("anthropic-version = %q, attendu %q", gotVersion, anthropicVersion)
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("Content-Type = %q", gotContentType)
	}

	// Vérifier le body
	if gotBody.Model != "claude-3-5-haiku-20241022" {
		t.Errorf("model = %q", gotBody.Model)
	}
	if gotBody.MaxTokens != anthropicMaxTok {
		t.Errorf("max_tokens = %d", gotBody.MaxTokens)
	}
	if gotBody.System == "" {
		t.Error("system prompt vide")
	}
	if len(gotBody.Messages) == 0 || gotBody.Messages[0].Role != "user" {
		t.Errorf("messages mal formés: %+v", gotBody.Messages)
	}

	// Vérifier la sortie
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, attendu 2", len(out))
	}
	if out[0] != "FR:Hello" || out[1] != "FR:World" {
		t.Errorf("out = %v", out)
	}
}

func TestAnthropicMismatchErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAnthropic(t, w, "[1] seulement une ligne\n") // 2 demandées
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Type:    "anthropic",
		Model:   "claude-3-5-haiku-20241022",
		APIKey:  "k",
		BaseURL: srv.URL,
	}
	client := NewAnthropicClient(cfg)
	_, err := client.Translate(context.Background(), Request{
		TargetLang: "fr",
		Texts:      []string{"a", "b"},
	})
	if err == nil {
		t.Fatal("mismatch de lignes devrait renvoyer une erreur")
	}
}

func TestAnthropicHTTP4xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintln(w, `{"error":{"message":"invalid api key","type":"authentication_error"}}`)
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Type:    "anthropic",
		Model:   "m",
		APIKey:  "bad",
		BaseURL: srv.URL,
	}
	client := NewAnthropicClient(cfg)
	_, err := client.Translate(context.Background(), Request{
		TargetLang: "fr",
		Texts:      []string{"x"},
	})
	if err == nil {
		t.Fatal("statut 401 devrait renvoyer une erreur")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("erreur sans code statut: %v", err)
	}
}

func TestAnthropicName(t *testing.T) {
	cfg := config.ProviderConfig{Type: "anthropic", Model: "m", APIKey: "k"}
	client := NewAnthropicClient(cfg)
	if client.Name() != "anthropic" {
		t.Errorf("Name() = %q", client.Name())
	}
}
