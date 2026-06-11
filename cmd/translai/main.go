// Command translai traduit des sous-titres SRT via un endpoint LLM.
//
// Entrypoint unique. AUCUNE logique métier ici : les commandes appellent
// uniquement internal/core, internal/config, etc. (voir docs/PLAN.md §règles).
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
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

// newTranslateCmd : stub Phase 0. Branché sur le pipeline en Phase 5.
func newTranslateCmd() *cobra.Command {
	var (
		input       string
		output      string
		outDir      string
		source      string
		target      string
		provider    string
		model       string
		configPath  string
		concurrency int
		verbose     bool
	)
	cmd := &cobra.Command{
		Use:   "translate",
		Short: "Traduit un fichier .srt ou un lot (dossier/glob)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Phase 0 : non implémenté. Remplacé en Phase 5/6.
			return fmt.Errorf("translate: non implémenté (voir docs/PLAN.md phase 5)")
		},
	}
	f := cmd.Flags()
	f.StringVarP(&input, "input", "i", "", "fichier .srt, dossier ou glob (requis)")
	f.StringVarP(&output, "output", "o", "", "fichier de sortie (fichier unique)")
	f.StringVar(&outDir, "out-dir", "", "dossier de sortie (mode batch)")
	f.StringVar(&source, "source", "auto", "langue source (code ISO ou 'auto')")
	f.StringVar(&target, "target", "", "langue cible (code ISO, requis)")
	f.StringVar(&provider, "provider", "", "override provider (sinon config)")
	f.StringVar(&model, "model", "", "override modèle (sinon config)")
	f.StringVar(&configPath, "config", "", "chemin config.yaml")
	f.IntVar(&concurrency, "concurrency", 0, "fichiers en parallèle (batch)")
	f.BoolVarP(&verbose, "verbose", "v", false, "logs verbeux")
	return cmd
}
