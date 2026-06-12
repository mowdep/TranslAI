// Package detect identifie la langue d'un échantillon de texte via lingua-go.
//
// Le détecteur est restreint à un set de langues courantes pour limiter la
// taille du binaire (lingua embarque un modèle par langue).
package detect

import (
	"errors"
	"strings"
	"sync"

	"github.com/pemistahl/lingua-go"
)

// supported : set restreint de langues (réduit la taille du binaire).
var supported = []lingua.Language{
	lingua.English,
	lingua.French,
	lingua.Spanish,
	lingua.German,
	lingua.Italian,
	lingua.Portuguese,
	lingua.Dutch,
}

var (
	detector     lingua.LanguageDetector
	detectorOnce sync.Once
)

func getDetector() lingua.LanguageDetector {
	detectorOnce.Do(func() {
		detector = lingua.NewLanguageDetectorBuilder().
			FromLanguages(supported...).
			Build()
	})
	return detector
}

// Detect renvoie le code ISO 639-1 ("en", "fr", "es", ...) de la langue
// dominante de sample. Erreur si sample est vide ou si aucune langue du set
// restreint ne ressort.
func Detect(sample string) (string, error) {
	if strings.TrimSpace(sample) == "" {
		return "", errors.New("detect: échantillon vide")
	}
	lang, ok := getDetector().DetectLanguageOf(sample)
	if !ok {
		return "", errors.New("detect: langue indéterminée")
	}
	return strings.ToLower(lang.IsoCode639_1().String()), nil
}
