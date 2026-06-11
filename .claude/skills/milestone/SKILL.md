---
name: milestone
description: Clôture une phase du build translai — lance le gate (make check) puis commit gitmoji si vert. Utiliser après l'implémentation d'une phase de docs/PLAN.md (ou quand l'utilisateur dit "milestone", "clos la phase", "commit la phase"). Ne push jamais.
---

# milestone — clôture de phase (gate + commit gitmoji)

Clôt une phase du build autonome translai. Commit **pré-autorisé** uniquement à
la complétion d'une phase verte. **Jamais de `git push`.**

## Étapes

1. **Gate.** Lancer `make check` (tidy + vet + lint + test + build).
   - Rouge → **STOP**. Reporter l'échec exact. Ne pas commiter. Corriger d'abord.
   - `golangci-lint` absent = sauté (non bloquant), ce n'est pas un échec.

2. **Identifier la phase** depuis `docs/PLAN.md` / le diff (`git status`, `git diff --stat`).

3. **Message gitmoji.** Format `:emoji: type(scope): sujet impératif court`.
   Reprendre le sujet exact défini dans la spec/PLAN de la phase. Table :

   | Emoji | Type | Usage |
   |---|---|---|
   | 🎉 | chore | init / bootstrap |
   | ✨ | feat | nouvelle fonctionnalité (cas standard d'une phase) |
   | ✅ | test | tests seuls |
   | 🐛 | fix | correctif |
   | ♻️ | refactor | refactor sans changement de comportement |
   | 🔧 | chore | outillage (lint, make) |
   | 📝 | docs | documentation |

   Les tests d'une phase sont inclus dans son commit `feat` (pas de commit séparé).

4. **Commit.**
   ```
   git add -A
   git commit -m ":emoji: type(scope): sujet"
   ```
   Ne PAS ajouter de `Co-Authored-By`. Ne PAS push.

5. **Reporter** : hash court + sujet + rappel de la phase suivante (PLAN.md).

## Garde-fous
- Si le gate est rouge, aucune autre étape ne s'exécute.
- Un seul commit par phase. Si plusieurs phases sont mélangées dans le diff,
  signaler l'anomalie au lieu de commiter en bloc.
