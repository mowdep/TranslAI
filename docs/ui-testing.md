# UI Testing & Screenshots Plan

## Objectif
- Vérifier que l'UI répond correctement après le fix htmx
- Capturer des screenshots pour le README GitHub
- Documenter les flows principaux

## Prérequis
- `make build` → binaire `./translai`
- Ollama optionnel (tests avec mock provider sinon)
- Un outil de screenshot : `screencapture` (macOS), `playwright`, ou navigateur manuel

## Plan de test (golden path)

### 1. Démarrage serveur
```bash
mkdir -p config
cp config.example.yaml config/config.yaml
./translai web --addr :8080 --config config/config.yaml
```
Vérifie : `curl -s http://localhost:8080/ | grep -i htmx` → la page charge

### 2. Page d'accueil `/`
- [ ] La page charge sans erreur JS console
- [ ] Zone de drag-and-drop visible
- [ ] Dropdowns langue source / cible présents
- [ ] Bouton Convert visible
- Screenshot → `docs/screenshots/home.png`

### 3. Page admin `/admin`
- [ ] Liste des providers s'affiche
- [ ] Formulaire provider éditable
- [ ] Bouton "Test connexion" présent
- Screenshot → `docs/screenshots/admin.png`

### 4. Flow de conversion (avec mock ou Ollama)
- [ ] Upload d'un `testdata/en.srt`
- [ ] Détection langue source auto → `en`
- [ ] Sélection cible `fr`
- [ ] Click Convert → SSE stream visible
- [ ] Bouton Télécharger apparaît
- [ ] Bouton Tout télécharger (ZIP) apparaît
- Screenshot résultats → `docs/screenshots/results.png`

### 5. Page review `/review`
- [ ] Tableau source|cible s'affiche
- [ ] Cues suspectes flaggées (si fallback utilisé)
- [ ] Edit d'une cible → autosave blur
- Screenshot → `docs/screenshots/review.png`

## Screenshots pour README
Intégrer dans README.md section `## Interface` :
```markdown
### Conversion
![Page de conversion](docs/screenshots/home.png)

### Administration
![Page admin](docs/screenshots/admin.png)

### Résultats
![Vue résultats](docs/screenshots/results.png)

### Éditeur de review
![Éditeur alignement](docs/screenshots/review.png)
```

## Outils disponibles
- `verify` skill : lance le binaire et observe le comportement
- `run` skill : démarre l'app et retourne l'état
- macOS `screencapture` : capture d'écran si navigateur ouvert manuellement
- Playwright/puppeteer : non disponible sans setup supplémentaire
- MCP browser : non disponible dans cette session

## Commande screenshot macOS
```bash
# Ouvrir http://localhost:8080 dans le navigateur, puis :
screencapture -l $(osascript -e 'tell app "Safari" to id of window 1') docs/screenshots/home.png
# ou simplement :
screencapture -i docs/screenshots/home.png   # sélection manuelle
```

## Limitations connues
- Pas de MCP playwright/browser dans cette session → screenshots manuels ou via `verify` skill
- Si Ollama absent, la conversion réelle n'est pas testable (mais les mocks httptest couvrent le flow)
