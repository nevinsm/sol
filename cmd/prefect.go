package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var prefectCmd = &cobra.Command{
	Use:     "prefect",
	Short:   "Manage the sol prefect",
	GroupID: groupProcesses,
}

var prefectRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run the prefect (foreground)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logPath := filepath.Join(config.RuntimeDir(), "prefect.log")
		logger, logFile, err := prefect.NewLogger(logPath)
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}
		if logFile != nil {
			defer logFile.Close()
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := session.New()
		cfg := prefect.DefaultConfig()

		consulEnabled, _ := cmd.Flags().GetBool("consul")
		sourceRepo, _ := cmd.Flags().GetString("source-repo")
		worlds, _ := cmd.Flags().GetStringSlice("worlds")
		if consulEnabled {
			cfg.ConsulEnabled = true
		}
		if sourceRepo != "" {
			cfg.ConsulSourceRepo = sourceRepo
		}
		if len(worlds) > 0 {
			cfg.Worlds = worlds
		}

		eventLog := events.NewLogger(config.Home())
		sup := prefect.New(cfg, sphereStore, mgr, logger, eventLog)

		// Signal handling.
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Prefect started (pid %d)\n", os.Getpid())
		fmt.Fprintf(os.Stderr, "Log: %s\n", logPath)
		return sup.Run(ctx)
	},
}

var prefectStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the running prefect",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := prefect.ReadPID()
		if err != nil {
			return err
		}
		if pid == 0 {
			return fmt.Errorf("no prefect running")
		}
		if !prefect.IsRunning(pid) {
			prefect.ClearPID()
			return fmt.Errorf("prefect not running (stale PID %d removed)", pid)
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to send SIGTERM to prefect (pid %d): %w", pid, err)
		}
		fmt.Fprintf(os.Stderr, "Sent SIGTERM to prefect (pid %d)\n", pid)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(prefectCmd)
	prefectCmd.AddCommand(prefectRunCmd)
	prefectCmd.AddCommand(prefectStopCmd)

	prefectRunCmd.Flags().Bool("consul", false, "Enable consul monitoring and auto-start")
	prefectRunCmd.Flags().String("source-repo", "", "Source repository path (for consul dispatch)")
	prefectRunCmd.Flags().StringSlice("worlds", nil, "Comma-separated list of worlds to supervise (default: all)")
}
