# Spec — cmd/translai translate (Phases 5 & 6)

Brancher la commande `translate` (stub Phase 0) sur `internal/core`. **Aucune
logique métier ici** : la commande lit les flags, appelle le core, affiche.

## Phase 5 — fichier unique
- Résoudre `--source auto` → `detect.Detect(srt.TextSample(doc, N))`.
- `srt.Parse` → `core.Translate(...)` → `srt.Save` vers `-o` (ou `<input>.<target>.srt` par défaut).
- **Barre de progression sur stderr** alimentée par le canal `core.Event`.
- Code retour 0 si succès.
- **Supprimer `TestTranslateStubNotImplemented`** (le stub devient fonctionnel).

### Tests
- `translate -i testdata/en.srt --target fr` contre un mock httptest (provider
  openai_compat) → SRT écrit, mêmes index/timestamps, texte remplacé, tags OK, exit 0.
- `--source en` override la détection.

### Commit
`✨ feat(cli): translate fichier unique + progression`

## Phase 6 — batch
- `-i` accepte dossier ou glob ; `--out-dir` = destination.
- **Pool de `Concurrency` workers entre fichiers** ; séquentiel **dans** un fichier
  (le contexte dépend de l'ordre des cues).
- Agrégation des erreurs : un fichier KO **n'arrête pas** les autres. Code retour
  ≠ 0 si ≥1 fichier échoue. Logger par fichier.

### Tests
- Dossier de 3 fixtures + 1 provider qui échoue sur 1 fichier → 2 SRT produits,
  exit ≠ 0, erreurs isolées.
- Glob `*.srt` résolu correctement.

### Commit
`✨ feat(cli): mode batch dossier/glob + pool workers`

## Flags (déjà déclarés en Phase 0)
`-i/--input`, `-o/--output`, `--out-dir`, `--source` (défaut `auto`), `--target`,
`--provider`, `--model`, `--config`, `--concurrency`, `-v/--verbose`.
