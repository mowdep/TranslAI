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

// writeGemini écrit une réponse Gemini generateContent valide.
func writeGemini(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	resp := geminiResponse{
		Candidates: []struct {
			Content geminiContent `json:"content"`
		}{
			{Content: geminiContent{Parts: []geminiPart{{Text: content}}}},
		},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("encode réponse gemini: %v", err)
	}
}

// echoGeminiResponse produit une réponse indexée à partir du prompt.
func echoGeminiResponse(prompt string) string {
	var b strings.Builder
	for _, m := range reIndexed.FindAllStringSubmatch(prompt, -1) {
		fmt.Fprintf(&b, "[%s] FR:%s\n", m[1], m[2])
	}
	return b.String()
}

func TestGeminiRequestShape(t *testing.T) {
	var gotPath, gotAPIKeyHeader string
	var gotBody geminiRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKeyHeader = r.Header.Get("x-goog-api-key")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("décodage corps: %v", err)
		}
		// extraire le texte du prompt pour construire la réponse echo
		prompt := ""
		if len(gotBody.Contents) > 0 && len(gotBody.Contents[0].Parts) > 0 {
			prompt = gotBody.Contents[0].Parts[0].Text
		}
		writeGemini(t, w, echoGeminiResponse(prompt))
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Type:        "gemini",
		Model:       "gemini-1.5-flash",
		APIKey:      "AIza-secret",
		Temperature: 0.2,
		BaseURL:     srv.URL, // rediriger vers le mock
	}
	client := NewGeminiClient(cfg)

	out, err := client.Translate(context.Background(), Request{
		SourceLang: "en",
		TargetLang: "fr",
		Texts:      []string{"Hello", "World"},
	})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	// Vérifier le path et la clé API dans le header (pas dans l'URL)
	if !strings.Contains(gotPath, "gemini-1.5-flash") {
		t.Errorf("path = %q, devrait contenir le modèle", gotPath)
	}
	if !strings.Contains(gotPath, "generateContent") {
		t.Errorf("path = %q, devrait contenir generateContent", gotPath)
	}
	if gotAPIKeyHeader != "AIza-secret" {
		t.Errorf("x-goog-api-key header = %q, attendu %q", gotAPIKeyHeader, "AIza-secret")
	}

	// Vérifier le body
	if len(gotBody.Contents) == 0 || len(gotBody.Contents[0].Parts) == 0 {
		t.Error("contents/parts vides dans la requête")
	}
	if gotBody.GenerationConfig.MaxOutputTokens != geminiMaxTok {
		t.Errorf("maxOutputTokens = %d", gotBody.GenerationConfig.MaxOutputTokens)
	}

	// Vérifier que le prompt contient les cues indexées
	prompt := gotBody.Contents[0].Parts[0].Text
	if !strings.Contains(prompt, "[1] Hello") || !strings.Contains(prompt, "[2] World") {
		t.Errorf("prompt sans cues indexées: %q", prompt)
	}

	// Vérifier la sortie
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, attendu 2", len(out))
	}
	if out[0] != "FR:Hello" || out[1] != "FR:World" {
		t.Errorf("out = %v", out)
	}
}

func TestGeminiMismatchErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeGemini(t, w, "[1] seulement une ligne\n") // 2 demandées
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Type:    "gemini",
		Model:   "gemini-1.5-flash",
		APIKey:  "k",
		BaseURL: srv.URL,
	}
	client := NewGeminiClient(cfg)
	_, err := client.Translate(context.Background(), Request{
		TargetLang: "fr",
		Texts:      []string{"a", "b"},
	})
	if err == nil {
		t.Fatal("mismatch de lignes devrait renvoyer une erreur")
	}
}

func TestGeminiHTTP4xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintln(w, `{"error":{"message":"API key not valid","code":400}}`)
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Type:    "gemini",
		Model:   "m",
		APIKey:  "bad",
		BaseURL: srv.URL,
	}
	client := NewGeminiClient(cfg)
	_, err := client.Translate(context.Background(), Request{
		TargetLang: "fr",
		Texts:      []string{"x"},
	})
	if err == nil {
		t.Fatal("statut 400 devrait renvoyer une erreur")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("erreur sans code statut: %v", err)
	}
}

func TestGeminiName(t *testing.T) {
	cfg := config.ProviderConfig{Type: "gemini", Model: "m", APIKey: "k"}
	client := NewGeminiClient(cfg)
	if client.Name() != "gemini" {
		t.Errorf("Name() = %q", client.Name())
	}
}
