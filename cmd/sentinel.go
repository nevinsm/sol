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
	"github.com/nevinsm/sol/internal/daemon"
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

// sentinelLifecycle returns a daemon.Lifecycle for the sentinel daemon of the
// given world. Per-world daemons use a factory so the pidfile/log paths
// can close over the world variable.
func sentinelLifecycle(world string) daemon.Lifecycle {
	return daemon.Lifecycle{
		Name:    "sentinel[" + world + "]",
		PIDPath: func() string { return sentinel.PIDPath(world) },
		RunArgs: []string{"sentinel", "run", "--world=" + world},
		LogPath: func() string { return sentinel.LogPath(world) },
	}
}

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

		// Flock-authoritative pidfile bootstrap. A second instance trying to
		// start concurrently will exit here with a clear error. Previously
		// sentinel.Run did this itself; the lifecycle package now owns it.
		release, err := daemon.RunBootstrap(sentinelLifecycle(world))
		if err != nil {
			return fmt.Errorf("sentinel run: %w", err)
		}
		defer release()

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
				WritID:     writID,
				World:      world,
				SourceRepo: sourceRepo,
			}, worldStore, sphereStore, mgr, eventLog)
			if err != nil {
				return nil, err
			}
			return &sentinel.CastResult{
				WritID:      result.WritID,
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

		sleeping, err := config.IsSleeping(world)
		if err != nil {
			return fmt.Errorf("failed to check sleep status for world %q: %w", world, err)
		}
		if sleeping {
			return fmt.Errorf("world %q is sleeping (wake it with 'sol world wake %s')", world, world)
		}

		lc := sentinelLifecycle(world)
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		res, err := daemon.Start(lc)
		if err != nil {
			return err
		}
		switch res.Status {
		case "running":
			fmt.Printf("Sentinel already running for world %q (pid %d)\n", world, res.PID)
		case "started":
			fmt.Printf("Sentinel started for world %q (pid %d)\n", world, res.PID)
			fmt.Printf("  Log: sol sentinel log --world=%s --follow\n", world)
		}
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
		if err := daemon.Stop(sentinelLifecycle(world)); err != nil {
			return err
		}
		fmt.Printf("Sentinel stopped for world %q\n", world)
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
		lc := sentinelLifecycle(world)
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		if err := daemon.Restart(lc); err != nil {
			return err
		}
		fmt.Printf("Sentinel restarted for world %q\n", world)
		return nil
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
	World         string `json:"world"`
	Running       bool   `json:"running"`
	PID           int    `json:"pid,omitempty"`
	PatrolCount   int    `json:"patrol_count,omitempty"`
	AgentsChecked int    `json:"agents_checked,omitempty"`
	StalledCount  int    `json:"stalled_count,omitempty"`
	ReapedCount   int    `json:"reaped_count,omitempty"`
	HeartbeatAge  string `json:"heartbeat_age,omitempty"`
	Status        string `json:"status,omitempty"`
}

var sentinelStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sentinel status",
	Long: `Show whether the sentinel process is running and its health metrics.

Exit codes:
  0 - Sentinel is running
  1 - Sentinel is not running`,
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
			if err := printJSON(summary); err != nil {
				return err
			}
		} else {
			printSentinelStatus(summary)
		}
		if !running {
			return &exitError{code: 1}
		}
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
