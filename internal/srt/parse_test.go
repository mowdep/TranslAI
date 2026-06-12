package srt

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

var fixtures = []string{"en", "fr", "es"}

// fixtureBytes lit testdata/<name>.srt (relatif au package).
func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("..", "..", "testdata", name+".srt")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("lecture fixture %s: %v", p, err)
	}
	return b
}

// TestRoundTrip vérifie que Parse→Save produit une sortie équivalente à l'entrée
// pour chaque fixture : index, timestamps, tags et multi-ligne intacts.
func TestRoundTrip(t *testing.T) {
	for _, name := range fixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			in := fixtureBytes(t, name)

			doc, err := Parse(bytes.NewReader(in))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			var out bytes.Buffer
			if err := Save(&out, doc); err != nil {
				t.Fatalf("Save: %v", err)
			}

			// Sortie UTF-8, sans BOM.
			if bytes.HasPrefix(out.Bytes(), []byte{0xEF, 0xBB, 0xBF}) {
				t.Errorf("sortie préfixée d'un BOM UTF-8")
			}

			gotLines := normalize(out.String())
			wantLines := normalize(string(in))
			if !equalLines(gotLines, wantLines) {
				t.Errorf("round-trip différent\n--- entrée ---\n%s\n--- sortie ---\n%s",
					strings.Join(wantLines, "\n"), strings.Join(gotLines, "\n"))
			}
		})
	}
}

// TestRoundTripPreservesStructure vérifie explicitement les invariants sur les
// cues : index, timestamps, tag <i> et multi-ligne.
func TestRoundTripPreservesStructure(t *testing.T) {
	in := fixtureBytes(t, "en")
	doc, err := Parse(bytes.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := len(doc.Cues); got != 3 {
		t.Fatalf("nombre de cues = %d, attendu 3", got)
	}

	// Cue 1 : deux lignes.
	if got := len(doc.Cues[0].Lines); got != 2 {
		t.Errorf("cue 1: %d lignes, attendu 2", got)
	}
	if doc.Cues[0].Start != 1*time.Second {
		t.Errorf("cue 1 Start = %v, attendu 1s", doc.Cues[0].Start)
	}
	if doc.Cues[0].End != 3*time.Second {
		t.Errorf("cue 1 End = %v, attendu 3s", doc.Cues[0].End)
	}

	// Cue 2 : ligne en italique — le texte exposé ne contient pas le tag…
	if strings.Contains(doc.Cues[1].Lines[0], "<i>") {
		t.Errorf("le texte exposé ne doit pas contenir le tag <i>: %q", doc.Cues[1].Lines[0])
	}
	// …mais la resérialisation doit le restaurer.
	var out bytes.Buffer
	if err := Save(&out, doc); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.Contains(out.String(), "<i>This is an italic line.</i>") {
		t.Errorf("tag <i> non préservé en sortie:\n%s", out.String())
	}
}

// TestParseWindows1252 vérifie qu'une entrée Windows-1252 est convertie en UTF-8.
func TestParseWindows1252(t *testing.T) {
	// Texte avec caractères non-ASCII : é, à, ç.
	utf8SRT := "1\n00:00:01,000 --> 00:00:02,000\nÀ bientôt, ça va ?\n"

	enc, err := charmap.Windows1252.NewEncoder().String(utf8SRT)
	if err != nil {
		t.Fatalf("encodage Windows-1252: %v", err)
	}
	if utf8.ValidString(enc) {
		t.Fatalf("la fixture Windows-1252 est déjà de l'UTF-8 valide — test invalide")
	}

	doc, err := Parse(strings.NewReader(enc))
	if err != nil {
		t.Fatalf("Parse Windows-1252: %v", err)
	}

	var out bytes.Buffer
	if err := Save(&out, doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if !utf8.Valid(out.Bytes()) {
		t.Errorf("sortie non UTF-8 valide")
	}
	if !strings.Contains(out.String(), "À bientôt, ça va ?") {
		t.Errorf("texte accentué non préservé:\n%s", out.String())
	}
}

// TestTextSample vérifie un échantillon non vide, sans timestamps.
func TestTextSample(t *testing.T) {
	in := fixtureBytes(t, "fr")
	doc, err := Parse(bytes.NewReader(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	sample := TextSample(doc, 2)
	if strings.TrimSpace(sample) == "" {
		t.Fatalf("TextSample vide")
	}
	if strings.Contains(sample, "-->") || strings.Contains(sample, "00:00:") {
		t.Errorf("TextSample contient des timestamps: %q", sample)
	}
	if !strings.Contains(sample, "Bonjour") {
		t.Errorf("TextSample ne contient pas le texte attendu: %q", sample)
	}

	// maxCues <= 0 ou > len : prend tout.
	if all := TextSample(doc, 0); all == "" {
		t.Errorf("TextSample(0) vide")
	}
}

// --- helpers ---

// normalize découpe en lignes, retire le BOM et les espaces de fin, et supprime
// les lignes vides en tête/queue pour comparer entrée et sortie indépendamment
// du nombre de retours-ligne terminaux.
func normalize(s string) []string {
	s = strings.TrimPrefix(s, "\uFEFF")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	raw := strings.Split(s, "\n")
	lines := make([]string, 0, len(raw))
	for _, l := range raw {
		lines = append(lines, strings.TrimRight(l, " \t"))
	}
	// Trim leading/trailing empty lines.
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
