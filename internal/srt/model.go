// Package srt enveloppe github.com/asticode/go-astisub pour parser et
// resérialiser des fichiers .srt sans perte de formatage.
//
// Le module ne contient aucune logique LLM. Il expose un modèle simplifié
// (Cue/Document) au reste de l'application, tout en conservant l'objet astisub
// d'origine afin de resérialiser à l'identique (timestamps, index, tags).
package srt

import (
	"time"

	"github.com/asticode/go-astisub"
)

// Cue représente une réplique de sous-titre.
//
// Index, Start et End ne sont JAMAIS modifiés par ce module : seul le texte
// (Lines) est destiné à être traduit puis réinjecté dans la structure d'origine
// par les phases ultérieures.
type Cue struct {
	Index int
	Start time.Duration
	End   time.Duration
	Lines []string // une entrée par ligne affichée
}

// Document agrège les cues exposées et l'objet astisub d'origine.
//
// raw est conservé pour resérialiser sans perte : Save réinjecte le texte des
// Cues dans raw avant d'écrire, ce qui préserve tags et positionnement.
type Document struct {
	Cues []Cue
	raw  *astisub.Subtitles // conservé pour resérialiser sans perte
}
