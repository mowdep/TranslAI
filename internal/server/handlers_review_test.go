package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/gabrielfareau/translai/internal/config"
)

// newTestServerWithReview cree un serveur de test avec des cues pre-remplies.
func newTestServerWithReview(t *testing.T) (*httptest.Server, *Server, string, string) {
	t.Helper()

	// LLM mock simple.
	llmMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "[1] Traduction test\n"}},
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

	// Injecter un job avec des cues.
	jobID := "review-test-job"
	fileID := "test.srt"
	job := srv.reviewStore.CreateReview(jobID)

	cues := []*CueState{
		{
			Index:       1,
			SourceLines: []string{"Hello world"},
			TargetLines: []string{"Bonjour monde"},
		},
		{
			Index:       2,
			SourceLines: []string{"How are you?"},
			TargetLines: []string{"How are you?"}, // echo
		},
		{
			Index:       3,
			SourceLines: []string{"Goodbye"},
			TargetLines: []string{""},  // empty
		},
	}
	for _, c := range cues {
		c.Flags = computeFlags(c)
	}
	job.mu.Lock()
	job.files[fileID] = &FileState{Name: fileID, Cues: cues}
	job.mu.Unlock()

	return ts, srv, jobID, fileID
}

// TestHandlerGetCues verifie GET /api/review/cues retourne les cues avec flags.
func TestHandlerGetCues(t *testing.T) {
	ts, _, jobID, fileID := newTestServerWithReview(t)

	resp, err := http.Get(ts.URL + "/api/review/cues?job=" + jobID + "&file=" + fileID)
	if err != nil {
		t.Fatalf("GET /api/review/cues: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}

	var cues []*CueState
	if err := json.NewDecoder(resp.Body).Decode(&cues); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if len(cues) != 3 {
		t.Fatalf("len(cues) = %d, want 3", len(cues))
	}

	// Cue 2 doit avoir FlagEcho.
	if !hasFlag(cues[1].Flags, FlagEcho) {
		t.Errorf("cue 2: FlagEcho attendu, flags = %v", cues[1].Flags)
	}
	// Cue 3 doit avoir FlagEmpty.
	if !hasFlag(cues[2].Flags, FlagEmpty) {
		t.Errorf("cue 3: FlagEmpty attendu, flags = %v", cues[2].Flags)
	}
}

// TestHandlerGetCuesMissingParams verifie que les params manquants retournent 400.
func TestHandlerGetCuesMissingParams(t *testing.T) {
	ts, _, _, _ := newTestServerWithReview(t)

	resp, err := http.Get(ts.URL + "/api/review/cues")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// TestHandlerPatchCue verifie PATCH /api/review/cue met a jour l'etat in-memory.
func TestHandlerPatchCue(t *testing.T) {
	ts, srv, jobID, fileID := newTestServerWithReview(t)

	body := PatchCueRequest{
		Job:   jobID,
		File:  fileID,
		Index: 1,
		Lines: []string{"Salut monde"},
	}
	data, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/review/cue", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/review/cue: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}

	var updated CueState
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if len(updated.TargetLines) == 0 || updated.TargetLines[0] != "Salut monde" {
		t.Errorf("TargetLines = %v, want [Salut monde]", updated.TargetLines)
	}

	// Verifier que l'etat in-memory est bien mis a jour.
	cues, _ := srv.reviewStore.GetCues(jobID, fileID)
	if cues[0].TargetLines[0] != "Salut monde" {
		t.Errorf("etat in-memory: TargetLines = %v, want [Salut monde]", cues[0].TargetLines)
	}
}

// TestHandlerPatchCueNotFound verifie que PATCH retourne 404 pour job/cue inexistants.
func TestHandlerPatchCueNotFound(t *testing.T) {
	ts, _, _, _ := newTestServerWithReview(t)

	body := PatchCueRequest{
		Job:   "inexistant",
		File:  "test.srt",
		Index: 1,
		Lines: []string{"x"},
	}
	data, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/review/cue", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// TestHandlerRetranslate verifie POST /api/review/retranslate avec un Translator mock.
func TestHandlerRetranslate(t *testing.T) {
	// Creer un LLM mock qui retourne une traduction previsible.
	llmMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "[1] Nouvelle traduction\n"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer llmMock.Close()

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
	defer ts.Close()

	// Injecter un job.
	jobID := "retranslate-job"
	fileID := "sub.srt"
	job := srv.reviewStore.CreateReview(jobID)
	cues := []*CueState{
		{Index: 1, SourceLines: []string{"Hello"}, TargetLines: []string{"Old translation"}, durationSecs: 2.0},
	}
	for _, c := range cues {
		c.Flags = computeFlags(c)
	}
	job.mu.Lock()
	job.files[fileID] = &FileState{Name: fileID, Cues: cues}
	job.mu.Unlock()

	body := RetranslateRequest{Job: jobID, File: fileID, Index: 1}
	data, _ := json.Marshal(body)

	resp, err := http.Post(ts.URL+"/api/review/retranslate", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST /api/review/retranslate: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}

	var updated CueState
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	// La traduction a ete mise a jour (pas vide).
	if len(updated.TargetLines) == 0 {
		t.Error("TargetLines vide apres retranslate")
	}
}

// TestHTMLReviewPage verifie que GET /review contient la review-table et des tr.flagged.
func TestHTMLReviewPage(t *testing.T) {
	ts, _, jobID, fileID := newTestServerWithReview(t)

	resp, err := http.Get(ts.URL + "/review?job=" + jobID + "&file=" + fileID)
	if err != nil {
		t.Fatalf("GET /review: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		t.Fatalf("goquery parse: %v", err)
	}

	if doc.Find("table.review-table").Length() == 0 {
		t.Error("page /review ne contient pas table.review-table")
	}

	// Il doit y avoir des tr.flagged (cues 2 et 3 sont suspectes).
	flagged := doc.Find("tr.flagged")
	if flagged.Length() == 0 {
		t.Error("page /review ne contient pas de tr.flagged")
	}
}

// TestHTMLReviewPageNoParams verifie que GET /review sans params affiche la page vide.
func TestHTMLReviewPageNoParams(t *testing.T) {
	ts, _, _, _ := newTestServerWithReview(t)

	resp, err := http.Get(ts.URL + "/review")
	if err != nil {
		t.Fatalf("GET /review: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

