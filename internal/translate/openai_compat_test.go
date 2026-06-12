package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// decodeReq lit le corps chatRequest reçu par le mock.
func decodeReq(t *testing.T, r *http.Request) chatRequest {
	t.Helper()
	var cr chatRequest
	if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
		t.Fatalf("décodage requête mock: %v", err)
	}
	return cr
}

// userContent renvoie le contenu du message user.
func userContent(req chatRequest) string {
	for _, m := range req.Messages {
		if m.Role == "user" {
			return m.Content
		}
	}
	return ""
}

// echoResponse fabrique une réponse [N] qui préfixe chaque texte par "FR:".
func echoResponse(user string) string {
	var b strings.Builder
	for _, m := range reIndexed.FindAllStringSubmatch(user, -1) {
		fmt.Fprintf(&b, "[%s] FR:%s\n", m[1], m[2])
	}
	return b.String()
}

func writeChat(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	resp := chatResponse{}
	resp.Choices = append(resp.Choices, struct {
		Message chatMessage `json:"message"`
	}{Message: chatMessage{Role: "assistant", Content: content}})
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("encode réponse: %v", err)
	}
}

func TestTranslateRequestShape(t *testing.T) {
	var gotPath, gotAuth, gotUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		req := decodeReq(t, r)
		gotUser = userContent(req)
		writeChat(t, w, echoResponse(gotUser))
	}))
	defer srv.Close()

	tr := NewOpenAICompat("ollama", srv.URL+"/v1", "qwen2.5", "secret", 0.2)
	out, err := tr.Translate(context.Background(), Request{
		SourceLang: "en", TargetLang: "fr",
		Texts: []string{"Hello", "World"},
	})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("auth = %q", gotAuth)
	}
	if !strings.Contains(gotUser, "[1] Hello") || !strings.Contains(gotUser, "[2] World") {
		t.Errorf("user prompt sans lignes indexées: %q", gotUser)
	}
	if len(out) != 2 || out[0] != "FR:Hello" || out[1] != "FR:World" {
		t.Errorf("out = %v", out)
	}
}

func TestTranslateNoAuthWhenKeyEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeChat(t, w, echoResponse(userContent(decodeReq(t, r))))
	}))
	defer srv.Close()

	tr := NewOpenAICompat("local", srv.URL+"/v1", "m", "", 0.0)
	if _, err := tr.Translate(context.Background(), Request{TargetLang: "fr", Texts: []string{"x"}}); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("clé vide ne doit pas envoyer Authorization, got %q", gotAuth)
	}
}

func TestTranslateMismatchErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = decodeReq(t, r)
		writeChat(t, w, "[1] seulement une ligne\n") // 2 demandées
	}))
	defer srv.Close()

	tr := NewOpenAICompat("m", srv.URL+"/v1", "m", "", 0.2)
	_, err := tr.Translate(context.Background(), Request{TargetLang: "fr", Texts: []string{"a", "b"}})
	if err == nil {
		t.Fatal("mismatch de lignes devrait renvoyer une erreur")
	}
}

func TestTranslateHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	tr := NewOpenAICompat("m", srv.URL+"/v1", "m", "", 0.2)
	if _, err := tr.Translate(context.Background(), Request{TargetLang: "fr", Texts: []string{"a"}}); err == nil {
		t.Fatal("statut 500 devrait renvoyer une erreur")
	}
}
