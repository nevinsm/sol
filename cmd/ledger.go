package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/ledger"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/processutil"
	"github.com/spf13/cobra"
)

var ledgerStatusJSON bool

var ledgerCmd = &cobra.Command{
	Use:     "ledger",
	Short:   "Manage the token tracking ledger",
	GroupID: groupProcesses,
}

var ledgerRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run the ledger OTLP receiver (foreground)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := ledger.DefaultConfig(config.Home())
		eventLog := events.NewLogger(config.Home())
		l := ledger.New(cfg, eventLog)

		// Write PID file so prefect can track this process.
		if err := prefect.WriteDaemonPID("ledger", os.Getpid()); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write PID file: %v\n", err)
		}

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Ledger started (OTLP HTTP on 127.0.0.1:%d)\n", cfg.Port)
		err := l.Run(ctx)
		fmt.Fprintf(os.Stderr, "Ledger stopped\n")
		return err
	},
}

var ledgerStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the ledger as a background process",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if already running via PID.
		pid := readDaemonPID("ledger")
		if pid > 0 && prefect.IsRunning(pid) {
			fmt.Printf("Ledger already running (pid %d)\n", pid)
			return nil
		}

		// Clear stale PID if any.
		clearDaemonPID("ledger")

		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find sol binary: %w", err)
		}

		if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
			return fmt.Errorf("failed to create runtime directory: %w", err)
		}

		logPath := daemonLogPath("ledger")
		newPID, err := processutil.StartDaemon(logPath, append(os.Environ(), "SOL_HOME="+config.Home()), solBin, "ledger", "run")
		if err != nil {
			return fmt.Errorf("failed to start ledger: %w", err)
		}

		_ = writeDaemonPID("ledger", newPID)

		// Wait briefly and confirm alive.
		time.Sleep(time.Second)
		if prefect.IsRunning(newPID) {
			fmt.Printf("Ledger started (pid %d)\n", newPID)
		} else {
			clearDaemonPID("ledger")
			return fmt.Errorf("ledger exited immediately (check %s)", logPath)
		}

		return nil
	},
}

var ledgerStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the ledger background process",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid := readDaemonPID("ledger")
		if pid == 0 {
			fmt.Println("Ledger not running")
			return nil
		}
		if !prefect.IsRunning(pid) {
			clearDaemonPID("ledger")
			fmt.Printf("Ledger not running (stale PID %d removed)\n", pid)
			return nil
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to send SIGTERM to ledger (pid %d): %w", pid, err)
		}
		clearDaemonPID("ledger")
		fmt.Printf("Sent SIGTERM to ledger (pid %d)\n", pid)
		return nil
	},
}

var ledgerRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the ledger (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Stop if running.
		pid := readDaemonPID("ledger")
		if pid > 0 && prefect.IsRunning(pid) {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				return fmt.Errorf("failed to stop ledger (pid %d): %w", pid, err)
			}
			clearDaemonPID("ledger")
			fmt.Printf("Ledger stopped (pid %d)\n", pid)
			// Brief pause for graceful shutdown.
			time.Sleep(time.Second)
		}
		return ledgerStartCmd.RunE(ledgerStartCmd, args)
	},
}

var ledgerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show ledger status",
	Long: `Show whether the ledger process is running.

Prints PID, OTLP port, and heartbeat info. Use --json for machine-readable output.

Exit codes:
  0 - Ledger is running
  1 - Ledger is not running`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid := readDaemonPID("ledger")
		running := pid > 0 && prefect.IsRunning(pid)

		// Also check heartbeat for richer status.
		hb, _ := ledger.ReadHeartbeat()

		if !running {
			if ledgerStatusJSON {
				data, _ := json.Marshal(map[string]any{
					"status": "stopped",
				})
				fmt.Println(string(data))
				return nil
			}
			fmt.Println("Ledger is not running.")
			return &exitError{code: 1}
		}

		if ledgerStatusJSON {
			out := map[string]any{
				"status": "running",
				"pid":    pid,
				"port":   ledger.DefaultPort,
			}
			if hb != nil {
				out["heartbeat_age"] = time.Since(hb.Timestamp).Truncate(time.Second).String()
				out["requests_total"] = hb.RequestsTotal
				out["tokens_processed"] = hb.TokensProcessed
				out["worlds_written"] = hb.WorldsWritten
			}
			data, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("Ledger: running\n")
		fmt.Printf("PID: %d\n", pid)
		fmt.Printf("OTLP port: %d\n", ledger.DefaultPort)
		if hb != nil {
			fmt.Printf("Heartbeat: %s ago\n", time.Since(hb.Timestamp).Truncate(time.Second))
			fmt.Printf("Requests: %d\n", hb.RequestsTotal)
			fmt.Printf("Tokens processed: %d\n", hb.TokensProcessed)
			fmt.Printf("Worlds written: %d\n", hb.WorldsWritten)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(ledgerCmd)
	ledgerCmd.AddCommand(ledgerRunCmd)
	ledgerCmd.AddCommand(ledgerStartCmd)
	ledgerCmd.AddCommand(ledgerStopCmd)
	ledgerCmd.AddCommand(ledgerRestartCmd)
	ledgerCmd.AddCommand(ledgerStatusCmd)

	ledgerStatusCmd.Flags().BoolVar(&ledgerStatusJSON, "json", false, "output as JSON")
}
