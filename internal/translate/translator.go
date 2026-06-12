// Package translate définit l'interface Translator et ses implémentations.
//
// Aucune orchestration ici (chunking, retry, fallback) : ça vit dans
// internal/core. Un Translator traduit un batch de textes en respectant le
// contrat len(out) == len(req.Texts).
package translate

import "context"

// Request est un batch à traduire.
type Request struct {
	SourceLang string   // code ISO, jamais "auto" (résolu en amont)
	TargetLang string   // code ISO
	Texts      []string // textes des cues du batch, dans l'ordre
	Context    []string // cues précédentes déjà traduites (référence), peut être vide
}

// Translator traduit un batch. len(out) DOIT valoir len(req.Texts).
type Translator interface {
	Translate(ctx context.Context, req Request) ([]string, error)
	Name() string
}

// Registry associe un nom de provider à son Translator.
type Registry map[string]Translator
