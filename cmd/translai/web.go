package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/gabrielfareau/translai/internal/config"
	"github.com/gabrielfareau/translai/internal/server"
)

// webFlags regroupe les flags de la commande web.
type webFlags struct {
	addr       string
	configPath string
	verbose    bool
}

func newWebCmd() *cobra.Command {
	var f webFlags
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Lance le serveur HTTP avec interface HTMX",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if f.verbose {
				slog.SetLogLoggerLevel(slog.LevelDebug)
			}
			return runWeb(cmd.Context(), f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.addr, "addr", ":8080", "adresse d'écoute (ex: :8080)")
	fl.StringVar(&f.configPath, "config", "", "chemin config.yaml")
	fl.BoolVarP(&f.verbose, "verbose", "v", false, "logs verbeux")
	return cmd
}

func runWeb(ctx context.Context, f webFlags) error {
	// Charger la configuration.
	cfgPath := f.configPath
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	store := config.NewStore(cfg)

	// Créer le serveur.
	srv := server.New(f.addr, store, cfgPath)

	// Écouter SIGINT / SIGTERM → annuler le contexte → graceful shutdown.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("web: signal reçu, arrêt…", "signal", sig)
		cancel()
	}()

	return srv.Run(ctx)
}
