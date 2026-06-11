# subtrans — Outil de traduction de sous-titres SRT (Go)

> **Document destiné à un LLM agentique (Cline / Continue.dev).**
> Construis le projet **par phases**, dans l'ordre. À la fin de chaque phase : code compile (`go build ./...`), tests passent (`go test ./...`), commit. Ne passe pas à la phase suivante tant que la précédente n'est pas verte.

---

## 1. Objectif

Outil **Go** qui traduit des fichiers de sous-titres `.srt` en utilisant un endpoint LLM local (**Ollama** ou **llama.cpp** en priorité, providers cloud en option). Trois surfaces d'usage :

1. **Web UI** — 2 pages (admin config + page de conversion drag-and-drop).
2. **CLI** — traduction fichier unique ou batch.
3. **Docker** — image prépackagée, binaire unique.

Le cœur métier (`internal/core`) est **partagé** entre la CLI et le serveur web. Aucune logique de traduction ne doit vivre dans les handlers HTTP ou les commandes Cobra : elles ne font qu'appeler le core.

---

## 2. Stack & dépendances imposées

| Besoin | Choix | Raison |
|---|---|---|
| HTTP router | `github.com/go-chi/chi/v5` | léger, middleware propre (stdlib `net/http` acceptable si tu préfères) |
| CLI | `github.com/spf13/cobra` | standard, sous-commandes |
| Parsing SRT | `github.com/asticode/go-astisub` | parse/sérialise SRT/VTT, gère le multi-ligne et les tags |
| Détection langue | `github.com/pemistahl/lingua-go` | détection fiable même sur texte court, pur Go |
| Config | `gopkg.in/yaml.v3` | config admin éditable, lisible à la main |
| Front | **HTMX** + **SSE** + vanilla JS minimal | pas de build front, assets embarqués via `embed` |
| Logs | `log/slog` (stdlib) | structuré, zéro dépendance |

**Contrainte forte** : un seul binaire statique, assets web embarqués avec `//go:embed`. Pas de Node, pas de bundler.

---

## 3. Architecture des fichiers

```
subtrans/
├── cmd/subtrans/main.go          # entrypoint unique (cobra root) → web | translate
├── internal/
│   ├── core/
│   │   ├── pipeline.go           # orchestration: parse → detect → chunk → translate → reassemble
│   │   ├── chunk.go              # découpage des cues en batches + fenêtre de contexte
│   │   └── job.go                # type Job, statuts, progression (canal d'events)
│   ├── srt/
│   │   ├── parse.go              # wrap astisub: load/save, extraction texte des cues
│   │   └── model.go              # Subtitle{Index, Start, End, Lines, Tags}
│   ├── detect/detect.go          # lingua-go: DetectLanguage(sampleText) → code ISO 639-1
│   ├── translate/
│   │   ├── translator.go         # interface Translator + Registry
│   │   ├── openai_compat.go      # Ollama + llama.cpp + OpenAI (endpoint /v1/chat/completions)
│   │   ├── anthropic.go          # provider Anthropic (optionnel)
│   │   ├── gemini.go             # provider Gemini (optionnel)
│   │   └── prompt.go             # construction du prompt de traduction batch
│   ├── config/
│   │   ├── config.go             # load/save YAML, validation
│   │   └── store.go              # accès thread-safe (mutex) + chemin /config/config.yaml
│   └── server/
│       ├── server.go             # chi router, embed assets, montage routes
│       ├── handlers_admin.go     # GET/POST config endpoints
│       ├── handlers_convert.go   # upload, detect, convert (SSE stream)
│       └── sse.go                # helper Server-Sent Events
├── web/
│   ├── templates/                # admin.html, convert.html, layout.html
│   └── static/                   # htmx.min.js, app.js, style.css
├── Dockerfile
├── docker-compose.yml            # subtrans + ollama (exemple)
├── go.mod
└── README.md
```

---

## 4. Modèle de données

