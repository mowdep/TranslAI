package core

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/gabrielfareau/translai/internal/srt"
	"github.com/gabrielfareau/translai/internal/translate"
)

// mockTranslator implémente translate.Translator pour les tests.
type mockTranslator struct {
	transform  func(string) string
	failBatch  bool       // erreur si len(Texts)>1 → force le fallback cue-par-cue
	gotContext [][]string // contexte reçu à chaque appel
}

func (m *mockTranslator) Name() string { return "mock" }

func (m *mockTranslator) Translate(_ context.Context, req translate.Request) ([]string, error) {
	m.gotContext = append(m.gotContext, append([]string(nil), req.Context...))
	if m.failBatch && len(req.Texts) > 1 {
		return nil, errors.New("mock: batch refusé")
	}
	out := make([]string, len(req.Texts))
	for i, t := range req.Texts {
		out[i] = m.transform(t)
	}
	return out, nil
}

func loadDoc(t *testing.T, path string) *srt.Document {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	doc, err := srt.Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

type cueMeta struct {
	index      int
	start, end time.Duration
	nLines     int
}

func snapshot(doc *srt.Document) []cueMeta {
	m := make([]cueMeta, len(doc.Cues))
	for i, c := range doc.Cues {
		m[i] = cueMeta{c.Index, c.Start, c.End, len(c.Lines)}
	}
	return m
}

func TestPipelinePreservesStructure(t *testing.T) {
	doc := loadDoc(t, "../../testdata/en.srt")
	before := snapshot(doc)

	tr := &mockTranslator{transform: func(s string) string { return "FR:" + s }}
	err := Translate(context.Background(), doc, Options{Source: "en", Target: "fr", BatchSize: 2, Translator: tr}, nil)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	for i, c := range doc.Cues {
		if c.Index != before[i].index || c.Start != before[i].start || c.End != before[i].end {
			t.Errorf("cue %d: index/timestamps modifiés", i)
		}
		if len(c.Lines) != before[i].nLines {
			t.Errorf("cue %d: nb lignes %d != %d (origine)", i, len(c.Lines), before[i].nLines)
		}
		if len(c.Lines) == 0 || len(c.Lines[0]) < 3 || c.Lines[0][:3] != "FR:" {
			t.Errorf("cue %d: texte non traduit: %q", i, c.Lines)
		}
	}

	// La sortie doit rester un SRT valide.
	if err := srt.Save(io.Discard, doc); err != nil {
		t.Fatalf("Save après traduction: %v", err)
	}
}

func TestPipelineFallbackCueByCue(t *testing.T) {
	doc := loadDoc(t, "../../testdata/en.srt")
	tr := &mockTranslator{transform: func(s string) string { return "X" + s }, failBatch: true}

	// BatchSize 25 → un seul batch de 3 cues, refusé 2× → fallback cue-par-cue.
	if err := Translate(context.Background(), doc, Options{Source: "en", Target: "fr", BatchSize: 25, Translator: tr}, nil); err != nil {
		t.Fatalf("Translate (fallback): %v", err)
	}
	for i, c := range doc.Cues {
		if len(c.Lines) == 0 || c.Lines[0][:1] != "X" {
			t.Errorf("cue %d non traduite via fallback: %q", i, c.Lines)
		}
	}
}

func TestPipelinePassesContext(t *testing.T) {
	doc := loadDoc(t, "../../testdata/en.srt") // 3 cues
	tr := &mockTranslator{transform: func(s string) string { return "T:" + s }}

	// BatchSize 1 → 3 batches ; le 2e doit recevoir la 1re cue traduite en contexte.
	if err := Translate(context.Background(), doc, Options{Source: "en", Target: "fr", BatchSize: 1, Translator: tr}, nil); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(tr.gotContext) < 2 {
		t.Fatalf("attendu ≥2 appels, got %d", len(tr.gotContext))
	}
	if len(tr.gotContext[0]) != 0 {
		t.Errorf("1er batch: contexte attendu vide, got %v", tr.gotContext[0])
	}
	if len(tr.gotContext[1]) != 1 || tr.gotContext[1][0][:2] != "T:" {
		t.Errorf("2e batch: contexte attendu = 1 cue traduite, got %v", tr.gotContext[1])
	}
}
