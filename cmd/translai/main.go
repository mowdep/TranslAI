// Command translai traduit des sous-titres SRT via un endpoint LLM.
//
// Entrypoint unique. AUCUNE logique métier ici : les commandes appellent
// uniquement internal/core, internal/srt, etc. (voir docs/PLAN.md §règles).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

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
		Short: "Traduit un ou plusieurs fichiers .srt (fichier, dossier ou glob)",
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

	// Mode batch : dossier ou glob (plusieurs fichiers).
	files, isBatch, err := resolveInputs(f.input)
	if err != nil {
		return err
	}
	if isBatch {
		return runBatch(ctx, files, f, tr)
	}
	return translateFile(ctx, f.input, outputPath(f), f, tr)
}

// resolveInputs retourne la liste des fichiers .srt correspondant à l'entrée.
// isBatch = true si l'entrée est un dossier ou un glob qui correspond à plusieurs fichiers.
func resolveInputs(input string) (files []string, isBatch bool, err error) {
	// 1. Dossier ?
	if info, statErr := os.Stat(input); statErr == nil && info.IsDir() {
		matches, globErr := filepath.Glob(filepath.Join(input, "*.srt"))
		if globErr != nil {
			return nil, false, fmt.Errorf("glob dossier %s: %w", input, globErr)
		}
		return matches, true, nil
	}
	// 2. Glob explicite (contient *, ?, [) ?
	if strings.ContainsAny(input, "*?[") {
		matches, globErr := filepath.Glob(input)
		if globErr != nil {
			return nil, false, fmt.Errorf("glob %s: %w", input, globErr)
		}
		return matches, true, nil
	}
	// 3. Fichier unique.
	return []string{input}, false, nil
}

// runBatch traduit une liste de fichiers en parallèle (pool de Concurrency workers).
// Un fichier KO n'arrête pas les autres ; code retour != 0 si >= 1 échec.
func runBatch(ctx context.Context, files []string, f translateFlags, tr translate.Translator) error {
	if len(files) == 0 {
		return fmt.Errorf("aucun fichier .srt trouvé pour %q", f.input)
	}

	concurrency := f.concurrency
	if concurrency < 1 {
		concurrency = 4
	}

	type result struct {
		file string
		err  error
	}

	work := make(chan string, len(files))
	for _, file := range files {
		work <- file
	}
	close(work)

	results := make(chan result, len(files))
	var wg sync.WaitGroup

	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range work {
				out := batchOutputPath(file, f)
				err := translateFile(ctx, file, out, f, tr)
				if err != nil {
					slog.Error("batch: échec fichier", "file", file, "err", err)
				} else {
					slog.Info("batch: fichier traduit", "file", file, "out", out)
				}
				results <- result{file: file, err: err}
			}
		}()
	}

	wg.Wait()
	close(results)

	var errs []error
	for r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.file, r.err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("batch: %d fichier(s) en échec:\n%w", len(errs), errors.Join(errs...))
	}
	return nil
}

// batchOutputPath calcule le chemin de sortie d'un fichier dans le mode batch.
func batchOutputPath(file string, f translateFlags) string {
	base := filepath.Base(file)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext) + "." + f.target + ext
	if f.outDir != "" {
		return filepath.Join(f.outDir, name)
	}
	return filepath.Join(filepath.Dir(file), name)
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
