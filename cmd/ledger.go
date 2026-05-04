package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	cliledger "github.com/nevinsm/sol/internal/cliapi/ledger"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/daemon"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/ledger"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/processutil"
	"github.com/spf13/cobra"
)

var ledgerStatusJSON bool

// ledgerLifecycle describes the ledger daemon to the shared internal/daemon
// package. The pidfile and log file live under $SOL_HOME/.runtime/.
var ledgerLifecycle = daemon.Lifecycle{
	Name:    "ledger",
	PIDPath: func() string { return daemonPIDPath("ledger") },
	RunArgs: []string{"ledger", "run"},
	LogPath: func() string { return daemonLogPath("ledger") },
}

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

		// Flock-authoritative pidfile bootstrap. A second instance trying
		// to start concurrently will exit here with a clear error.
		release, err := daemon.RunBootstrap(ledgerLifecycle)
		if err != nil {
			return fmt.Errorf("ledger run: %w", err)
		}
		defer release()

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Ledger started (OTLP HTTP on 127.0.0.1:%d)\n", cfg.Port)
		err = l.Run(ctx)
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
		if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
			return fmt.Errorf("failed to create runtime directory: %w", err)
		}
		lc := ledgerLifecycle
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		res, err := daemon.Start(lc)
		if err != nil {
			return err
		}
		switch res.Status {
		case "running":
			fmt.Printf("Ledger already running (pid %d)\n", res.PID)
		case "started":
			fmt.Printf("Ledger started (pid %d)\n", res.PID)
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
		if err := daemon.Stop(ledgerLifecycle); err != nil {
			return err
		}
		fmt.Println("Ledger stopped")
		return nil
	},
}

var ledgerRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the ledger (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		lc := ledgerLifecycle
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		if err := daemon.Restart(lc); err != nil {
			return err
		}
		fmt.Println("Ledger restarted")
		return nil
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
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, _ := processutil.ReadPID(daemonPIDPath("ledger"))
		running := pid > 0 && prefect.IsRunning(pid)

		// Also check heartbeat for richer status.
		hb, _ := ledger.ReadHeartbeat()

		if !running {
			if ledgerStatusJSON {
				data, _ := json.Marshal(cliledger.StatusResponse{
					Status: "stopped",
				})
				fmt.Println(string(data))
			} else {
				fmt.Println("Ledger is not running.")
			}
			return &exitError{code: 1}
		}

		if ledgerStatusJSON {
			resp := cliledger.StatusResponse{
				Status: "running",
				PID:    pid,
				Port:   ledger.DefaultPort,
			}
			if hb != nil {
				resp.HeartbeatAge = time.Since(hb.Timestamp).Truncate(time.Second).String()
				resp.RequestsTotal = &hb.RequestsTotal
				resp.TokensProcessed = &hb.TokensProcessed
				resp.WorldsWritten = &hb.WorldsWritten
			}
			data, err := json.Marshal(resp)
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
