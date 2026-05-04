package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/cliapi/consul"
	"github.com/nevinsm/sol/internal/config"
	iconsul "github.com/nevinsm/sol/internal/consul"
	"github.com/nevinsm/sol/internal/daemon"
	"github.com/nevinsm/sol/internal/escalation"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/processutil"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	consulInterval     string
	consulStaleTimeout string
	consulWebhook      string
	consulStatusJSON   bool
)

// consulPIDPath returns the path to the consul PID file.
func consulPIDPath() string {
	return filepath.Join(config.RuntimeDir(), "consul.pid")
}

// consulLogPath returns the path to the consul log file.
func consulLogPath() string {
	return filepath.Join(config.RuntimeDir(), "consul.log")
}

// readConsulPID is a thin wrapper retained for consulStatusCmd's "recorded PID
// is dead" wedge detection. All lifecycle operations go through the daemon
// package.
func readConsulPID() int {
	pid, _ := processutil.ReadPID(consulPIDPath())
	return pid
}

// consulLifecycle describes the consul daemon to the shared internal/daemon
// package. See cmd/up.go sphereDaemonLifecycles for how this is composed with
// the other sphere daemons.
var consulLifecycle = daemon.Lifecycle{
	Name:    "consul",
	PIDPath: consulPIDPath,
	RunArgs: []string{"consul", "run"},
	LogPath: consulLogPath,
}

var consulCmd = &cobra.Command{
	Use:     "consul",
	Short:   "Manage the sphere-level consul patrol process",
	GroupID: groupProcesses,
}

var consulRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run the consul patrol loop (foreground)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		interval, err := time.ParseDuration(consulInterval)
		if err != nil {
			return fmt.Errorf("invalid --interval: %w", err)
		}
		staleTimeout, err := time.ParseDuration(consulStaleTimeout)
		if err != nil {
			return fmt.Errorf("invalid --stale-timeout: %w", err)
		}

		webhook := consulWebhook
		if webhook == "" {
			webhook = os.Getenv("SOL_ESCALATION_WEBHOOK")
		}

		// Load global config for escalation thresholds.
		globalCfg, err := config.LoadGlobalConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load global config: %v (using defaults)\n", err)
			globalCfg.Escalation = config.DefaultEscalationConfig()
		}

		escThreshold := globalCfg.Escalation.EscalationThreshold
		if escThreshold <= 0 {
			escThreshold = 5
		}

		cfg := iconsul.Config{
			PatrolInterval:      interval,
			StaleTetherTimeout:  staleTimeout,
			SolHome:             config.Home(),
			EscalationWebhook:   webhook,
			EscalationThreshold: escThreshold,
			EscalationConfig:    globalCfg.Escalation,
		}

		// Flock-authoritative pidfile bootstrap. A second instance trying to
		// start concurrently will exit here with a clear error rather than
		// silently continuing and corrupting the pidfile.
		release, err := daemon.RunBootstrap(consulLifecycle)
		if err != nil {
			return fmt.Errorf("consul run: %w", err)
		}
		defer release()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		eventLog := events.NewLogger(config.Home())
		mgr := session.New()
		router := escalation.DefaultRouter(eventLog, sphereStore, webhook)

		d := iconsul.New(cfg, sphereStore, mgr, router, eventLog)

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Consul starting (patrol every %s, stale timeout %s, pid %d)\n",
			cfg.PatrolInterval, cfg.StaleTetherTimeout, os.Getpid())
		return d.Run(ctx)
	},
}

// consulStaleTimeoutThreshold is how long the heartbeat must be silent before
// `consul status` treats it as wedged rather than merely slow.
const consulStaleTimeoutThreshold = 10 * time.Minute

var consulStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show consul status from heartbeat",
	Long: `Show consul status from its heartbeat file.

Prints patrol count, stale tethers, caravan feeds, and escalation counts.
Use --json for machine-readable output.

Exit codes:
  0 - Consul is running and heartbeat is fresh
  1 - Consul is not running (no heartbeat file) or an I/O error occurred
  2 - Consul is wedged: heartbeat is stale, or the recorded PID is gone
      while the state still claims running (degraded/stuck case)`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		hb, err := iconsul.ReadHeartbeat(config.Home())
		if err != nil {
			return err
		}
		if hb == nil {
			fmt.Println("Consul is not running.")
			return &exitError{code: 1}
		}

		stale := hb.IsStale(consulStaleTimeoutThreshold)

		// Detect the "wedged" case: the heartbeat file claims we are
		// running, but the recorded PID is either missing or dead. The
		// PID file is best-effort; a missing PID alone is not conclusive,
		// but a known-dead PID is.
		pidGone := false
		if hb.Status == "running" {
			if pid := readConsulPID(); pid > 0 && !prefect.IsRunning(pid) {
				pidGone = true
			}
		}

		wedged := stale || pidGone

		if consulStatusJSON {
			out := consul.FromHeartbeat(hb, stale, pidGone, wedged)
			data, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			if wedged {
				return &exitError{code: 2}
			}
			return nil
		}

		ago := time.Since(hb.Timestamp).Round(time.Second)
		fmt.Printf("Consul: %s\n", hb.Status)
		fmt.Printf("Last patrol: %s ago (patrol #%d)\n", ago, hb.PatrolCount)
		fmt.Printf("Stale tethers recovered: %d\n", hb.StaleTethers)
		fmt.Printf("Caravan feeds: %d\n", hb.CaravanFeeds)
		fmt.Printf("Open escalations: %d\n", hb.Escalations)

		if stale {
			fmt.Println("\nWarning: heartbeat is stale — consul appears wedged")
		}
		if pidGone {
			fmt.Println("\nWarning: recorded consul PID is no longer alive — consul appears wedged")
		}
		if wedged {
			return &exitError{code: 2}
		}
		return nil
	},
}

var consulStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the consul as a background process",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		lc := consulLifecycle
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		res, err := daemon.Start(lc)
		if err != nil {
			return err
		}
		switch res.Status {
		case "running":
			fmt.Printf("Consul already running (pid %d)\n", res.PID)
		case "started":
			fmt.Printf("Consul started (pid %d)\n", res.PID)
		}
		return nil
	},
}

var consulStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the consul background process",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := daemon.Stop(consulLifecycle); err != nil {
			return err
		}
		fmt.Println("Consul stopped")
		return nil
	},
}

var consulRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the consul (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		lc := consulLifecycle
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		if err := daemon.Restart(lc); err != nil {
			return err
		}
		fmt.Println("Consul restarted")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(consulCmd)
	consulCmd.AddCommand(consulRunCmd)
	consulCmd.AddCommand(consulStartCmd)
	consulCmd.AddCommand(consulStopCmd)
	consulCmd.AddCommand(consulRestartCmd)
	consulCmd.AddCommand(consulStatusCmd)

	consulRunCmd.Flags().StringVar(&consulInterval, "interval", "5m", "patrol interval")
	consulRunCmd.Flags().StringVar(&consulStaleTimeout, "stale-timeout", "1h", "stale tether timeout")
	consulRunCmd.Flags().StringVar(&consulWebhook, "webhook", "", "escalation webhook URL")

	consulStatusCmd.Flags().BoolVar(&consulStatusJSON, "json", false, "output as JSON")
}
