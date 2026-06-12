# Rapport de sécurité translai — 2026-06-13

## Résumé exécutif

12 findings : **0 critiques, 4 élevés, 5 moyens, 3 faibles.**

---

## [ÉLEVÉ-1] Absence d'authentification sur les endpoints d'administration

**Fichier** : `internal/server/server.go:194-198`

Les routes `GET /admin`, `GET /api/config`, `POST /api/config` et `POST /api/test-provider` sont accessibles sans authentification. Le `docker-compose.yml` expose le port 8080 directement sur l'hôte. Tout utilisateur ayant accès réseau peut lire la config et la remplacer intégralement.

**Remédiation** : Middleware BasicAuth ou token statique sur le groupe `/admin` et `/api/`. À minima, écouter sur `127.0.0.1` par défaut.

---

## [ÉLEVÉ-2] Fuite de clé API Gemini dans les messages d'erreur HTTP

**Fichier** : `internal/translate/gemini.go:97`

L'URL Gemini contient `?key={apiKey}`. En cas d'erreur réseau, Go inclut l'URL complète dans le `*url.Error`, qui remonte jusqu'au handler HTTP (`http.Error`) et dans les logs `slog`.

**Remédiation** : Passer la clé dans l'en-tête `x-goog-api-key` au lieu du query param. Ou wrapper l'erreur en redactant l'URL avant propagation.

---

## [ÉLEVÉ-3] SSRF via `POST /api/config` (BaseURL contrôlable)

**Fichier** : `internal/server/handlers_admin.go:57-86`, `internal/config/config.go:24`

`base_url` est accepté sans validation. Un attaquant peut cibler `http://169.254.169.254/` (IMDS) ou des services internes via `POST /api/test-provider`.

**Remédiation** : Valider que `BaseURL` est HTTP/HTTPS et rejeter les espaces d'adressage privés (`127.x`, `10.x`, `172.16-31.x`, `192.168.x`, `169.254.x`). `DialContext` custom qui rejette les IPs privées résolues.

---

## [ÉLEVÉ-4] Path traversal via le nom de fichier uploadé

**Fichier** : `internal/server/handlers_convert.go:98` → `store.go:278,296,301`

`fh.Filename` est utilisé tel quel dans `filepath.Join(workDir, jobID, snap.name)`. Un nom `../../config/config.yaml` permet d'écraser la configuration. Le même nom est aussi injecté dans `Content-Disposition` sans sanitization (injection d'en-tête HTTP).

**Remédiation** :
```go
safeName := filepath.Base(fh.Filename)
```
Pour `Content-Disposition`, utiliser `mime.FormatMediaType`.

---

## [MOYEN-1] Job ID prévisible (UnixNano) — pas d'isolation inter-utilisateurs

**Fichier** : `internal/server/handlers_convert.go:102`

IDs générés par `time.Now().UnixNano()`. Aucun contrôle d'accès sur `/api/download?job_id=`, `/api/convert/stream?job_id=`, `/api/review/cues?job=`.

**Remédiation** : `crypto/rand` pour un ID de 128 bits opaque. Cookie signé pour l'isolation.

---

## [MOYEN-2] Absence d'en-têtes de sécurité HTTP

**Fichier** : `internal/server/server.go:186-190`

Aucun `Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`.

**Remédiation** : Middleware chi ajoutant ces en-têtes sur toutes les routes.

---

## [MOYEN-3] Absence de rate limiting sur `/api/convert` — DoS local

**Fichier** : `internal/server/handlers_convert.go:62-119`

Goroutines non bornées, pas de limite sur le nombre de fichiers, pas d'expiration des `JobResult` en mémoire (accumulation illimitée).

**Remédiation** : Max 20 fichiers par job, sémaphore sur les jobs actifs, TTL 1h sur les jobs.

---

## [MOYEN-4] Prompt injection via `source` / `target`

**Fichier** : `internal/translate/prompt.go:15-20` + `internal/server/handlers_convert.go:68-75`

Les valeurs `source` et `target` sont insérées directement dans le prompt LLM sans validation ni liste blanche.

**Remédiation** : Valider par regex `^[a-z]{2,3}(-[A-Z]{2})?$` + liste blanche des codes BCP-47 supportés.

---

## [MOYEN-5] Absence de timeout HTTP (serveur + clients LLM)

**Fichier** : `internal/server/server.go:227-228`, `internal/translate/openai_compat.go:33`, `anthropic.go:40`, `gemini.go:39`

`http.Server` sans `ReadTimeout`/`WriteTimeout`/`IdleTimeout`. Clients HTTP utilisant `http.DefaultClient` (pas de timeout). Goroutines de traduction avec `context.Background()` — jamais annulées.

**Remédiation** :
```go
srv := &http.Server{ReadTimeout: 10*time.Second, WriteTimeout: 120*time.Second, IdleTimeout: 60*time.Second}
httpClient := &http.Client{Timeout: 300 * time.Second}
```

---

## [FAIBLE-1] Permissions `sidecar.json` à 0644

**Fichier** : `internal/server/store.go:296`

Les sidecars sont créés en `0644` alors que `config.yaml` est en `0600`.

**Remédiation** : Remplacer `0o644` par `0o600` ligne 296.

---

## [FAIBLE-2] Binaire exécuté en root dans le conteneur distroless

**Fichier** : `Dockerfile:26-34`

Aucune directive `USER`. L'image distroless propose `nonroot` (UID 65532) mais le conteneur démarre en root.

**Remédiation** :
```dockerfile
FROM gcr.io/distroless/static-debian12:nonroot
USER nonroot:nonroot
```

---

## [FAIBLE-3] Images Docker non épinglées par digest SHA256

**Fichier** : `Dockerfile:4,26`, `docker-compose.yml:17`

Tags mutables (`golang:1.23-alpine`, `gcr.io/distroless/static-debian12`, `ollama/ollama:latest`).

**Remédiation** : Épingler par digest SHA256.

---

## Dépendance vulnérable

**`google.golang.org/protobuf v1.31.0` — CVE-2024-24786 (ÉLEVÉ)** : boucle infinie (DoS) sur décodage protobuf malformé. Versions < v1.33.0.

**Remédiation** : `go get google.golang.org/protobuf@v1.33.0`

---

## Points positifs

- Masquage des clés API dans `Store.Get()` + `mergeMaskedKeys` correct
- Routes `/api/download` servent depuis la mémoire (pas de path traversal sur le download)
- `io.LimitReader` sur les corps JSON
- `sync.RWMutex` correctement utilisé dans tous les stores
- `html/template` (pas `text/template`) — échappement HTML automatique
- SSE avec `ctx.Done()` correct — pas de goroutine leak
- `config.yaml` écrit en `0600`
- Image distroless sans shell
