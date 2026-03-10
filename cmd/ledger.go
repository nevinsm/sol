package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/ledger"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/session"
	"github.com/spf13/cobra"
)

const ledgerSessionName = "sol-ledger"

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
		l := ledger.New(cfg)

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
	Short:        "Start the ledger as a background tmux session",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()

		if mgr.Exists(ledgerSessionName) {
			return fmt.Errorf("ledger already running (session %s)", ledgerSessionName)
		}

		// Check for orphaned ledger process: port in use but no tmux session.
		if err := killOrphanedLedger(mgr); err != nil {
			fmt.Fprintf(os.Stderr, "warning: orphan cleanup failed: %v\n", err)
		}

		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find sol binary: %w", err)
		}

		env := map[string]string{
			"SOL_HOME": config.Home(),
		}

		if err := mgr.Start(ledgerSessionName, config.Home(),
			solBin+" ledger run", env, "ledger", ""); err != nil {
			return fmt.Errorf("failed to start ledger session: %w", err)
		}

		fmt.Printf("Ledger started: %s\n", ledgerSessionName)
		return nil
	},
}

// killOrphanedLedger detects and kills an orphaned ledger process.
// An orphan is a ledger process that is still alive (holding port 4318)
// but has no tmux session managing it.
func killOrphanedLedger(mgr *session.Manager) error {
	// Check if the ledger port is in use.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ledger.DefaultPort))
	if err == nil {
		// Port is free — no orphan.
		ln.Close()
		return nil
	}

	// Port is in use. Check if there is a PID file pointing to a live process.
	pid := ledger.ReadPID()
	if pid <= 0 {
		return fmt.Errorf("port %d in use but no PID file found; cannot identify orphan", ledger.DefaultPort)
	}

	if !prefect.IsRunning(pid) {
		// PID file exists but process is dead — stale PID file.
		// Clean up PID file; the port might be held by something else.
		ledger.RemovePID()
		ledger.RemoveHeartbeat()
		return nil
	}

	// Process is alive but no tmux session — it's an orphan. Kill it.
	fmt.Fprintf(os.Stderr, "killed orphaned ledger process (pid %d)\n", pid)
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to kill orphaned ledger (pid %d): %w", pid, err)
	}

	// Clean up PID and heartbeat files.
	ledger.RemovePID()
	ledger.RemoveHeartbeat()

	return nil
}

var ledgerStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the ledger background session",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()

		if !mgr.Exists(ledgerSessionName) {
			// Check for orphan: no session but process alive.
			pid := ledger.ReadPID()
			if pid > 0 && prefect.IsRunning(pid) {
				fmt.Fprintf(os.Stderr, "killed orphaned ledger process (pid %d)\n", pid)
				_ = syscall.Kill(pid, syscall.SIGTERM)
				ledger.RemovePID()
				ledger.RemoveHeartbeat()
				fmt.Printf("Ledger stopped (orphan killed)\n")
				return nil
			}
			return fmt.Errorf("no ledger running (session %s not found)", ledgerSessionName)
		}

		if err := mgr.Stop(ledgerSessionName, false); err != nil {
			return fmt.Errorf("failed to stop ledger: %w", err)
		}

		// Clean up PID/heartbeat in case the process didn't get to do it.
		ledger.RemovePID()
		ledger.RemoveHeartbeat()

		fmt.Printf("Ledger stopped: %s\n", ledgerSessionName)
		return nil
	},
}

var ledgerRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the ledger (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		return restartSession(mgr, ledgerSessionName, "ledger",
			fmt.Sprintf("Ledger stopped: %s", ledgerSessionName),
			nil, ledgerStartCmd, args)
	},
}

var ledgerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show ledger status",
	Long: `Show whether the ledger process is running.

Prints session name and OTLP port. Use --json for machine-readable output.

Exit codes:
  0 - Ledger is running
  1 - Ledger is not running`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		running := mgr.Exists(ledgerSessionName)

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
				"status":  "running",
				"session": ledgerSessionName,
				"port":    ledger.DefaultPort,
			}
			data, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("Ledger: running\n")
		fmt.Printf("Session: %s\n", ledgerSessionName)
		fmt.Printf("OTLP port: %d\n", ledger.DefaultPort)
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
