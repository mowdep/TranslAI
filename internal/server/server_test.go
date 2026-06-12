package server

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/gabrielfareau/translai/internal/config"
	"github.com/gabrielfareau/translai/internal/translate"
)

// ── Helpers ────────────────────────────────────────────────────────────────

// mockTranslator simule un Translator qui préfixe chaque texte.
type mockTranslator struct{ prefix string }

func (m *mockTranslator) Translate(_ context.Context, req translate.Request) ([]string, error) {
	out := make([]string, len(req.Texts))
	for i, t := range req.Texts {
		out[i] = m.prefix + t
	}
	return out, nil
}
func (m *mockTranslator) Name() string { return "mock" }

// newTestServer crée un serveur de test avec un provider openai_compat mocké.
// Le mockLLM est un httptest.Server qui répond au /v1/chat/completions.
func newTestServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()

	// LLM mock : renvoie chaque texte indexé préfixé par "FR:".
	llmMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		// Extraire les lignes [N] text du message user et les retourner préfixées.
		userContent := ""
		for _, m := range body.Messages {
			if m.Role == "user" {
				userContent = m.Content
				break
			}
		}

		var result strings.Builder
		for _, line := range strings.Split(userContent, "\n") {
			if strings.HasPrefix(line, "[") {
				idx := strings.Index(line, "]")
				if idx > 0 {
					num := line[1:idx]
					text := strings.TrimSpace(line[idx+1:])
					fmt.Fprintf(&result, "[%s] FR:%s\n", num, text)
				}
			}
		}

		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": result.String()}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(llmMock.Close)

	cfg := &config.Config{
		ActiveProvider: "test",
		DefaultTarget:  "fr",
		BatchSize:      25,
		Concurrency:    2,
		Providers: map[string]config.ProviderConfig{
			"test": {
				Type:        "openai_compat",
				BaseURL:     llmMock.URL + "/v1",
				Model:       "mock",
				Temperature: 0.2,
			},
		},
	}
	store := config.NewStore(cfg)
	srv := New(":0", store, "")
	ts := httptest.NewServer(srv.router)
	t.Cleanup(ts.Close)
	return ts, srv
}

// srtFixture retourne le contenu de testdata/en.srt.
func srtFixture(t *testing.T) []byte {
	t.Helper()
	// Le chemin est relatif au package de test; on remonte deux niveaux.
	data, err := os.ReadFile("../../testdata/en.srt")
	if err != nil {
		t.Fatalf("srtFixture: %v", err)
	}
	return data
}

// multipartBody construit un corps multipart/form-data avec un fichier SRT.
func multipartBody(t *testing.T, fieldName, fileName string, data []byte, extra map[string]string) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write file: %v", err)
	}
	for k, v := range extra {
		_ = w.WriteField(k, v)
	}
	w.Close()
	return &buf, w.FormDataContentType()
}

// ── Tests ──────────────────────────────────────────────────────────────────

