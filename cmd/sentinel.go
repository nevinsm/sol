package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	sentinelRunWorld    string
	sentinelStartWorld  string
	sentinelStopWorld   string
	sentinelAttachWorld string
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
	Short:        "Start the sentinel as a background tmux session",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(sentinelStartWorld)
		if err != nil {
			return err
		}

		if config.IsSleeping(world) {
			return fmt.Errorf("world %q is sleeping (wake it with 'sol world wake %s')", world, world)
		}

		sessName := config.SessionName(world, "sentinel")
		mgr := session.New()

		if mgr.Exists(sessName) {
			return fmt.Errorf("sentinel already running for world %q (session %s)", world, sessName)
		}

		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find sol binary: %w", err)
		}

		env := map[string]string{
			"SOL_HOME": config.Home(),
		}

		if err := mgr.Start(sessName, config.Home(),
			fmt.Sprintf("%s sentinel run --world=%s", solBin, world), env, "sentinel", world); err != nil {
			return fmt.Errorf("failed to start sentinel session: %w", err)
		}

		fmt.Printf("Sentinel started: %s\n", sessName)
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

		sessName := config.SessionName(world, "sentinel")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no sentinel running for world %q", world)
		}

		if err := mgr.Stop(sessName, false); err != nil {
			return fmt.Errorf("failed to stop sentinel: %w", err)
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

		sessName := config.SessionName(world, "sentinel")
		mgr := session.New()

		// Stop if running.
		if mgr.Exists(sessName) {
			if err := mgr.Stop(sessName, false); err != nil {
				return fmt.Errorf("failed to stop sentinel: %w", err)
			}
			fmt.Printf("Sentinel stopped for world %q\n", world)
		}

		// Start.
		if config.IsSleeping(world) {
			return fmt.Errorf("world %q is sleeping (wake it with 'sol world wake %s')", world, world)
		}

		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find sol binary: %w", err)
		}
		env := map[string]string{"SOL_HOME": config.Home()}
		if err := mgr.Start(sessName, config.Home(),
			fmt.Sprintf("%s sentinel run --world=%s", solBin, world), env, "sentinel", world); err != nil {
			return fmt.Errorf("failed to start sentinel session: %w", err)
		}
		fmt.Printf("Sentinel started: %s\n", sessName)
		return nil
	},
}

var sentinelAttachCmd = &cobra.Command{
	Use:          "attach",
	Short:        "Attach to the sentinel tmux session",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(sentinelAttachWorld)
		if err != nil {
			return err
		}

		sessName := config.SessionName(world, "sentinel")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no sentinel session for world %q (run 'sol sentinel start --world=%s' first)", world, world)
		}

		return mgr.Attach(sessName)
	},
}

// --- sol sentinel status ---

type sentinelStatusSummary struct {
	World       string `json:"world"`
	Running     bool   `json:"running"`
	SessionName string `json:"session_name"`
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

		sessName := config.SessionName(world, "sentinel")
		mgr := session.New()
		running := mgr.Exists(sessName)

		summary := sentinelStatusSummary{
			World:       world,
			Running:     running,
			SessionName: sessName,
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
		fmt.Printf("  Process:  running (%s)\n", s.SessionName)
	} else {
		fmt.Printf("  Process:  stopped\n")
	}
}

func init() {
	rootCmd.AddCommand(sentinelCmd)
	sentinelCmd.AddCommand(sentinelRunCmd)
	sentinelCmd.AddCommand(sentinelStartCmd)
	sentinelCmd.AddCommand(sentinelStopCmd)
	sentinelCmd.AddCommand(sentinelRestartCmd)
	sentinelCmd.AddCommand(sentinelAttachCmd)
	sentinelCmd.AddCommand(sentinelStatusCmd)

	sentinelRunCmd.Flags().StringVar(&sentinelRunWorld, "world", "", "world name")
	sentinelStartCmd.Flags().StringVar(&sentinelStartWorld, "world", "", "world name")
	sentinelStopCmd.Flags().StringVar(&sentinelStopWorld, "world", "", "world name")
	sentinelRestartCmd.Flags().StringVar(&sentinelRestartWorld, "world", "", "world name")
	sentinelAttachCmd.Flags().StringVar(&sentinelAttachWorld, "world", "", "world name")
	sentinelStatusCmd.Flags().StringVar(&sentinelStatusWorld, "world", "", "world name")
	sentinelStatusCmd.Flags().Bool("json", false, "output as JSON")
}