```go
// internal/srt/model.go
type Cue struct {
    Index int
    Start time.Duration
    End   time.Duration
    Lines []string // une entrée par ligne affichée
}

type Document struct {
    Cues []Cue
    // conserver l'objet astisub original pour resérialiser sans perte de formatage
}
```

```go
// internal/config/config.go
type Config struct {
    ActiveProvider string                     `yaml:"active_provider"` // "ollama" | "llamacpp" | "openai" | "anthropic" | "gemini"
    DefaultTarget  string                     `yaml:"default_target"`  // ex: "fr"
    Providers      map[string]ProviderConfig  `yaml:"providers"`
    BatchSize      int                        `yaml:"batch_size"`      // cues par requête LLM, défaut 25
    Concurrency    int                        `yaml:"concurrency"`     // fichiers en // (batch), défaut 2
    WorkDir        string                     `yaml:"work_dir"`        // snapshots des jobs en review, défaut /config/jobs
    FlushDebounce  int                        `yaml:"flush_debounce"`  // secondes après dernier edit avant flush disque, défaut 4
    FlushInterval  int                        `yaml:"flush_interval"`  // ticker plafond, flush des jobs dirty, défaut 60
}

type ProviderConfig struct {
    Type        string  `yaml:"type"`         // "openai_compat" | "anthropic" | "gemini"
    BaseURL     string  `yaml:"base_url"`     // ex: http://ollama:11434/v1
    Model       string  `yaml:"model"`        // ex: qwen2.5:7b
    APIKey      string  `yaml:"api_key"`      // vide pour local
    Temperature float64 `yaml:"temperature"`  // défaut 0.2
}
```

> Note : Ollama **et** llama.cpp exposent un endpoint OpenAI-compatible (`/v1/chat/completions`). Le provider `openai_compat` couvre donc les trois (Ollama, llama.cpp, OpenAI) avec juste une `BaseURL`/`Model`/`APIKey` différents. Anthropic et Gemini ont des APIs distinctes → clients dédiés. Quand tu implémentes Anthropic/Gemini, **vérifie la doc API à jour** (format des messages, headers, versions).

---

## 5. Interface Translator

```go
// internal/translate/translator.go
type Request struct {
    SourceLang string   // code ISO, "auto" possible mais résolu avant l'appel
    TargetLang string
    Texts      []string // textes des cues du batch, dans l'ordre
    Context    []string // cues précédentes déjà traduites (cohérence), peut être vide
}

type Translator interface {
    Translate(ctx context.Context, req Request) ([]string, error) // len(out) DOIT == len(req.Texts)
    Name() string
}
```

**Contrat impératif** : `len(out) == len(req.Texts)`. Si le provider renvoie un nombre de lignes différent → erreur, géré par le pipeline (voir §6).

---

## 6. Pipeline de traduction (le point critique)

C'est ici que la plupart des outils ratent. **Ne jamais altérer index ni timestamps.** On ne traduit que le texte, puis on réinjecte dans la structure d'origine.

### Découpage (chunk.go)
- Grouper les cues en batches de `BatchSize` (défaut 25).
- Pour chaque batch, fournir au LLM une **fenêtre de contexte** : les 2-3 dernières cues déjà traduites (cohérence des pronoms, registre, termes récurrents). Le contexte n'est **pas** retraduit.

### Format prompt (prompt.go)
Construire un prompt qui force un mapping 1:1 indexé :

```
System: Tu es un traducteur de sous-titres professionnel. Traduis de {src} vers {tgt}.
Règles STRICTES :
- Renvoie EXACTEMENT le même nombre de lignes numérotées, dans le même ordre.
- Préserve les balises de formatage (<i>, {\an8}, etc.) et les retours à la ligne internes.
- Ne fusionne pas, ne découpe pas, n'ajoute pas de commentaire.
- Format de sortie : une ligne par cue, préfixée par [N].

User:
[1] <texte cue 1>
[2] <texte cue 2>
...
```

### Parsing + validation
- Parser la sortie en réextrayant les `[N]`.
- Vérifier `count(out) == count(in)`.
- **Si mismatch** : retry une fois ; si toujours KO → fallback **cue par cue** (1 requête par cue, lent mais robuste). Logguer en `warn`.
- Conserver les retours à la ligne internes d'une cue (le LLM tend à les écraser → re-split sur `\n` ou marqueur).

