package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gabrielfareau/translai/internal/srt"
)

// execRoot exécute la commande racine avec args et capture stdout.
func execRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestRootVersion(t *testing.T) {
	out, err := execRoot(t, "--version")
	if err != nil {
		t.Fatalf("--version a échoué: %v", err)
	}
	if !strings.Contains(out, "translai version") {
		t.Fatalf("sortie version inattendue: %q", out)
	}
}

func TestTranslateHelp(t *testing.T) {
	out, err := execRoot(t, "translate", "--help")
	if err != nil {
		t.Fatalf("translate --help a échoué: %v", err)
	}
	for _, flag := range []string{"--input", "--target", "--source", "--model", "--base-url"} {
		if !strings.Contains(out, flag) {
			t.Errorf("flag %q absent du help translate", flag)
		}
	}
}

// mockLLM renvoie un serveur qui échoue les lignes [N] préfixées "FR:".
func mockLLM(t *testing.T) *httptest.Server {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*\[(\d+)\]\s?(.*)$`)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role, Content string
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("décodage requête mock: %v", err)
		}
		var user string
		for _, m := range req.Messages {
			if m.Role == "user" {
				user = m.Content
			}
		}
		var b strings.Builder
		for _, m := range re.FindAllStringSubmatch(user, -1) {
			b.WriteString("[" + m[1] + "] FR:" + m[2] + "\n")
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": b.String()}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestTranslateFileEndToEnd(t *testing.T) {
	srv := mockLLM(t)
	defer srv.Close()

	// Référence : index/timestamps d'origine.
	origDoc := parseFixture(t, "../../testdata/en.srt")

	dir := t.TempDir()
	in := filepath.Join(dir, "en.srt")
	copyFile(t, "../../testdata/en.srt", in)
	out := filepath.Join(dir, "en.fr.srt")

	f := translateFlags{
		input:       in,
		output:      out,
		source:      "en",
		target:      "fr",
		model:       "mock",
		baseURL:     srv.URL + "/v1",
		temperature: 0.2,
		batchSize:   2,
	}
	if err := runTranslate(context.Background(), f); err != nil {
		t.Fatalf("runTranslate: %v", err)
	}

	got := parseFixture(t, out)
	if len(got.Cues) != len(origDoc.Cues) {
		t.Fatalf("nb cues %d != %d", len(got.Cues), len(origDoc.Cues))
	}
	for i := range got.Cues {
		o, g := origDoc.Cues[i], got.Cues[i]
		if g.Index != o.Index || g.Start != o.Start || g.End != o.End {
			t.Errorf("cue %d: index/timestamps modifiés", i)
		}
		if len(g.Lines) == 0 || !strings.HasPrefix(g.Lines[0], "FR:") {
			t.Errorf("cue %d: texte non traduit: %q", i, g.Lines)
		}
	}
}

func TestRunTranslateRequiresModel(t *testing.T) {
	err := runTranslate(context.Background(), translateFlags{input: "x.srt", target: "fr"})
	if err == nil || !strings.Contains(err.Error(), "--model") {
		t.Fatalf("attendu erreur --model requis, got %v", err)
	}
}

func parseFixture(t *testing.T, path string) *srt.Document {
	t.Helper()
	fr, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer fr.Close()
	doc, err := srt.Parse(fr)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}
