package core

import "strings"

// lineSep encode les retours-ligne internes d'une cue dans le texte envoyé au
// LLM (une cue = une ligne [N] dans le prompt). Re-splité au réassemblage.
const lineSep = " ⏎ "

// chunkRanges découpe n éléments en intervalles [start,end) de taille size.
func chunkRanges(n, size int) [][2]int {
	if size < 1 {
		size = 1
	}
	ranges := make([][2]int, 0, (n+size-1)/size)
	for start := 0; start < n; start += size {
		end := start + size
		if end > n {
			end = n
		}
		ranges = append(ranges, [2]int{start, end})
	}
	return ranges
}

// reconcile force lines à exactement want entrées (le nb de lignes d'origine de
// la cue), pour réinjecter sans corrompre la structure SRT même si le LLM a
// mangé le marqueur : surplus fusionné dans la dernière, manque complété par "".
func reconcile(lines []string, want int) []string {
	if want <= 0 {
		want = 1
	}
	if len(lines) == want {
		return lines
	}
	out := make([]string, want)
	if len(lines) > want {
		copy(out, lines[:want-1])
		out[want-1] = strings.Join(lines[want-1:], " ")
		return out
	}
	copy(out, lines) // entrées manquantes = "" (padding)
	return out
}
