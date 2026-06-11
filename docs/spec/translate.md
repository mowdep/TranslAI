# Spec — internal/translate (Phase 3)

Interface `Translator` + provider OpenAI-compatible (couvre Ollama / llama.cpp /
OpenAI via `BaseURL`/`Model`/`APIKey`).

## Fichiers
- `translator.go` — interface, types, Registry
- `prompt.go` — construction prompt batch indexé
- `openai_compat.go` — client HTTP `/v1/chat/completions`

## Types
```go
type Request struct {
    SourceLang string   // code ISO (résolu avant l'appel, jamais "auto")
    TargetLang string
    Texts      []string // textes des cues du batch, dans l'ordre
    Context    []string // cues précédentes déjà traduites (peut être vide)
}

type Translator interface {
    Translate(ctx context.Context, req Request) ([]string, error) // len(out) == len(Texts)
    Name() string
}

type Registry map[string]Translator // clé = nom provider
```

## prompt.go
System : « traducteur de sous-titres professionnel, {src}→{tgt} ». Règles STRICTES :
même nombre de lignes numérotées dans le même ordre ; préserver balises
(`<i>`, `{\an8}`) et retours-ligne internes ; pas de fusion/découpe/commentaire ;
sortie = une ligne par cue préfixée `[N]`. User : les `Texts` en `[1] …`, `[2] …`.
Le `Context` fourni en référence, **non retraduit**.

## openai_compat.go
- `POST {BaseURL}/chat/completions`, body `{model, messages, temperature}`,
  header `Authorization: Bearer {APIKey}` si non vide.
- Parser `choices[0].message.content`, réextraire les `[N]`, réordonner.
- **Contrat** : si `len(out) != len(req.Texts)` → erreur (le pipeline gère retry/fallback).
- `defer resp.Body.Close()` (lint bodyclose). Ne jamais logger l'APIKey (gosec).

## Tests (`openai_compat_test.go`)
- Mock `httptest` de `/v1/chat/completions` : vérifier méthode, path, présence du
  bearer quand clé fournie, body contient les `[N]`.
- Réponse bien formée → `len(out)==len(in)`, ordre respecté.
- Réponse avec lignes manquantes → erreur (mismatch).

## Gate + commit
`make check` vert → `✨ feat(translate): provider openai-compat + prompt batch`
