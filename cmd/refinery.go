package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/refinery"
	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var refineryCmd = &cobra.Command{
	Use:   "refinery",
	Short: "Manage the merge pipeline refinery",
}

var refineryRunCmd = &cobra.Command{
	Use:   "run <rig>",
	Short: "Run the refinery for a rig (foreground)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]

		if err := config.EnsureDirs(); err != nil {
			return fmt.Errorf("failed to ensure directories: %w", err)
		}

		logPath := filepath.Join(config.RuntimeDir(), fmt.Sprintf("refinery-%s.log", rig))
		logger, logFile, err := refinery.NewLogger(logPath)
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}
		if logFile != nil {
			defer logFile.Close()
		}

		rigStore, err := store.OpenRig(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		sourceRepo, err := dispatch.DiscoverSourceRepo()
		if err != nil {
			return err
		}

		cfg := refinery.DefaultConfig()

		// Load quality gates from config file.
		gatesPath := filepath.Join(config.RigDir(rig), "refinery", "quality-gates.txt")
		gates, err := refinery.LoadQualityGates(gatesPath, cfg.QualityGates)
		if err != nil {
			return fmt.Errorf("failed to load quality gates: %w", err)
		}
		cfg.QualityGates = gates

		ref := refinery.New(rig, sourceRepo, rigStore, townStore, cfg, logger)

		// Signal handling.
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Refinery started for rig %q (pid %d)\n", rig, os.Getpid())
		fmt.Fprintf(os.Stderr, "Log: %s\n", logPath)
		return ref.Run(ctx)
	},
}

func init() {
	rootCmd.AddCommand(refineryCmd)
	refineryCmd.AddCommand(refineryRunCmd)
}
