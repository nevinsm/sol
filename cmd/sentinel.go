package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/sentinel"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var sentinelStatusWorld string

var (
	sentinelRunWorld   string
	sentinelStartWorld string
	sentinelStopWorld  string
	sentinelLogWorld   string
	sentinelLogFollow  bool
)

var sentinelCmd = &cobra.Command{
	Use:     "sentinel",
	Short:   "Manage the per-world sentinel health monitor",
	GroupID: groupProcesses,
}

var sentinelRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run the sentinel patrol loop (foreground)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(sentinelRunWorld)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		// Config-first source repo discovery.
		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return err
		}
		sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
		if err != nil {
			return err
		}

		eventLog := events.NewLogger(config.Home())
		mgr := session.New()
		cfg := sentinel.DefaultConfig(world, sourceRepo, config.Home())
		w := sentinel.New(cfg, sphereStore, worldStore, mgr, eventLog)

		// Wire up cast function for auto-recast of failed MRs.
		w.SetCastFunc(func(writID string) (*sentinel.CastResult, error) {
			result, err := dispatch.Cast(cmd.Context(), dispatch.CastOpts{
				WritID: writID,
				World:      world,
				SourceRepo: sourceRepo,
			}, worldStore, sphereStore, mgr, eventLog)
			if err != nil {
				return nil, err
			}
			return &sentinel.CastResult{
				WritID:  result.WritID,
				AgentName:   result.AgentName,
				SessionName: result.SessionName,
				WorktreeDir: result.WorktreeDir,
			}, nil
		})

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Sentinel started for world %q (patrol interval: %s)\n",
			world, cfg.PatrolInterval)
		return w.Run(ctx)
	},
}

var sentinelStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the sentinel as a background process",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(sentinelStartWorld)
		if err != nil {
			return err
		}

		if config.IsSleeping(world) {
			return fmt.Errorf("world %q is sleeping (wake it with 'sol world wake %s')", world, world)
		}

		// Check if already running via PID file.
		if pid := sentinel.ReadPID(world); pid > 0 && sentinel.IsRunning(pid) {
			return fmt.Errorf("sentinel already running for world %q (pid %d)", world, pid)
		}

		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find sol binary: %w", err)
		}

		// Open log file for output.
		logPath := sentinel.LogPath(world)
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		// Fork `sol sentinel run --world=<world>` as a background process.
		proc := exec.Command(solBin, "sentinel", "run", "--world="+world)
		proc.Stdout = logFile
		proc.Stderr = logFile
		proc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if err := proc.Start(); err != nil {
			logFile.Close()
			return fmt.Errorf("failed to start sentinel process: %w", err)
		}

		pid := proc.Process.Pid
		logFile.Close()

		// Detach so the sentinel survives the parent.
		_ = proc.Process.Release()

		fmt.Printf("Sentinel started for world %q (pid %d)\n", world, pid)
		fmt.Printf("  Log: sol sentinel log --world=%s --follow\n", world)
		return nil
	},
}

var sentinelStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the sentinel",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(sentinelStopWorld)
		if err != nil {
			return err
		}

		pid := sentinel.ReadPID(world)
		if pid <= 0 || !sentinel.IsRunning(pid) {
			return fmt.Errorf("no sentinel running for world %q", world)
		}

		// Send SIGTERM for graceful shutdown.
		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("failed to find sentinel process: %w", err)
		}
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to send SIGTERM to sentinel (pid %d): %w", pid, err)
		}

		// Wait briefly for process to exit.
		for i := 0; i < 30; i++ {
			if !sentinel.IsRunning(pid) {
				fmt.Printf("Sentinel stopped for world %q\n", world)
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}

		// Force kill if still running.
		_ = proc.Signal(syscall.SIGKILL)
		sentinel.ClearPID(world)
		fmt.Printf("Sentinel force-killed for world %q (pid %d)\n", world, pid)
		return nil
	},
}

var sentinelRestartWorld string

var sentinelRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the sentinel (stop then start)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(sentinelRestartWorld)
		if err != nil {
			return err
		}

		// Stop if running.
		pid := sentinel.ReadPID(world)
		if pid > 0 && sentinel.IsRunning(pid) {
			sentinelStopWorld = world
			if err := sentinelStopCmd.RunE(sentinelStopCmd, nil); err != nil {
				return err
			}
		}

		// Start.
		sentinelStartWorld = world
		return sentinelStartCmd.RunE(sentinelStartCmd, nil)
	},
}

