# translai

Traducteur de sous-titres `.srt` via LLM (Ollama, llama.cpp, ou tout endpoint
OpenAI-compatible). CLI Go, binaire statique unique.

**Préserve index et timestamps** : seul le texte est traduit puis réinjecté.
Tags de formatage (`<i>`, `{\an8}`) et retours-ligne internes conservés.

> État : **v1** — CLI + UI web HTMX + éditeur de relecture + Docker. Providers : Ollama, llama.cpp, OpenAI-compatible, Anthropic, Gemini.

## Prérequis

- Go 1.23+ (pour build local) **ou** Docker.
- Un endpoint LLM OpenAI-compatible. Le plus simple : [Ollama](https://ollama.com).
  ```bash
  ollama serve              # écoute sur localhost:11434
  ollama pull llama3.2      # ou qwen2.5, mistral, ...
  ```

## Build

```bash
make build        # → ./translai (statique, CGO désactivé)
```

## Utilisation

```bash
# Traduction simple (langue source auto-détectée)
translai translate -i film.srt --target fr --model llama3.2

# Forcer la langue source
translai translate -i film.srt --source en --target fr --model llama3.2

# Fichier de sortie explicite (défaut : film.fr.srt)
translai translate -i film.srt --target fr --model llama3.2 -o film_fr.srt

# Endpoint distant / OpenAI-compatible
translai translate -i film.srt --target fr \
  --base-url https://api.exemple.com/v1 --api-key "$API_KEY" --model gpt-4o-mini
```

### Flags principaux

| Flag | Défaut | Rôle |
|---|---|---|
| `-i, --input` | — | fichier `.srt` (requis) |
| `--target` | — | langue cible, code ISO (requis) |
| `--source` | `auto` | langue source (ISO ou `auto`) |
| `--model` | — | modèle LLM (requis) |
| `--base-url` | `http://localhost:11434/v1` | endpoint OpenAI-compatible |
| `--api-key` | vide | clé API (vide en local) |
| `--temperature` | `0.2` | échantillonnage |
| `--batch-size` | `25` | cues par requête LLM |
| `-o, --output` | `<input>.<target>.srt` | fichier de sortie |
| `-v, --verbose` | `false` | logs détaillés |

La progression s'affiche sur stderr ; code retour ≠ 0 si la traduction échoue.

## Quickstart (Docker)

Pre-requis : Docker + Docker Compose.

1. Copier la config d'exemple dans `./config/` (deja presente dans le repo) :
   ```bash
   # config/config.yaml est deja dans le repo, pret a l'emploi.
   # Editer le modele ou les providers selon vos besoins.
   ```

2. Lancer les services :
   ```bash
   docker compose up -d
   ```
   L'interface web est accessible sur `http://localhost:8080`.

3. Tirer un modele Ollama (premiere utilisation) :
   ```bash
   docker compose exec ollama ollama pull qwen2.5:7b
   ```

4. Ouvrir `http://localhost:8080`, verifier la connexion au provider, puis
   deposer un fichier `.srt` pour le traduire.

5. Arreter les services :
   ```bash
   docker compose down
   ```
   Le volume `/config` persiste la configuration et les jobs (etat de review restaure au redemarrage).

### CLI Docker (sans UI)

```bash
# Construire l'image
docker build -t translai .

# Traduire un fichier (Ollama tournant sur l'hote)
docker run --rm -v "$PWD:/data" translai \
  translate -i /data/film.srt --target fr \
  --base-url http://host.docker.internal:11434/v1 --model llama3.2
```

## Comment ça marche

```
parse SRT → détection langue → découpage en batches (+ fenêtre de contexte)
          → traduction LLM (1:1 indexé [N]) → validation len(out)==len(in)
          → retry → fallback cue-par-cue → réassemblage → écriture SRT
```

Le contrat `len(out) == len(in)` est strict : si le modèle renvoie un nombre de
lignes incorrect, retry une fois puis fallback cue-par-cue. Jamais de SRT corrompu.

## Développement

```bash
make check          # gate local : vet + lint + test + build (lint sauté si golangci-lint absent)
make test           # tests
make docker-test    # gate complet en conteneur (golangci-lint inclus, reproductible)
make docker-int     # tests d'intégration vs Ollama réel (docker compose)
```

Architecture et plan de build par phases : [docs/PLAN.md](docs/PLAN.md),
specs par package : [docs/spec/](docs/spec/).

Voir [docs/PLAN.md](docs/PLAN.md) pour les specs et l'historique des phases.
