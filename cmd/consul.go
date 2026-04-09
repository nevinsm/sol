package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/consul"
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

func readConsulPID() int {
	pid, _ := processutil.ReadPID(consulPIDPath())
	return pid
}

func writeConsulPID(pid int) error {
	return processutil.WritePID(consulPIDPath(), pid)
}

func clearConsulPID() {
	_ = processutil.ClearPID(consulPIDPath())
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
			fmt.Printf("Consul already running (pid %d)\n", existing)
			return nil
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
			out := map[string]any{
				"status":        hb.Status,
				"timestamp":     hb.Timestamp.Format(time.RFC3339),
				"patrol_count":  hb.PatrolCount,
				"stale_tethers": hb.StaleTethers,
				"caravan_feeds": hb.CaravanFeeds,
				"escalations":   hb.Escalations,
				"stale":         stale,
				"pid_gone":      pidGone,
				"wedged":        wedged,
			}
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

		// Reap the child in the background so it does not become a zombie.
		go func() { _ = proc.Wait() }()

		// Wait briefly and determine final state from the pidfile.
		// See the analogous three-state logic in cmd/up.go startSphereDaemons
		// and writ sol-a0d18aac092e8ab4 for the reasoning: a spawned `sol
		// consul run` that legitimately returns via the "already running"
		// early-exit path is a SUCCESS, not a failure, and must not clobber
		// the existing instance's pidfile.
		time.Sleep(time.Second)
		filePID := readConsulPID()
		switch {
		case filePID > 0 && filePID != pid && prefect.IsRunning(filePID):
			fmt.Printf("Consul already running (pid %d)\n", filePID)
			return nil
		case filePID == pid && prefect.IsRunning(pid):
			fmt.Printf("Consul started (pid %d)\n", pid)
			return nil
		default:
			_ = processutil.ClearPIDIfMatches(consulPIDPath(), pid)
			return fmt.Errorf("consul exited immediately (check %s)", logPath)
		}
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

// findRunningConsuls is a package-level indirection so tests can stub the
// /proc scan used by the consul restart recovery path.
var findRunningConsuls = func() ([]int, error) {
	return processutil.FindSolSubcommandPIDs("consul", "run")
}

// stopConsulForRestart stops the consul daemon in preparation for a restart.
// It handles three cases:
//  1. Pidfile contains a live pid — SIGTERM / SIGKILL it, clear the pidfile.
//  2. Pidfile is empty or stale but a real `sol consul run` process is alive
//     (the pidfile bug recovery path) — locate the orphan via /proc scan,
//     SIGTERM it. If multiple matches are found, refuse to guess.
//  3. Nothing to do — return nil.
func stopConsulForRestart() error {
	// Case 1: pidfile is authoritative.
	if pid := readConsulPID(); pid > 0 && prefect.IsRunning(pid) {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to stop consul (pid %d): %w", pid, err)
		}
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
		return nil
	}

	// Case 2: pidfile useless — look for an orphan `sol consul run` process.
	pids, err := findRunningConsuls()
	if err != nil {
		return fmt.Errorf("failed to scan for running consul processes: %w", err)
	}
	switch len(pids) {
	case 0:
		// Nothing running — restart will just start.
		return nil
	case 1:
		orphan := pids[0]
		fmt.Fprintf(os.Stderr,
			"pidfile empty but found running consul at pid %d via proc scan; killing to proceed with restart\n",
			orphan)
		if err := syscall.Kill(orphan, syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to stop orphan consul (pid %d): %w", orphan, err)
		}
		for i := 0; i < 60; i++ {
			time.Sleep(500 * time.Millisecond)
			if !prefect.IsRunning(orphan) {
				break
			}
		}
		if prefect.IsRunning(orphan) {
			_ = syscall.Kill(orphan, syscall.SIGKILL)
			time.Sleep(500 * time.Millisecond)
		}
		clearConsulPID()
		fmt.Printf("Orphan consul killed (pid %d)\n", orphan)
		return nil
	default:
		return fmt.Errorf(
			"pidfile empty and multiple (%d) `sol consul run` processes found via proc scan; "+
				"refusing to guess which to kill — resolve manually and retry",
			len(pids))
	}
}

var consulRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the consul (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := stopConsulForRestart(); err != nil {
			return err
		}
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
