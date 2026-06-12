package srt

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/asticode/go-astisub"
	"golang.org/x/text/encoding/charmap"
)

// Parse lit un flux SRT et le décode en Document.
//
// Encodage : l'UTF-8 est l'encodage par défaut. Si l'entrée n'est pas de l'UTF-8
// valide, elle est interprétée comme du Windows-1252 (sur-ensemble de
// l'ISO 8859-1 / latin-1) et convertie en UTF-8 avant le parsing. La sortie de
// ce module est donc toujours de l'UTF-8.
func Parse(r io.Reader) (*Document, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("srt: lecture du flux: %w", err)
	}

	data = toUTF8(data)

	subs, err := astisub.ReadFromSRT(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("srt: parsing: %w", err)
	}

	d := &Document{
		raw:  subs,
		Cues: make([]Cue, 0, len(subs.Items)),
	}
	for _, item := range subs.Items {
		d.Cues = append(d.Cues, Cue{
			Index: item.Index,
			Start: item.StartAt,
			End:   item.EndAt,
			Lines: linesText(item.Lines),
		})
	}
	return d, nil
}

// Save resérialise le Document en SRT (UTF-8 forcé) via l'objet astisub d'origine.
//
// Le texte des Cues est réinjecté dans la structure raw avant l'écriture, ce qui
// préserve tags de formatage, positionnement et tout autre attribut conservé par
// astisub. Index, Start et End de raw ne sont jamais touchés ici.
func Save(w io.Writer, d *Document) error {
	if d == nil || d.raw == nil {
		return fmt.Errorf("srt: document non initialisé (raw nil)")
	}

	if err := injectText(d); err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := d.raw.WriteToSRT(&buf); err != nil {
		return fmt.Errorf("srt: sérialisation: %w", err)
	}

	// astisub préfixe un BOM UTF-8 ; on le retire pour une sortie UTF-8 propre.
	out := bytes.TrimPrefix(buf.Bytes(), []byte{0xEF, 0xBB, 0xBF})
	if _, err := w.Write(out); err != nil {
		return fmt.Errorf("srt: écriture: %w", err)
	}
	return nil
}

// TextSample concatène le texte des maxCues premières cues, séparé par des
// retours-ligne. Utilisé par internal/detect pour la détection de langue : le
// résultat ne contient ni index ni timestamps.
func TextSample(d *Document, maxCues int) string {
	if d == nil {
		return ""
	}
	n := len(d.Cues)
	if maxCues > 0 && maxCues < n {
		n = maxCues
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		for _, line := range d.Cues[i].Lines {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(line)
		}
	}
	return b.String()
}

// toUTF8 renvoie data inchangé s'il est déjà de l'UTF-8 valide, sinon le décode
// comme du Windows-1252 vers de l'UTF-8.
func toUTF8(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	if decoded, err := charmap.Windows1252.NewDecoder().Bytes(data); err == nil {
		return decoded
	}
	return data
}

// linesText extrait le texte affiché de chaque Line astisub.
func linesText(lines []astisub.Line) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.String())
	}
	return out
}

// injectText réinjecte le texte des Cues dans l'objet astisub raw.
//
// Pour chaque cue, le nombre de lignes doit correspondre à la structure raw. Le
// texte de chaque ligne est posé sur le premier LineItem (préservant son style)
// et les éventuels items suivants sont vidés ; ainsi les tags hérités du parsing
// (italique, couleur, …) restent appliqués sur la ligne réécrite.
func injectText(d *Document) error {
	if len(d.Cues) != len(d.raw.Items) {
		return fmt.Errorf("srt: incohérence cues/items (%d vs %d)", len(d.Cues), len(d.raw.Items))
	}
	for ci, cue := range d.Cues {
		item := d.raw.Items[ci]
		if len(cue.Lines) != len(item.Lines) {
			return fmt.Errorf("srt: cue %d: incohérence lignes (%d vs %d)", ci, len(cue.Lines), len(item.Lines))
		}
		for li := range cue.Lines {
			line := &item.Lines[li]
			if len(line.Items) == 0 {
				line.Items = []astisub.LineItem{{Text: cue.Lines[li]}}
				continue
			}
			line.Items[0].Text = cue.Lines[li]
			line.Items = line.Items[:1]
		}
	}
	return nil
}
