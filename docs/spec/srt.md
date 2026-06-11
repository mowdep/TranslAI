# Spec — internal/srt (Phase 1)

Wrapper autour de `github.com/asticode/go-astisub`. Parse/sérialise SRT sans
perte de formatage. Aucune dépendance LLM.

## Fichiers
- `model.go` — types
- `parse.go` — load/save + extraction texte

## Types
```go
type Cue struct {
    Index int
    Start time.Duration
    End   time.Duration
    Lines []string // une entrée par ligne affichée
}

type Document struct {
    Cues []Cue
    raw  *astisub.Subtitles // conservé pour resérialiser sans perte
}
```

## API
- `Parse(r io.Reader) (*Document, error)` — détecte l'encodage (UTF-8 défaut ;
  latin-1 / Windows-1252 → convertit en UTF-8 à la lecture), remplit `Cues` + `raw`.
- `Save(w io.Writer, d *Document) error` — resérialise via `raw` après réinjection
  du texte des `Cues`. **UTF-8 forcé en sortie.**
- `TextSample(d *Document, maxCues int) string` — concatène le texte des N
  premières cues (pour `internal/detect`).

## Invariants
- `Index`, `Start`, `End` jamais modifiés par le module.
- Tags de formatage (`<i>`, `{\an8}`, …) et retours-ligne internes préservés.

## Tests (`parse_test.go`)
- Round-trip `Parse`→`Save` sur `testdata/{en,fr,es}.srt` : sortie équivalente à
  l'entrée (index, timestamps, tags, multi-ligne intacts).
- Entrée Windows-1252 (fixture à générer dans le test) → sortie UTF-8 correcte.
- `TextSample` renvoie du texte non vide, sans timestamps.

## Gate + commit
`make check` vert → `✨ feat(srt): parse/save round-trip via astisub`
