# PLAN v0 — translai (CLI, phases 0–7)

Plan d'exécution autonome pour Claude Code. Source de vérité fonctionnelle :
`.claude/PROMPT.md`. Ce fichier découpe la **v0 CLI** en milestones atomiques.

> **Périmètre v0** : phases 0–7 (CLI). PAS de serveur web, review, cloud, Docker
> (phases 8+ reportées). Module `github.com/gabrielfareau/translai`, binaire
> `translai`.

---

## Règles permanentes (non négociables)

1. **Ordre strict.** Une phase n'est PAS commencée tant que la précédente n'est
   pas verte (`make check` OK).
2. **`make check` avant chaque commit.** Lance `tidy + vet + lint + test + build`.
   Rouge → on corrige, on ne commit pas.
3. **Commit à chaque milestone**, format **gitmoji** (voir table en bas). Les
   commits de milestone sont **pré-autorisés** par l'utilisateur pour ce build
   autonome — uniquement à la complétion d'une phase verte, jamais de `push`.
4. **Aucune logique métier dans `cmd/`.** Les commandes Cobra appellent
   `internal/core`, `internal/config`, etc. Pareil pour de futurs handlers.
5. **Invariant pipeline** : ne JAMAIS altérer index ni timestamps. Seul le texte
   est traduit puis réinjecté dans la structure d'origine.
6. **Contrat Translator** : `len(out) == len(in)`. Sinon retry une fois, puis
   fallback cue-par-cue. Jamais de SRT corrompu en sortie.
7. **UTF-8 forcé en sortie.** Entrée latin-1/Windows-1252 détectée/convertie à la
   lecture.

---

## Définition de « terminé » (acceptation v0)

Un test global `cmd/translai` (ou `internal/core`) doit prouver, sans LLM réel :

- [ ] `make check` vert (compile + `go vet` + golangci-lint + tests).
- [ ] `translai --version` et `translai translate --help` répondent (CLI se lance).
- [ ] `translate -i testdata/en.srt --target fr` contre un **mock httptest**
      (provider openai_compat) produit un SRT : **mêmes index et timestamps**
      que l'entrée, texte remplacé, tags `<i>` et retours-ligne internes préservés.
- [ ] Détection auto correcte sur `testdata/{en,fr,es}.srt`, overridable par `--source`.
- [ ] Mismatch de lignes simulé → retry puis fallback cue-par-cue, jamais de crash.
- [ ] Batch d'un dossier → tous fichiers traités, un fichier KO n'arrête pas les autres.
- [ ] `make test-integration` (tag `integration`) : skip propre si Ollama absent.

---

## Pilotage (sous-agent + skill)

Le thread principal **orchestre**, il ne code pas chaque phase à la main :

1. Déléguer la phase au sous-agent **`phase-builder`** (`.claude/agents/phase-builder.md`)
   — il lit `docs/spec/<phase>.md` + le code dépendant, implémente code+tests,
   lance `make check`, renvoie un diff. **Il ne commit pas.**
2. Relire le diff (option : skill `code-review`).
3. Clôturer avec le skill **`/milestone`** — relance le gate puis commit gitmoji.
4. Phase suivante seulement après commit vert.

| Phase | Spec | Package |
|---|---|---|
| 1 | `docs/spec/srt.md` | `internal/srt` |
| 2 | `docs/spec/detect.md` | `internal/detect` |
| 3 | `docs/spec/translate.md` | `internal/translate` |
| 4 | `docs/spec/pipeline.md` | `internal/core` |
| 5–6 | `docs/spec/cli.md` | `cmd/translai` |
| 7 | `docs/spec/config.md` | `internal/config` |

Chaque bloc de phase ci-dessous est le résumé ; **la spec fait foi** pour les
types, signatures et tests.

## Phases

### Phase 0 — Squelette ✅ (amorcé)
Déjà en place : `go.mod`, `cmd/translai/main.go` (root cobra + stub translate),
`Makefile`, `.golangci.yml`, `.gitignore`, `testdata/`.
- **À faire** : `go get github.com/spf13/cobra@latest`, `make tidy`, `make build`.
- **Test** : `cmd/translai` — `TestRootVersion`, `TestTranslateHelp` (la commande
  existe, flags `-i/--target/--source/...` présents).
- **Gate** : `make check`.
- **Commit** : `🎉 chore: bootstrap module + squelette CLI`

### Phase 1 — SRT core
`internal/srt/` : `model.go` (`Cue{Index,Start,End,Lines}`, `Document`),
`parse.go` (wrap `go-astisub`). Conserver l'objet astisub original dans `Document`
pour resérialiser sans perte. Gérer encodage entrée → UTF-8.
- `Parse(r io.Reader) (*Document, error)`, `Save(w io.Writer, *Document) error`,
  `TextSample(*Document, n int) string` (pour la détection).
