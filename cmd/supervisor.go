package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
	"github.com/nevinsm/gt/internal/supervisor"
	"github.com/spf13/cobra"
)

var supervisorCmd = &cobra.Command{
	Use:   "supervisor",
	Short: "Manage the gt supervisor",
}

var supervisorRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the supervisor (foreground)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		logPath := filepath.Join(config.RuntimeDir(), "supervisor.log")
		logger, logFile, err := supervisor.NewLogger(logPath)
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}
		if logFile != nil {
			defer logFile.Close()
		}

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		mgr := session.New()
		cfg := supervisor.DefaultConfig()
		sup := supervisor.New(cfg, townStore, mgr, logger)

		// Signal handling.
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Supervisor started (pid %d)\n", os.Getpid())
		fmt.Fprintf(os.Stderr, "Log: %s\n", logPath)
		return sup.Run(ctx)
	},
}

var supervisorStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running supervisor",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := supervisor.ReadPID()
		if err != nil {
			return err
		}
		if pid == 0 {
			return fmt.Errorf("no supervisor running")
		}
		if !supervisor.IsRunning(pid) {
			supervisor.ClearPID()
			return fmt.Errorf("supervisor not running (stale PID %d removed)", pid)
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to send SIGTERM to supervisor (pid %d): %w", pid, err)
		}
		fmt.Fprintf(os.Stderr, "Sent SIGTERM to supervisor (pid %d)\n", pid)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(supervisorCmd)
	supervisorCmd.AddCommand(supervisorRunCmd)
	supervisorCmd.AddCommand(supervisorStopCmd)
}