### Réassemblage
- Réinjecter chaque texte traduit dans la `Cue` d'origine (index + timestamps inchangés).
- Resérialiser via astisub → SRT valide.

### Concurrence
- **Au sein d'un fichier** : séquentiel (le contexte dépend de l'ordre).
- **Entre fichiers** (mode batch) : pool de workers, `Concurrency` en parallèle.

### Progression
`Job` expose un canal d'events `{Stage, Done, Total, Err}` que la CLI affiche en barre de progression et que le serveur web stream en SSE.

---

## 7. Web UI

### Page `/` — Conversion
Layout (HTMX, pas de SPA) :

- **Zone de dépôt** : input file + drag-and-drop, multi-fichiers (batch). Au drop d'un `.srt` :
  - POST vers `/api/detect` (upload léger) → backend parse + `lingua-go` sur un échantillon concaténé des cues → renvoie le code langue détecté.
  - Le dropdown **langue source** se positionne sur la valeur détectée mais **reste éditable** (override user si mauvaise détection).
- **Box de gauche** : 2 menus déroulants — *langue source* (auto-rempli, overridable) et *langue cible* (défaut = `DefaultTarget` de la config).
- **Gros bouton « Convert »** dessous.
- **Box de sortie** dessous : sous-titres traduits, **remplie en streaming via SSE** au fur et à mesure des batches.
- **Vue résultats / téléchargement** (état d'atterrissage après conversion) : une ligne par fichier, chacune avec son statut, un bouton **Télécharger** (`.srt`), et à sa droite un bouton **Réviser** qui ouvre `/review` pour *ce* fichier. On reste sur cette vue : **rien ne force la review**, elle est strictement opt-in par ligne.
  - **Symétrie upload/download** : si l'upload accepte le batch, le download doit l'accepter aussi → bouton **« Tout télécharger »** en `.zip` regroupant tous les `.srt` du batch. C'est une exigence, pas une option.
  - La review est **mono-fichier par design** : la conversion LLM parallélise (batch), la relecture humaine est séquentielle — pas de mode review batch. Le bouton réviser est donc toujours par ligne, jamais global.

### Page `/admin` — Configuration
- Liste des providers configurés, sélection du **provider actif**.
- Formulaire par provider : type, base URL, modèle, clé API (masquée), température.
- Bouton **« Test connexion »** → ping l'endpoint (ex: liste des modèles Ollama, ou une requête triviale) → feedback OK/KO.
- `BatchSize`, `Concurrency`, `DefaultTarget` éditables.
- Sauvegarde → écrit `config.yaml` via `config.Store` (thread-safe).

### Routes API
```
GET  /                      page conversion
GET  /admin                 page config
GET  /api/config            config courante (clés API masquées)
POST /api/config            update config
POST /api/test-provider     test connexion provider
POST /api/detect            upload SRT → {detected_lang}
POST /api/convert           lance le job → renvoie job_id
GET  /api/convert/stream    SSE: progression + résultat (?job_id=)
GET  /api/download          télécharge un .srt résultat (?job_id=&file=)
GET  /api/download/all       télécharge tous les .srt du batch en .zip (?job_id=)

# review / éditeur d'alignement (Phase 8.5)
GET  /review                page side-by-side (?job=ID&file=...)
GET  /api/review/cues       cues alignées source|cible + flags (?job=ID&file=...)
PATCH /api/review/cue       update une cible {job, file, index, text} → maj in-memory
POST /api/review/retranslate retraduit une cue via le pipeline {job, file, index}
```

### Page `/review` — Éditeur d'alignement
**Pas un diff textuel** : index et timecodes étant garantis identiques entre source et cible, l'alignement est un mapping 1:1 par index — aucun algo de matching. Tableau : `index | timecode | source (lecture seule) | cible (éditable)`.

- **Flagging automatique** des cues suspectes (on saute de flag en flag plutôt que tout relire) :
  - `cible == source` (non traduit / echo), `cible vide`
  - ratio de longueur hors bande (`<0.4` ou `>2.5`) → troncature / hallucination
  - nombre de lignes différent entre source et cible
  - flag **« fallback utilisé »** (issu du pipeline, métadonnée runtime)
  - **CPS** (chars/seconde sur la durée de la cue) `> 20` → sous-titre illisible même si la trad est correcte ; longueur de ligne `> 42` chars
- **Autosave** : édition d'une cible → `PATCH` au blur → maj **in-memory immédiate** (l'edit est sauvé côté serveur tout de suite).
- **« Retraduire cette cue »** par ligne → rappelle le pipeline sur la seule cue avec son contexte, réutilise le core.
- Export du SRT corrigé via `/api/download`.