- **Test** : round-trip `Parse`→`Save` sur les 3 fixtures = octets équivalents
  (timestamps/index/tags/multi-ligne intacts). Test encodage Windows-1252.
- **Gate** + **Commit** : `✨ feat(srt): parse/save round-trip via astisub`

### Phase 2 — Détection langue
`internal/detect/detect.go` : `lingua-go`. `Detect(sample string) (string, error)`
→ code ISO 639-1. Restreindre le set de langues (`FromLanguages`) pour limiter le binaire.
- **Test** : détecte `en`/`fr`/`es` sur `TextSample` des fixtures.
- **Gate** + **Commit** : `✨ feat(detect): détection de langue (lingua-go)`

### Phase 3 — Translator OpenAI-compat
`internal/translate/translator.go` (`Request`, interface `Translator`, `Registry`),
`openai_compat.go` (POST `/v1/chat/completions`), `prompt.go` (prompt batch indexé `[N]`).
Couvre Ollama / llama.cpp / OpenAI via BaseURL/Model/APIKey.
- **Test** : mock `httptest` de `/v1/chat/completions`. Vérifier construction
  requête, parsing réponse, **contrat `len(out)==len(in)`**, erreur si mismatch.
  Fermer le `resp.Body` (bodyclose).
- **Gate** + **Commit** : `✨ feat(translate): provider openai-compat + prompt batch`

### Phase 4 — Pipeline ⚠️ phase critique
`internal/core/` : `chunk.go` (batches de `BatchSize`, fenêtre contexte 2-3 cues),
`pipeline.go` (parse→detect→chunk→translate→valider→réassembler→save),
`job.go` (`Job`, canal d'events `{Stage,Done,Total,Err}`).
- Validation sortie : réextraire `[N]`, vérifier le compte ; mismatch → retry 1×
  → fallback **cue-par-cue** (`warn`). Préserver retours-ligne internes (re-split).
  Réinjecter index+timestamps inchangés.
- **Test bout-en-bout avec Translator mock** : echo→flag, mismatch→fallback,
  tags+multi-ligne préservés, index/timestamps identiques entrée/sortie.
- **Gate** + **Commit** : `✨ feat(core): pipeline traduction + retry/fallback`

### Phase 5 — CLI translate (fichier unique)
Brancher `newTranslateCmd` sur `core.Pipeline`. Barre de progression sur **stderr**
(consomme le canal d'events). Résolution `--source auto` → `detect`.
- **Test** : `translate -i testdata/en.srt --target fr` contre mock httptest →
  SRT valide écrit, invariants OK. Code retour 0.
- **Gate** + **Commit** : `✨ feat(cli): translate fichier unique + progression`

### Phase 6 — Batch
Mode dossier/glob via `-i`, `--out-dir`. Pool de `Concurrency` workers
**entre fichiers** (séquentiel **dans** un fichier). Agrégation erreurs : un fichier
KO n'arrête pas les autres, code retour ≠ 0 si ≥1 échec.
- **Test** : dossier de 3 fixtures, 1 provider qui échoue sur 1 fichier → 2 OK,
  exit ≠ 0, erreurs isolées.
- **Gate** + **Commit** : `✨ feat(cli): mode batch dossier/glob + pool workers`

### Phase 7 — Config
`internal/config/` : `config.go` (`Config`/`ProviderConfig`, load/save YAML,
validation), `store.go` (accès thread-safe mutex). Résolution provider actif ;
overrides CLI (`--provider/--model`) priment sur le YAML.
- **Test** : load/save round-trip YAML, accès concurrent (`-race`), masquage clé
  API non vide, résolution provider actif + overrides.
- **Gate** + **Commit** : `✨ feat(config): store YAML thread-safe + résolution provider`

---

## Clôture v0
Après Phase 7 verte : vérifier toute la checklist « Définition de terminé »,
puis `📝 docs: bilan v0 CLI` si des notes/README minimal sont ajoutés.

> **Post-v0** (non inclus) : phase 8 web (HTMX+SSE, tests httptest+goquery),
> 8.5 review/write-behind, 9 cloud (Anthropic/Gemini), 10 Docker. Voir
> `.claude/PROMPT.md` pour les specs.

---

## Table gitmoji (commits milestone)

| Emoji | Type | Usage |
|---|---|---|
| 🎉 | `chore` | init / bootstrap |
| ✨ | `feat` | nouvelle fonctionnalité |
| ✅ | `test` | ajout/correction de tests seuls |
| 🐛 | `fix` | correction de bug |
| ♻️ | `refactor` | refactor sans changement de comportement |
| 🔧 | `chore` | config/outillage (lint, make) |
| 📝 | `docs` | documentation |
| 🚧 | `wip` | travail en cours (éviter en milestone) |

Format : `:emoji: type(scope): sujet impératif court`. Les tests d'une phase sont
inclus dans le commit `feat` de cette phase (pas de commit séparé sauf correctif).
