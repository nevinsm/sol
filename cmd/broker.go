package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/broker"
	clibroker "github.com/nevinsm/sol/internal/cliapi/broker"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/daemon"
	"github.com/nevinsm/sol/internal/events"
	"github.com/spf13/cobra"
)

var (
	brokerInterval   string
	brokerStatusJSON bool
)

// brokerLifecycle describes the broker daemon to the shared internal/daemon
// package.
var brokerLifecycle = daemon.Lifecycle{
	Name:    "broker",
	PIDPath: func() string { return daemonPIDPath("broker") },
	RunArgs: []string{"broker", "run"},
	LogPath: func() string { return daemonLogPath("broker") },
}

var tokenBrokerCmd = &cobra.Command{
	Use:     "broker",
	Short:   "Manage AI provider health probing",
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

		cfg := broker.Config{
			PatrolInterval: interval,
			DiscoverFn:     broker.DiscoverWorldRuntimes,
		}

		eventLog := events.NewLogger(config.Home())
		b := broker.New(cfg, eventLog)

		// Flock-authoritative pidfile bootstrap.
		release, err := daemon.RunBootstrap(brokerLifecycle)
		if err != nil {
			return fmt.Errorf("broker run: %w", err)
		}
		defer release()

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

Prints patrol count and per-runtime liveness state.
Use --json for machine-readable output.

Exit codes:
  0 - Broker is running
  1 - Broker is not running`,
	Args:         cobra.NoArgs,
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
			resp := clibroker.FromHeartbeat(hb, 10*time.Minute)
			data, err := json.Marshal(resp)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		ago := time.Since(hb.Timestamp).Round(time.Second)
		fmt.Printf("Broker: %s\n", hb.Status)
		fmt.Printf("Last patrol: %s ago (patrol #%d)\n", ago, hb.PatrolCount)

		// Per-runtime liveness.
		if len(hb.Runtimes) > 0 {
			fmt.Println("Runtime liveness:")
			for _, r := range hb.Runtimes {
				status := "ok"
				if !r.OK {
					status = "unreachable"
				}
				fmt.Printf("  %-16s %s\n", r.Runtime, status)
			}
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
		if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
			return fmt.Errorf("failed to create runtime directory: %w", err)
		}
		lc := brokerLifecycle
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		res, err := daemon.Start(lc)
		if err != nil {
			return err
		}
		switch res.Status {
		case "running":
			fmt.Printf("Broker already running (pid %d)\n", res.PID)
		case "started":
			fmt.Printf("Broker started (pid %d)\n", res.PID)
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
		if err := daemon.Stop(brokerLifecycle); err != nil {
			return err
		}
		fmt.Println("Broker stopped")
		return nil
	},
}

var tokenBrokerRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the broker (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		lc := brokerLifecycle
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		if err := daemon.Restart(lc); err != nil {
			return err
		}
		fmt.Println("Broker restarted")
		return nil
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

	tokenBrokerStatusCmd.Flags().BoolVar(&brokerStatusJSON, "json", false, "output as JSON")
}