// TestDetect vérifie que POST /api/detect retourne la langue détectée.
func TestDetect(t *testing.T) {
	ts, _ := newTestServer(t)
	srtData := srtFixture(t)
	body, ct := multipartBody(t, "file", "en.srt", srtData, nil)

	resp, err := http.Post(ts.URL+"/api/detect", ct, body)
	if err != nil {
		t.Fatalf("POST /api/detect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if result["detected_lang"] != "en" {
		t.Errorf("detected_lang = %q, want %q", result["detected_lang"], "en")
	}
}

// TestConvertAndStream vérifie POST /api/convert + GET /api/convert/stream.
func TestConvertAndStream(t *testing.T) {
	ts, _ := newTestServer(t)
	srtData := srtFixture(t)
	body, ct := multipartBody(t, "files", "en.srt", srtData, map[string]string{
		"target": "fr",
		"source": "en",
	})

	// POST /api/convert
	resp, err := http.Post(ts.URL+"/api/convert", ct, body)
	if err != nil {
		t.Fatalf("POST /api/convert: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("convert status %d: %s", resp.StatusCode, b)
	}

	var convertResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&convertResp); err != nil {
		t.Fatalf("decode convert resp: %v", err)
	}
	jobID := convertResp["job_id"]
	if jobID == "" {
		t.Fatal("job_id vide")
	}

	// GET /api/convert/stream avec timeout
	// On attend au plus 5s pour recevoir au moins un event progress/result/error.
	streamURL := ts.URL + "/api/convert/stream?job_id=" + jobID

	var progressSeen, resultSeen bool
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) && (!progressSeen || !resultSeen) {
		streamResp, err := http.Get(streamURL)
		if err != nil {
			t.Fatalf("GET stream: %v", err)
		}

		scanner := newSSEScanner(streamResp.Body)
		for scanner.Scan() {
			ev := scanner.Event()
			if ev.eventType == "progress" {
				progressSeen = true
			}
			if ev.eventType == "result" || ev.eventType == "error" {
				resultSeen = true
			}
			if progressSeen && resultSeen {
				break
			}
		}
		streamResp.Body.Close()

		if progressSeen && resultSeen {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !progressSeen {
		t.Error("aucun event 'progress' reçu")
	}
	if !resultSeen {
		t.Error("aucun event 'result' ou 'error' reçu")
	}
}

// TestDownload vérifie GET /api/download retourne Content-Type application/x-subrip.
func TestDownload(t *testing.T) {
	ts, srv := newTestServer(t)

	// Injecter un job directement dans le store.
	jobID := "test-download"
	job := &JobResult{ID: jobID}
	job.addFile(FileResult{Name: "test.srt", SRTOut: []byte("1\n00:00:01,000 --> 00:00:02,000\nBonjour\n\n")})
	srv.jobs.Set(jobID, job)

	resp, err := http.Get(ts.URL + "/api/download?job_id=" + jobID + "&file=test.srt")
	if err != nil {
		t.Fatalf("GET /api/download: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "application/x-subrip" {
		t.Errorf("Content-Type = %q, want application/x-subrip", ct)
	}
}

// TestDownloadAll vérifie GET /api/download/all retourne un .zip non vide.
func TestDownloadAll(t *testing.T) {
	ts, srv := newTestServer(t)

	jobID := "test-zip"
	job := &JobResult{ID: jobID}
	job.addFile(FileResult{Name: "a.srt", SRTOut: []byte("1\n00:00:01,000 --> 00:00:02,000\nBonjour\n\n")})
	job.addFile(FileResult{Name: "b.srt", SRTOut: []byte("1\n00:00:01,000 --> 00:00:02,000\nMonde\n\n")})
	srv.jobs.Set(jobID, job)

	resp, err := http.Get(ts.URL + "/api/download/all?job_id=" + jobID)
	if err != nil {
		t.Fatalf("GET /api/download/all: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}

	data, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip invalide: %v", err)
	}
	if len(zr.File) == 0 {
		t.Error("archive ZIP vide")
	}
}

// TestGetConfig vérifie GET /api/config masque bien les APIKey.
func TestGetConfig(t *testing.T) {
	// Config avec une clé API non vide.
	cfg := &config.Config{
		ActiveProvider: "ollama",
		DefaultTarget:  "fr",
		BatchSize:      25,
		Concurrency:    2,
		Providers: map[string]config.ProviderConfig{
			"ollama": {BaseURL: "http://localhost:11434/v1", Model: "llama3", APIKey: "secret123"},
		},
	}
	store := config.NewStore(cfg)
	srv := New(":0", store, "")
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/config")
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var result config.Config
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p, ok := result.Providers["ollama"]; ok {
		// "secret123" → "sec***123" (3 premiers + *** + 3 derniers)
		if p.APIKey != "sec***123" {
			t.Errorf("APIKey masquée attendue %q, got %q", "sec***123", p.APIKey)
		}
	} else {
		t.Error("provider 'ollama' absent de la réponse")
	}
}

// TestPostConfig vérifie POST /api/config met à jour la config.
func TestPostConfig(t *testing.T) {
	cfg := &config.Config{
		ActiveProvider: "test",
		DefaultTarget:  "fr",
		BatchSize:      25,
		Concurrency:    2,
		Providers: map[string]config.ProviderConfig{
			"test": {BaseURL: "http://localhost:11434/v1", Model: "llama3"},
		},
	}
	store := config.NewStore(cfg)
	srv := New(":0", store, "") // cfgPath vide → pas de sauvegarde disque
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	newCfg := config.Config{
		ActiveProvider: "test",
		DefaultTarget:  "en",
		BatchSize:      10,
		Concurrency:    4,
		Providers: map[string]config.ProviderConfig{
			"test": {BaseURL: "http://localhost:11434/v1", Model: "llama3.2"},
		},
	}
	body, _ := json.Marshal(newCfg)
	resp, err := http.Post(ts.URL+"/api/config", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}

	// Vérifier que la config a bien été mise à jour.
	got := store.Get()
	if got.DefaultTarget != "en" {
		t.Errorf("DefaultTarget = %q, want en", got.DefaultTarget)
	}
	if got.BatchSize != 10 {
		t.Errorf("BatchSize = %d, want 10", got.BatchSize)
	}
}

// TestHTMLConvertPage vérifie que GET / contient #drop-zone (goquery).
func TestHTMLConvertPage(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("goquery parse: %v", err)
	}
	if doc.Find("#drop-zone").Length() == 0 {
		t.Error("page / ne contient pas #drop-zone")
	}
}

// TestHTMLAdminPage vérifie que GET /admin contient un formulaire provider.
func TestHTMLAdminPage(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/admin")
	if err != nil {
		t.Fatalf("GET /admin: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("goquery parse: %v", err)
	}
	if doc.Find("#config-form").Length() == 0 {
		t.Error("page /admin ne contient pas #config-form")
	}
	if doc.Find(".provider-card").Length() == 0 {
		t.Error("page /admin ne contient pas de .provider-card")
	}
}

// ── SSE Scanner ───────────────────────────────────────────────────────────

type sseEvent struct {
	eventType string
	data      string
}

type sseScanner struct {
	sc  *bufio.Scanner
	cur sseEvent
}

func newSSEScanner(r io.Reader) *sseScanner {
	return &sseScanner{sc: bufio.NewScanner(r)}
}

func (s *sseScanner) Scan() bool {
	var evType, evData string
	for s.sc.Scan() {
		line := strings.TrimRight(s.sc.Text(), "\r")
		if strings.HasPrefix(line, "event: ") {
			evType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			evData = strings.TrimPrefix(line, "data: ")
		} else if line == "" && evType != "" {
			s.cur = sseEvent{eventType: evType, data: evData}
			return true
		}
	}
	return false
}

func (s *sseScanner) Event() sseEvent {
	return s.cur
}
