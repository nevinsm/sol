package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/broker"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/spf13/cobra"
)

var (
	brokerInterval      string
	brokerRefreshMargin string
	brokerStatusJSON    bool
)

var tokenBrokerCmd = &cobra.Command{
	Use:     "broker",
	Short:   "Manage AI provider credentials and health",
	GroupID: groupProcesses,
}

var tokenBrokerRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run the broker loop (foreground)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		interval, err := time.ParseDuration(brokerInterval)
		if err != nil {
			return fmt.Errorf("invalid --interval: %w", err)
		}
		margin, err := time.ParseDuration(brokerRefreshMargin)
		if err != nil {
			return fmt.Errorf("invalid --refresh-margin: %w", err)
		}

		cfg := broker.Config{
			PatrolInterval: interval,
			RefreshMargin:  margin,
		}

		eventLog := events.NewLogger(config.Home())
		b := broker.New(cfg, eventLog)

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		return b.Run(ctx)
	},
}

var tokenBrokerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show broker status from heartbeat",
	Long: `Show whether the broker process is running via its heartbeat file.

Prints patrol count, account info, refresh statistics, and provider health state.
Use --json for machine-readable output.

Exit codes:
  0 - Broker is running
  1 - Broker is not running`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		hb, err := broker.ReadHeartbeat()
		if err != nil {
			return err
		}
		if hb == nil {
			fmt.Println("Broker is not running.")
			return &exitError{code: 1}
		}

		if brokerStatusJSON {
			out := map[string]any{
				"status":       hb.Status,
				"timestamp":    hb.Timestamp.Format(time.RFC3339),
				"patrol_count": hb.PatrolCount,
				"accounts":     hb.Accounts,
				"agent_dirs":   hb.AgentDirs,
				"refreshed":    hb.Refreshed,
				"errors":       hb.Errors,
				"stale":        hb.IsStale(10 * time.Minute),
			}
			if hb.LastRefresh != "" {
				out["last_refresh"] = hb.LastRefresh
			}
			// Provider health fields.
			if hb.ProviderHealth != "" {
				out["provider_health"] = string(hb.ProviderHealth)
			} else {
				out["provider_health"] = "healthy"
			}
			out["consecutive_failures"] = hb.ConsecutiveFailures
			if !hb.LastProbe.IsZero() {
				out["last_probe"] = hb.LastProbe.Format(time.RFC3339)
			}
			if !hb.LastHealthy.IsZero() {
				out["last_healthy"] = hb.LastHealthy.Format(time.RFC3339)
			}
			data, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		ago := time.Since(hb.Timestamp).Round(time.Second)
		fmt.Printf("Broker: %s\n", hb.Status)
		fmt.Printf("Last patrol: %s ago (patrol #%d)\n", ago, hb.PatrolCount)
		fmt.Printf("Accounts: %d\n", hb.Accounts)
		fmt.Printf("Agent dirs managed: %d\n", hb.AgentDirs)
		fmt.Printf("Tokens refreshed this patrol: %d\n", hb.Refreshed)
		if hb.Errors > 0 {
			fmt.Printf("Errors: %d\n", hb.Errors)
		}
		if hb.LastRefresh != "" {
			fmt.Printf("Last refresh: %s\n", hb.LastRefresh)
		}

		// Provider health.
		providerHealth := hb.ProviderHealth
		if providerHealth == "" {
			providerHealth = broker.HealthHealthy
		}
		fmt.Printf("Provider health: %s\n", providerHealth)
		if hb.ConsecutiveFailures > 0 {
			fmt.Printf("Consecutive failures: %d\n", hb.ConsecutiveFailures)
		}
		if !hb.LastProbe.IsZero() {
			probeAgo := time.Since(hb.LastProbe).Round(time.Second)
			fmt.Printf("Last probe: %s ago\n", probeAgo)
		}
		if !hb.LastHealthy.IsZero() && providerHealth != broker.HealthHealthy {
			healthyAgo := time.Since(hb.LastHealthy).Round(time.Second)
			fmt.Printf("Last healthy: %s ago\n", healthyAgo)
		}

		if hb.IsStale(10 * time.Minute) {
			fmt.Println("\nWarning: heartbeat is stale — broker may not be running")
		}
		return nil
	},
}

var tokenBrokerStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the broker as a background process",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid := readDaemonPID("broker")
		if pid > 0 && prefect.IsRunning(pid) {
			fmt.Printf("Broker already running (pid %d)\n", pid)
			return nil
		}

		// Clear stale PID if any.
		clearDaemonPID("broker")

		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find sol binary: %w", err)
		}

		if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
			return fmt.Errorf("failed to create runtime directory: %w", err)
		}

		logPath := daemonLogPath("broker")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		proc := exec.Command(solBin, "broker", "run")
		proc.Stdout = logFile
		proc.Stderr = logFile
		proc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if err := proc.Start(); err != nil {
			logFile.Close()
			return fmt.Errorf("failed to start broker: %w", err)
		}

		newPID := proc.Process.Pid
		logFile.Close()

		_ = writeDaemonPID("broker", newPID)
		_ = proc.Process.Release()

		// Wait briefly and confirm alive.
		time.Sleep(time.Second)
		if prefect.IsRunning(newPID) {
			fmt.Printf("Broker started (pid %d)\n", newPID)
		} else {
			clearDaemonPID("broker")
			return fmt.Errorf("broker exited immediately (check %s)", logPath)
		}

		return nil
	},
}

var tokenBrokerStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the running broker",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid := readDaemonPID("broker")
		if pid == 0 {
			fmt.Println("Broker not running")
			return nil
		}
		if !prefect.IsRunning(pid) {
			clearDaemonPID("broker")
			fmt.Printf("Broker not running (stale PID %d removed)\n", pid)
			return nil
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to send SIGTERM to broker (pid %d): %w", pid, err)
		}
		clearDaemonPID("broker")
		fmt.Printf("Sent SIGTERM to broker (pid %d)\n", pid)
		return nil
	},
}

var tokenBrokerRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the broker (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Stop if running.
		pid := readDaemonPID("broker")
		if pid > 0 && prefect.IsRunning(pid) {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				return fmt.Errorf("failed to send SIGTERM to broker (pid %d): %w", pid, err)
			}
			clearDaemonPID("broker")
			fmt.Fprintf(os.Stderr, "Sent SIGTERM to broker (pid %d), waiting for exit...\n", pid)
			for i := 0; i < 10; i++ {
				time.Sleep(500 * time.Millisecond)
				if !prefect.IsRunning(pid) {
					break
				}
			}
		} else if pid > 0 {
			clearDaemonPID("broker")
		}

		// Start.
		return tokenBrokerStartCmd.RunE(tokenBrokerStartCmd, args)
	},
}

func init() {
	rootCmd.AddCommand(tokenBrokerCmd)
	tokenBrokerCmd.AddCommand(tokenBrokerRunCmd)
	tokenBrokerCmd.AddCommand(tokenBrokerStartCmd)
	tokenBrokerCmd.AddCommand(tokenBrokerStopCmd)
	tokenBrokerCmd.AddCommand(tokenBrokerRestartCmd)
	tokenBrokerCmd.AddCommand(tokenBrokerStatusCmd)

	tokenBrokerRunCmd.Flags().StringVar(&brokerInterval, "interval", "5m", "patrol interval")
	tokenBrokerRunCmd.Flags().StringVar(&brokerRefreshMargin, "refresh-margin", "30m", "refresh tokens this long before expiry")

	tokenBrokerStatusCmd.Flags().BoolVar(&brokerStatusJSON, "json", false, "output as JSON")
}