var sentinelLogCmd = &cobra.Command{
	Use:          "log",
	Short:        "Show or tail the sentinel log",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(sentinelLogWorld)
		if err != nil {
			return err
		}

		logPath := sentinel.LogPath(world)
		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			return fmt.Errorf("no sentinel log for world %q", world)
		}

		if sentinelLogFollow {
			c := exec.Command("tail", "-f", logPath)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		}

		c := exec.Command("tail", "-50", logPath)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	},
}

// --- sol sentinel status ---

type sentinelStatusSummary struct {
	World        string `json:"world"`
	Running      bool   `json:"running"`
	PID          int    `json:"pid,omitempty"`
	PatrolCount  int    `json:"patrol_count,omitempty"`
	AgentsChecked int   `json:"agents_checked,omitempty"`
	StalledCount int    `json:"stalled_count,omitempty"`
	ReapedCount  int    `json:"reaped_count,omitempty"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	Status       string `json:"status,omitempty"`
}

var sentinelStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show sentinel status",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(sentinelStatusWorld)
		if err != nil {
			return err
		}

		pid := sentinel.ReadPID(world)
		running := pid > 0 && sentinel.IsRunning(pid)

		summary := sentinelStatusSummary{
			World:   world,
			Running: running,
		}

		if running {
			summary.PID = pid
		}

		// Read heartbeat for metrics.
		if hb, err := sentinel.ReadHeartbeat(world); err == nil && hb != nil {
			summary.PatrolCount = hb.PatrolCount
			summary.AgentsChecked = hb.AgentsChecked
			summary.StalledCount = hb.StalledCount
			summary.ReapedCount = hb.ReapedCount
			summary.HeartbeatAge = time.Since(hb.Timestamp).Truncate(time.Second).String()
			summary.Status = hb.Status
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printJSON(summary)
		}

		printSentinelStatus(summary)
		return nil
	},
}

func printSentinelStatus(s sentinelStatusSummary) {
	fmt.Printf("Sentinel: %s\n\n", s.World)

	if s.Running {
		fmt.Printf("  Process:       running (pid %d)\n", s.PID)
	} else {
		fmt.Printf("  Process:       stopped\n")
	}

	if s.PatrolCount > 0 {
		fmt.Printf("  Patrols:       %d\n", s.PatrolCount)
		fmt.Printf("  Agents checked: %d\n", s.AgentsChecked)
		if s.StalledCount > 0 {
			fmt.Printf("  Stalled:       %d\n", s.StalledCount)
		}
		if s.ReapedCount > 0 {
			fmt.Printf("  Reaped:        %d\n", s.ReapedCount)
		}
	}

	if s.HeartbeatAge != "" {
		fmt.Printf("  Heartbeat:     %s ago (%s)\n", s.HeartbeatAge, s.Status)
	}
}

func init() {
	rootCmd.AddCommand(sentinelCmd)
	sentinelCmd.AddCommand(sentinelRunCmd)
	sentinelCmd.AddCommand(sentinelStartCmd)
	sentinelCmd.AddCommand(sentinelStopCmd)
	sentinelCmd.AddCommand(sentinelRestartCmd)
	sentinelCmd.AddCommand(sentinelLogCmd)
	sentinelCmd.AddCommand(sentinelStatusCmd)

	sentinelRunCmd.Flags().StringVar(&sentinelRunWorld, "world", "", "world name")
	sentinelStartCmd.Flags().StringVar(&sentinelStartWorld, "world", "", "world name")
	sentinelStopCmd.Flags().StringVar(&sentinelStopWorld, "world", "", "world name")
	sentinelRestartCmd.Flags().StringVar(&sentinelRestartWorld, "world", "", "world name")
	sentinelLogCmd.Flags().StringVar(&sentinelLogWorld, "world", "", "world name")
	sentinelLogCmd.Flags().BoolVar(&sentinelLogFollow, "follow", false, "follow (tail -f) the log")
	sentinelStatusCmd.Flags().StringVar(&sentinelStatusWorld, "world", "", "world name")
	sentinelStatusCmd.Flags().Bool("json", false, "output as JSON")
}