Les heuristiques de flagging sont **déterministes** → recalculées au chargement, jamais persistées. Seules les métadonnées runtime (flag fallback, statut reviewed) vont dans le sidecar (voir persistance ci-dessous).

### Persistance des jobs en review (write-behind)
In-memory autoritatif, disque en backup asynchrone. Deux couches distinctes :
- **Edit → in-memory** : immédiat (l'autosave).
- **in-memory → disque** : **debounce** (~`FlushDebounce`s après le dernier edit, coalesce les rafales) + **ticker plafond** (`FlushInterval`s, flush tout job encore `dirty`) + **flush au SIGTERM** (non négociable : sans ça un `docker restart` perd les edits non flushés).
- **flag `dirty` par job** : on ne réécrit que les jobs modifiés.
- **snapshot sous mutex, write hors mutex** : copier l'état sous lock, relâcher, écrire sur disque — ne jamais tenir le mutex pendant l'IO.
- Format disque sous `WorkDir/<job_id>/` : le **SRT** (canonique, directement téléchargeable) + un **sidecar JSON** pour les métadonnées runtime non recalculables (flag fallback, statut reviewed). Reload au démarrage → restaure les jobs en cours.

---

## 8. CLI

Sous-commandes Cobra :

```bash
# fichier unique
subtrans translate -i film.srt -o film.fr.srt --target fr

# source auto-détectée (défaut), override possible
subtrans translate -i film.srt --source en --target fr

# batch : dossier ou glob, --out-dir pour la destination
subtrans translate -i ./subs/ --target fr --out-dir ./subs-fr/
subtrans translate -i "*.srt" --target fr

# override provider/modèle ad hoc (sinon config.yaml)
subtrans translate -i film.srt --target fr --provider ollama --model qwen2.5:7b

# serveur web
subtrans web --addr :8080 --config /config/config.yaml
```

Flags : `-i/--input`, `-o/--output`, `--out-dir`, `--source` (défaut `auto`), `--target`, `--provider`, `--model`, `--config`, `--concurrency`, `-v/--verbose`. Barre de progression sur stderr, code retour ≠ 0 si au moins un fichier échoue.

---

## 9. Docker

Multi-stage, binaire statique, image finale minimale :

```dockerfile
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /subtrans ./cmd/subtrans

FROM gcr.io/distroless/static-debian12
COPY --from=build /subtrans /subtrans
VOLUME ["/config"]
EXPOSE 8080
ENTRYPOINT ["/subtrans"]
CMD ["web", "--addr", ":8080", "--config", "/config/config.yaml"]
```

`docker-compose.yml` d'exemple avec un service `ollama` + `subtrans`, le `base_url` du provider pointant sur `http://ollama:11434/v1`. Le dossier `/config` monté en volume pour persister `config.yaml`.

> Note `lingua-go` : tous les modèles de langue gonflent le binaire. Si la taille pose problème, restreindre le détecteur à un set de langues courantes (`lingua.NewLanguageDetectorBuilder().FromLanguages(...)`).

---

## 10. Phases d'implémentation

**Phase 0 — Squelette.** `go mod init`, arbo, root cobra avec `translate` + `web` qui ne font rien. Compile.

**Phase 1 — SRT core.** `srt.Parse`/`srt.Save` via astisub, round-trip test (parse puis save = fichier identique). Tests sur un `.srt` de fixture.

**Phase 2 — Détection langue.** `detect.DetectLanguage` sur échantillon de cues. Test sur fixtures EN/FR/ES.

**Phase 3 — Translator OpenAI-compat.** Implémenter `openai_compat.go` (couvre Ollama/llama.cpp). Mockable via httptest. Test du contrat `len(out)==len(in)`.

**Phase 4 — Pipeline.** Chunking + prompt + validation + retry + fallback cue-par-cue + réassemblage. Test bout-en-bout avec un Translator mock. **C'est la phase à ne pas bâcler.**

**Phase 5 — CLI.** `translate` fichier unique branché sur le pipeline + barre de progression.

**Phase 6 — Batch.** Mode dossier/glob, pool de workers, agrégation des erreurs.

**Phase 7 — Config.** YAML load/save thread-safe, résolution provider actif.

**Phase 8 — Serveur web.** Routes, templates HTMX, upload, `/api/detect`, SSE de conversion, page admin + test connexion.

**Phase 8.5 — Review / éditeur d'alignement.** Store de jobs mutable (in-memory + write-behind disque : debounce + ticker + flush SIGTERM), page `/review` side-by-side, flagging des cues suspectes, autosave PATCH, retraduire-une-cue. Test : edit → flush → reload restaure l'état ; SIGTERM flush bien tout `dirty`.

**Phase 9 — Providers cloud.** Anthropic + Gemini (vérifier doc API à jour). Optionnel.

**Phase 10 — Docker.** Dockerfile + compose, README avec quickstart.

---

## 11. Critères d'acceptation

- [ ] `subtrans translate -i fixture.srt --target fr` produit un SRT valide, **mêmes index et timestamps** que l'entrée, texte traduit.
- [ ] Détection auto correcte sur fixtures EN/FR/ES, overridable.
- [ ] Mismatch de lignes du LLM → retry puis fallback, jamais de SRT corrompu.
- [ ] Balises `<i>` et retours à la ligne internes préservés.
- [ ] Batch d'un dossier : tous les fichiers traités, erreurs isolées (un fichier KO ne tue pas les autres).
- [ ] Upload batch web → vue résultats avec download par fichier **et** « tout télécharger » en `.zip` fonctionnel.
- [ ] Web UI : drop SRT → langue source auto-remplie → Convert → sortie streamée → download.
- [ ] Admin : config provider persiste après restart (volume), bouton test connexion fonctionnel.
- [ ] Review : cues alignées par index, cues suspectes flaggées (echo, ratio, CPS, fallback), édition d'une cible persiste.
- [ ] Retraduire une cue ne touche que cette cue, contexte conservé.
- [ ] Edit puis `docker compose restart` → l'état de review est restauré (flush SIGTERM OK).
- [ ] `docker compose up` → UI accessible, traduction via Ollama OK.
- [ ] `go vet ./...` propre, `go test ./...` vert.

---

## 12. Points de vigilance

- **Token budget** : un batch trop gros dépasse le contexte des petits modèles locaux. `BatchSize` doit être conservateur par défaut (25) et ajustable.
- **Modèles locaux faibles** : un 7B peut ignorer le format `[N]`. Le fallback cue-par-cue est le filet de sécurité, ne pas le sauter.
- **Streaming SSE** : flush après chaque event, gérer la déconnexion client (ctx annulé → stopper le job ou le laisser finir selon ton choix, à documenter).
- **Sécurité config** : ne jamais renvoyer les clés API en clair dans `GET /api/config` (masquer).
- **Encodage** : forcer UTF-8 en sortie, gérer les SRT d'entrée en latin-1/Windows-1252 (détecter/convertir à la lecture).
- **Write-behind / shutdown** : le flush au SIGTERM est obligatoire, sinon perte d'edits au `docker restart`. Penser au `http.Server.Shutdown()` + flush des jobs dirty dans le même handler de signal, avec un timeout (ex: 5s) pour ne pas bloquer l'arrêt.
