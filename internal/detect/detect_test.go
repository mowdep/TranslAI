package detect

import (
	"os"
	"testing"

	"github.com/gabrielfareau/translai/internal/srt"
)

// fixtureSample parse une fixture SRT et en extrait un échantillon de texte.
func fixtureSample(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	doc, err := srt.Parse(f)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return srt.TextSample(doc, 5)
}

func TestDetectFixtures(t *testing.T) {
	cases := map[string]string{
		"../../testdata/en.srt": "en",
		"../../testdata/fr.srt": "fr",
		"../../testdata/es.srt": "es",
	}
	for path, want := range cases {
		got, err := Detect(fixtureSample(t, path))
		if err != nil {
			t.Errorf("Detect(%s): %v", path, err)
			continue
		}
		if got != want {
			t.Errorf("Detect(%s) = %q, attendu %q", path, got, want)
		}
	}
}

func TestDetectEmpty(t *testing.T) {
	if _, err := Detect("   \n\t"); err == nil {
		t.Fatal("Detect sur échantillon vide devrait renvoyer une erreur")
	}
}
