package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/consul"
	"github.com/nevinsm/sol/internal/escalation"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/prefect"
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

// readConsulPID reads the consul PID from its PID file. Returns 0 if not found.
func readConsulPID() int {
	data, err := os.ReadFile(consulPIDPath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// writeConsulPID writes the consul PID to the PID file.
func writeConsulPID(pid int) error {
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}
	return os.WriteFile(consulPIDPath(), []byte(strconv.Itoa(pid)), 0o644)
}

// clearConsulPID removes the consul PID file.
func clearConsulPID() {
	_ = os.Remove(consulPIDPath())
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

		cfg := consul.Config{
			PatrolInterval:      interval,
			StaleTetherTimeout:  staleTimeout,
			SolHome:             config.Home(),
			EscalationWebhook:   webhook,
			EscalationThreshold: escThreshold,
			EscalationConfig:    globalCfg.Escalation,
		}

		// Write PID file (guard against duplicate).
		existing := readConsulPID()
		if existing > 0 && prefect.IsRunning(existing) {
			return fmt.Errorf("consul already running (pid %d)", existing)
		}
		if err := writeConsulPID(os.Getpid()); err != nil {
			return fmt.Errorf("failed to write PID file: %w", err)
		}
		defer clearConsulPID()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		eventLog := events.NewLogger(config.Home())
		mgr := session.New()
		router := escalation.DefaultRouter(eventLog, sphereStore, webhook)

		d := consul.New(cfg, sphereStore, mgr, router, eventLog)

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

var consulStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show consul status from heartbeat",
	Long: `Show consul status from its heartbeat file.

Prints patrol count, stale tethers, caravan feeds, and escalation counts.
Use --json for machine-readable output.

Exit codes:
  0 - Consul is running
  1 - Consul is not running`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		hb, err := consul.ReadHeartbeat(config.Home())
		if err != nil {
			return err
		}
		if hb == nil {
			fmt.Println("Consul is not running.")
			return &exitError{code: 1}
		}

		if consulStatusJSON {
			stale := hb.IsStale(10 * time.Minute)
			out := map[string]any{
				"status":        hb.Status,
				"timestamp":     hb.Timestamp.Format(time.RFC3339),
				"patrol_count":  hb.PatrolCount,
				"stale_tethers": hb.StaleTethers,
				"caravan_feeds": hb.CaravanFeeds,
				"escalations":   hb.Escalations,
				"stale":         stale,
			}
			data, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		ago := time.Since(hb.Timestamp).Round(time.Second)
		fmt.Printf("Consul: %s\n", hb.Status)
		fmt.Printf("Last patrol: %s ago (patrol #%d)\n", ago, hb.PatrolCount)
		fmt.Printf("Stale tethers recovered: %d\n", hb.StaleTethers)
		fmt.Printf("Caravan feeds: %d\n", hb.CaravanFeeds)
		fmt.Printf("Open escalations: %d\n", hb.Escalations)

		if hb.IsStale(10 * time.Minute) {
			fmt.Println("\nWarning: heartbeat is stale — consul may not be running")
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
		// Check if already running via PID.
		if pid := readConsulPID(); pid > 0 && prefect.IsRunning(pid) {
			fmt.Printf("Consul already running (pid %d)\n", pid)
			return nil
		}

		// Clear stale PID file.
		clearConsulPID()

		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find sol binary: %w", err)
		}

		logPath := consulLogPath()
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		proc := exec.Command(solBin, "consul", "run")
		proc.Stdout = logFile
		proc.Stderr = logFile
		proc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if err := proc.Start(); err != nil {
			logFile.Close()
			return fmt.Errorf("failed to start consul: %w", err)
		}

		pid := proc.Process.Pid
		logFile.Close()

		// Detach so consul survives our exit.
		_ = proc.Process.Release()

		// Wait briefly and confirm alive.
		time.Sleep(time.Second)
		if !prefect.IsRunning(pid) {
			clearConsulPID()
			return fmt.Errorf("consul exited immediately (check %s)", logPath)
		}

		fmt.Printf("Consul started (pid %d)\n", pid)
		return nil
	},
}

var consulStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the consul background process",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid := readConsulPID()
		if pid <= 0 || !prefect.IsRunning(pid) {
			fmt.Println("Consul not running")
			clearConsulPID()
			return nil
		}

		// Send SIGTERM for graceful shutdown.
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to stop consul (pid %d): %w", pid, err)
		}

		// Wait for process to exit (up to 30 seconds for graceful shutdown).
		for i := 0; i < 60; i++ {
			time.Sleep(500 * time.Millisecond)
			if !prefect.IsRunning(pid) {
				clearConsulPID()
				fmt.Printf("Consul stopped (pid %d)\n", pid)
				return nil
			}
		}

		// Force kill if still alive.
		_ = syscall.Kill(pid, syscall.SIGKILL)
		clearConsulPID()
		fmt.Printf("Consul killed (pid %d)\n", pid)
		return nil
	},
}

var consulRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the consul (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Stop if running.
		pid := readConsulPID()
		if pid > 0 && prefect.IsRunning(pid) {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				return fmt.Errorf("failed to stop consul (pid %d): %w", pid, err)
			}
			// Wait for exit.
			for i := 0; i < 60; i++ {
				time.Sleep(500 * time.Millisecond)
				if !prefect.IsRunning(pid) {
					break
				}
			}
			if prefect.IsRunning(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				time.Sleep(500 * time.Millisecond)
			}
			clearConsulPID()
			fmt.Printf("Consul stopped (pid %d)\n", pid)
		}

		// Start.
		return consulStartCmd.RunE(consulStartCmd, args)
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
