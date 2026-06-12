// Command translai traduit des sous-titres SRT via un endpoint LLM.
//
// Entrypoint unique. AUCUNE logique métier ici : les commandes appellent
// uniquement internal/core, internal/srt, etc. (voir docs/PLAN.md §règles).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gabrielfareau/translai/internal/core"
	"github.com/gabrielfareau/translai/internal/srt"
	"github.com/gabrielfareau/translai/internal/translate"
)

// version est surchargeable au build via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "erreur:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "translai",
		Short:         "Traducteur de sous-titres SRT via LLM",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.AddCommand(newTranslateCmd())
	return root
}

// translateFlags regroupe les flags de la commande translate.
type translateFlags struct {
	input       string
	output      string
	outDir      string
	source      string
	target      string
	provider    string
	model       string
	baseURL     string
	apiKey      string
	temperature float64
	batchSize   int
	configPath  string
	concurrency int
	verbose     bool
}

func newTranslateCmd() *cobra.Command {
	var f translateFlags
	cmd := &cobra.Command{
		Use:   "translate",
		Short: "Traduit un fichier .srt (le mode batch arrive en Phase 6)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if f.verbose {
				slog.SetLogLoggerLevel(slog.LevelDebug)
			}
			return runTranslate(cmd.Context(), f)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.input, "input", "i", "", "fichier .srt, dossier ou glob (requis)")
	fl.StringVarP(&f.output, "output", "o", "", "fichier de sortie (fichier unique)")
	fl.StringVar(&f.outDir, "out-dir", "", "dossier de sortie (mode batch)")
	fl.StringVar(&f.source, "source", "auto", "langue source (code ISO ou 'auto')")
	fl.StringVar(&f.target, "target", "", "langue cible (code ISO, requis)")
	fl.StringVar(&f.provider, "provider", "", "override provider (sinon config)")
	fl.StringVar(&f.model, "model", "", "modèle LLM (ex: llama3.2)")
	fl.StringVar(&f.baseURL, "base-url", "http://localhost:11434/v1", "endpoint OpenAI-compatible")
	fl.StringVar(&f.apiKey, "api-key", "", "clé API (vide en local)")
	fl.Float64Var(&f.temperature, "temperature", 0.2, "température d'échantillonnage")
	fl.IntVar(&f.batchSize, "batch-size", 0, "cues par requête LLM (défaut 25)")
	fl.StringVar(&f.configPath, "config", "", "chemin config.yaml")
	fl.IntVar(&f.concurrency, "concurrency", 0, "fichiers en parallèle (batch)")
	fl.BoolVarP(&f.verbose, "verbose", "v", false, "logs verbeux")
	return cmd
}

func runTranslate(ctx context.Context, f translateFlags) error {
	if f.input == "" {
		return fmt.Errorf("--input requis")
	}
	if f.target == "" {
		return fmt.Errorf("--target requis")
	}
	tr, err := buildTranslator(f)
	if err != nil {
		return err
	}

	if info, statErr := os.Stat(f.input); statErr == nil && info.IsDir() {
		return fmt.Errorf("traduction par dossier non disponible (mode batch = Phase 6)")
	}
	return translateFile(ctx, f.input, outputPath(f), f, tr)
}

func buildTranslator(f translateFlags) (translate.Translator, error) {
	if f.model == "" {
		return nil, fmt.Errorf("--model requis (ex: --model llama3.2)")
	}
	return translate.NewOpenAICompat("cli", f.baseURL, f.model, f.apiKey, f.temperature), nil
}

func translateFile(ctx context.Context, in, out string, f translateFlags, tr translate.Translator) error {
	src, err := os.Open(in)
	if err != nil {
		return fmt.Errorf("ouverture %s: %w", in, err)
	}
	doc, err := srt.Parse(src)
	_ = src.Close()
	if err != nil {
		return fmt.Errorf("parsing %s: %w", in, err)
	}

	opts := core.Options{Source: f.source, Target: f.target, BatchSize: f.batchSize, Translator: tr}
	ev := make(chan core.Event)
	errCh := make(chan error, 1)
	go func() {
		errCh <- core.Translate(ctx, doc, opts, ev)
		close(ev)
	}()
	name := filepath.Base(in)
	for e := range ev {
		if e.Total > 0 {
			fmt.Fprintf(os.Stderr, "\r%s : %d/%d cues", name, e.Done, e.Total)
		}
	}
	fmt.Fprintln(os.Stderr)
	if err := <-errCh; err != nil {
		return err
	}

	dst, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("création %s: %w", out, err)
	}
	defer dst.Close()
	if err := srt.Save(dst, doc); err != nil {
		return fmt.Errorf("écriture %s: %w", out, err)
	}
	fmt.Fprintf(os.Stderr, "→ %s\n", out)
	return nil
}

// outputPath renvoie -o si fourni, sinon "<input sans ext>.<target>.srt".
func outputPath(f translateFlags) string {
	if f.output != "" {
		return f.output
	}
	ext := filepath.Ext(f.input)
	return strings.TrimSuffix(f.input, ext) + "." + f.target + ext
}
