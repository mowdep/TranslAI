# Spec — internal/detect (Phase 2)

Détection de langue via `github.com/pemistahl/lingua-go`.

## Fichier
- `detect.go`

## API
```go
// Detect renvoie un code ISO 639-1 ("en", "fr", "es", ...).
func Detect(sample string) (string, error)
```

## Détails
- Construire le détecteur avec un **set restreint** de langues
  (`NewLanguageDetectorBuilder().FromLanguages(...)`) pour limiter la taille du
  binaire — set v0 : en, fr, es, de, it, pt, nl. Détecteur construit une fois
  (package-level, `sync.Once` ou var init), pas à chaque appel.
- Sample court accepté (lingua gère le texte court). Si aucune langue détectée →
  erreur explicite (l'appelant gardera `auto` ou demandera override).

## Tests (`detect_test.go`)
- `srt.TextSample` des fixtures `en/fr/es` → `Detect` renvoie `en`/`fr`/`es`.
- Chaîne vide → erreur.

## Gate + commit
`make check` vert → `✨ feat(detect): détection de langue (lingua-go)`
