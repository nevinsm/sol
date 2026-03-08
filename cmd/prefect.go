package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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

		// Resolve sol binary path for starting world services (sentinel/forge).
		if solBin, err := os.Executable(); err == nil {
			cfg.SolBinary = solBin
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

var prefectStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the prefect as a background process",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := prefect.ReadPID()
		if err != nil {
			return err
		}
		if pid > 0 && prefect.IsRunning(pid) {
			fmt.Printf("Prefect already running (pid %d)\n", pid)
			return nil
		}

		// Clear stale PID if any.
		if pid > 0 {
			_ = prefect.ClearPID()
		}

		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find sol binary: %w", err)
		}

		if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
			return fmt.Errorf("failed to create runtime directory: %w", err)
		}

		logPath := filepath.Join(config.RuntimeDir(), "prefect.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		proc := exec.Command(solBin, "prefect", "run")
		proc.Stdout = logFile
		proc.Stderr = logFile
		proc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if err := proc.Start(); err != nil {
			logFile.Close()
			return fmt.Errorf("failed to start prefect: %w", err)
		}

		newPID := proc.Process.Pid
		logFile.Close()

		// Don't write PID — prefect.Run() writes its own.
		_ = proc.Process.Release()

		// Wait briefly and confirm alive.
		time.Sleep(time.Second)
		if prefect.IsRunning(newPID) {
			fmt.Printf("Prefect started (pid %d)\n", newPID)
		} else {
			return fmt.Errorf("prefect exited immediately (check %s)", logPath)
		}

		return nil
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
	prefectCmd.AddCommand(prefectStartCmd)
	prefectCmd.AddCommand(prefectStopCmd)

	prefectRunCmd.Flags().Bool("consul", false, "Enable consul monitoring and auto-start")
	prefectRunCmd.Flags().String("source-repo", "", "Source repository path (for consul dispatch)")
	prefectRunCmd.Flags().StringSlice("worlds", nil, "Comma-separated list of worlds to supervise (default: all)")
}
