package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gabrielfareau/translai/internal/detect"
	"github.com/gabrielfareau/translai/internal/srt"
	"github.com/gabrielfareau/translai/internal/translate"
)

const (
	defaultBatchSize = 25
	contextWindow    = 3 // cues précédentes traduites passées en référence
	sampleCues       = 5 // cues échantillonnées pour la détection auto
)

// Options paramètre une traduction.
type Options struct {
	Source     string // code ISO ou "auto"/"" → détection
	Target     string // code ISO (requis)
	BatchSize  int    // défaut 25
	Translator translate.Translator
}

// Translate traduit doc en place : seul le texte des cues change, index et
// timestamps sont préservés. ev (peut être nil) reçoit la progression.
func Translate(ctx context.Context, doc *srt.Document, opts Options, ev chan<- Event) error {
	if doc == nil {
		return errors.New("core: document nil")
	}
	if opts.Translator == nil {
		return errors.New("core: translator nil")
	}
	if opts.Target == "" {
		return errors.New("core: langue cible requise")
	}

	source := opts.Source
	if source == "" || source == "auto" {
		s, err := detect.Detect(srt.TextSample(doc, sampleCues))
		if err != nil {
			return fmt.Errorf("core: détection langue source: %w", err)
		}
		source = s
	}

	size := opts.BatchSize
	if size < 1 {
		size = defaultBatchSize
	}

	// Encode chaque cue en un texte unique (retours-ligne internes → marqueur).
	encoded := make([]string, len(doc.Cues))
	for i, c := range doc.Cues {
		encoded[i] = strings.Join(c.Lines, lineSep)
	}
	translated := make([]string, len(doc.Cues))

	total := len(doc.Cues)
	done := 0
	emit := func(stage string, err error) {
		if ev != nil {
			ev <- Event{Stage: stage, Done: done, Total: total, Err: err}
		}
	}
	emit(StageStart, nil)

	for _, r := range chunkRanges(len(encoded), size) {
		if err := ctx.Err(); err != nil {
			return err
		}
		start, end := r[0], r[1]
		ctxStart := start - contextWindow
		if ctxStart < 0 {
			ctxStart = 0
		}
		out, err := translateBatch(ctx, opts.Translator, source, opts.Target, encoded[start:end], translated[ctxStart:start])
		if err != nil {
			emit(StageError, err)
			return err
		}
		copy(translated[start:end], out)
		done = end
		emit(StageBatch, nil)
	}

	// Réassemblage : re-split sur le marqueur, réconcilié au nb de lignes d'origine.
	for i := range doc.Cues {
		doc.Cues[i].Lines = reconcile(strings.Split(translated[i], lineSep), len(doc.Cues[i].Lines))
	}
	emit(StageDone, nil)
	return nil
}

// translateBatch applique le contrat len(out)==len(in) : un essai, un retry, puis
// fallback cue-par-cue (lent mais robuste sur petits modèles).
func translateBatch(ctx context.Context, tr translate.Translator, source, target string, texts, ctxLines []string) ([]string, error) {
	req := translate.Request{SourceLang: source, TargetLang: target, Texts: texts, Context: ctxLines}

	if out, err := tr.Translate(ctx, req); err == nil && len(out) == len(texts) {
		return out, nil
	}
	if out, err := tr.Translate(ctx, req); err == nil && len(out) == len(texts) {
		return out, nil
	}

	slog.Warn("core: batch échoué, fallback cue-par-cue", "provider", tr.Name(), "size", len(texts))
	res := make([]string, len(texts))
	for i, t := range texts {
		out, err := tr.Translate(ctx, translate.Request{SourceLang: source, TargetLang: target, Texts: []string{t}, Context: ctxLines})
		if err != nil {
			return nil, fmt.Errorf("core: fallback cue %d: %w", i, err)
		}
		if len(out) != 1 {
			return nil, fmt.Errorf("core: fallback cue %d: %d réponses", i, len(out))
		}
		res[i] = out[0]
	}
	return res, nil
}
