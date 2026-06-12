package translate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// buildPrompt construit les messages system et user pour un batch.
//
// Le mapping est forcé en 1:1 via un préfixe [N] par cue. Le contexte (cues déjà
// traduites) est fourni en référence et ne doit pas être retraduit.
func buildPrompt(req Request) (system, user string) {
	system = fmt.Sprintf(`Tu es un traducteur de sous-titres professionnel. Traduis de %s vers %s.
Règles STRICTES :
- Renvoie EXACTEMENT le même nombre de lignes numérotées, dans le même ordre.
- Préserve les balises de formatage (<i>, {\an8}, etc.) et les retours à la ligne internes.
- Ne fusionne pas, ne découpe pas, n'ajoute pas de commentaire.
- Format de sortie : une ligne par cue, préfixée par [N].`, req.SourceLang, req.TargetLang)

	var b strings.Builder
	if len(req.Context) > 0 {
		b.WriteString("Contexte déjà traduit (référence, NE PAS retraduire) :\n")
		for _, c := range req.Context {
			b.WriteString(c)
			b.WriteByte('\n')
		}
		b.WriteString("\nÀ traduire :\n")
	}
	for i, t := range req.Texts {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, t)
	}
	return system, b.String()
}

var reIndexed = regexp.MustCompile(`(?m)^\s*\[(\d+)\]\s?(.*)$`)

// parseIndexed extrait les lignes [N] de la réponse LLM et les réordonne.
//
// Renvoie une erreur si le nombre de lignes valides distinctes ne vaut pas n
// (contrat len(out) == len(in) : le pipeline gère retry/fallback en amont).
func parseIndexed(content string, n int) ([]string, error) {
	out := make([]string, n)
	seen := make([]bool, n)
	count := 0
	for _, m := range reIndexed.FindAllStringSubmatch(content, -1) {
		idx, err := strconv.Atoi(m[1])
		if err != nil || idx < 1 || idx > n || seen[idx-1] {
			continue
		}
		seen[idx-1] = true
		out[idx-1] = m[2]
		count++
	}
	if count != n {
		return nil, fmt.Errorf("translate: réponse %d/%d lignes attendues", count, n)
	}
	return out, nil
}
