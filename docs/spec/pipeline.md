# Spec — internal/core (Phase 4) ⚠️ PHASE CRITIQUE

Orchestration : parse → (detect) → chunk → translate → valider → réassembler.
C'est ici que les outils ratent. **Aucune altération d'index ni timestamps.**

## Fichiers
- `chunk.go` — découpage + fenêtre de contexte
- `pipeline.go` — orchestration + validation + retry + fallback + réassemblage
- `job.go` — `Job`, statuts, canal d'events

## chunk.go
- Grouper les `srt.Cue` en batches de `BatchSize` (défaut 25).
- Pour chaque batch, fournir les **2-3 dernières cues déjà traduites** comme
  `Request.Context` (cohérence pronoms/registre). Le contexte n'est **pas** retraduit.

## pipeline.go
```go
type Options struct {
    Source, Target string // Source "auto" → résolu via detect.Detect(srt.TextSample(...))
    BatchSize      int
    Translator     translate.Translator
}
func Translate(ctx context.Context, doc *srt.Document, opts Options, ev chan<- Event) error
```
Pour chaque batch :
1. Encoder le texte de chaque cue (joindre `Lines` avec un marqueur stable pour
   les retours-ligne internes).
2. Appeler `Translator.Translate`. Valider `len(out)==len(in)`.
3. **Mismatch** → retry 1×. Toujours KO → **fallback cue-par-cue** (1 requête/cue),
   logger `slog.Warn` + marquer la métadonnée `fallback` de la cue.
4. Re-split sur le marqueur → réinjecter dans `Cue.Lines`. **Index/Start/End inchangés.**

## job.go
```go
type Event struct { Stage string; Done, Total int; Err error }
type Job struct { /* id, fichier, état, events */ }
```
Le canal d'events est consommé par la CLI (barre de progression) — plus tard par le web (SSE).

## Invariants (vérifiés par les tests)
- Sortie : mêmes `Index`/`Start`/`End` que l'entrée, à l'identique.
- Tags `<i>` et retours-ligne internes préservés.
- Jamais de panic ni de SRT corrompu, même si le translator se comporte mal.

## Tests (`pipeline_test.go`) — Translator mock
- Mock « echo » → texte identique (cas non-traduit, pour vérifier le flow).
- Mock renvoyant trop/pas assez de lignes → fallback déclenché, sortie complète.
- Mock normal → index/timestamps identiques entrée/sortie, tags + multi-ligne OK.
- Contexte : vérifier que les cues précédentes traduites sont passées en `Request.Context`.

## Gate + commit
`make check` vert → `✨ feat(core): pipeline traduction + retry/fallback`
