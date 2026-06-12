// Package core orchestre la traduction : encode les cues, découpe en batches
// avec fenêtre de contexte, appelle le Translator, valide/retente/fallback, puis
// réinjecte le texte traduit. Index et timestamps des cues ne sont jamais touchés.
package core

// Stages émis sur le canal d'events.
const (
	StageStart = "start"
	StageBatch = "batch"
	StageDone  = "done"
	StageError = "error"
)

// Event décrit la progression d'un job de traduction. Consommé par la CLI (barre
// de progression) et, plus tard, par le serveur web (SSE).
type Event struct {
	Stage string
	Done  int // cues traduites
	Total int // cues au total
	Err   error
}
