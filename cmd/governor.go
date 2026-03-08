package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/governor"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/worldsync"
	"github.com/spf13/cobra"
)

var governorCmd = &cobra.Command{
	Use:     "governor",
	Short:   "Manage the per-world governor coordinator",
	GroupID: groupProcesses,
}

// --- sol governor start ---

var governorStartWorld string

var governorStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the governor for a world",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(governorStartWorld)
		if err != nil {
			return err
		}

		if config.IsSleeping(world) {
			return fmt.Errorf("world %q is sleeping (wake it with 'sol world wake %s')", world, world)
		}

		// Ensure governor and brief directories exist.
		govDir := governor.GovernorDir(world)
		if err := os.MkdirAll(govDir, 0o755); err != nil {
			return fmt.Errorf("failed to create governor directory: %w", err)
		}
		if err := os.MkdirAll(governor.BriefDir(world), 0o755); err != nil {
			return fmt.Errorf("failed to create governor brief directory: %w", err)
		}

		sessName, err := startup.Launch(governor.RoleConfig(), world, "governor", startup.LaunchOpts{})
		if err != nil {
			return err
		}

		fmt.Printf("Started governor for world %q\n", world)
		fmt.Printf("  Session: %s\n", sessName)
		fmt.Printf("  Attach:  sol governor attach --world=%s\n", world)
		return nil
	},
}

// --- sol governor stop ---

var governorStopWorld string

var governorStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the governor for a world",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(governorStopWorld)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := session.New()

		if err := governor.Stop(world, sphereStore, mgr); err != nil {
			return err
		}

		fmt.Printf("Stopped governor for world %q\n", world)
		return nil
	},
}

// --- sol governor restart ---

var governorRestartWorld string

var governorRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the governor (stop then start)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(governorRestartWorld)
		if err != nil {
			return err
		}

		// Stop if running.
		sessName := config.SessionName(world, "governor")
		mgr := session.New()
		if mgr.Exists(sessName) {
			sphereStore, err := store.OpenSphere()
			if err != nil {
				return err
			}
			if err := governor.Stop(world, sphereStore, mgr); err != nil {
				sphereStore.Close()
				return err
			}
			sphereStore.Close()
			fmt.Printf("Stopped governor for world %q\n", world)
		}

		// Start (delegate to start command).
		governorStartWorld = world
		return governorStartCmd.RunE(governorStartCmd, args)
	},
}

// --- sol governor attach ---

var governorAttachWorld string

var governorAttachCmd = &cobra.Command{
	Use:          "attach",
	Short:        "Attach to the governor's tmux session",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(governorAttachWorld)
		if err != nil {
			return err
		}

		sessName := config.SessionName(world, "governor")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no governor session for world %q (run 'sol governor start --world=%s' first)",
				world, world)
		}

		return mgr.Attach(sessName)
	},
}

// --- sol governor brief ---

var governorBriefWorld string

var governorBriefCmd = &cobra.Command{
	Use:          "brief",
	Short:        "Display the governor's brief",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(governorBriefWorld)
		if err != nil {
			return err
		}

		briefPath := governor.BriefPath(world)
		data, err := os.ReadFile(briefPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("No brief found for governor in world %q\n", world)
				return nil
			}
			return fmt.Errorf("failed to read brief: %w", err)
		}

		fmt.Print(string(data))
		return nil
	},
}

// --- sol governor debrief ---

var governorDebriefWorld string

var governorDebriefCmd = &cobra.Command{
	Use:          "debrief",
	Short:        "Archive the governor's brief and reset",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(governorDebriefWorld)
		if err != nil {
			return err
		}

		briefPath := governor.BriefPath(world)
		if _, err := os.Stat(briefPath); err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("No brief found for governor in world %q\n", world)
				return nil
			}
			return fmt.Errorf("failed to check brief: %w", err)
		}

		// Create archive directory.
		briefDir := governor.BriefDir(world)
		archiveDir := filepath.Join(briefDir, "archive")
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			return fmt.Errorf("failed to create archive directory: %w", err)
		}

		// Generate archive filename with RFC3339 timestamp, colons replaced by dashes.
		ts := time.Now().UTC().Format(time.RFC3339)
		safeTS := strings.ReplaceAll(ts, ":", "-")
		archiveFile := safeTS + ".md"
		archivePath := filepath.Join(archiveDir, archiveFile)

		// Move current brief to archive.
		if err := os.Rename(briefPath, archivePath); err != nil {
			return fmt.Errorf("failed to archive brief: %w", err)
		}

		fmt.Printf("Archived brief to .brief/archive/%s\n", archiveFile)
		fmt.Printf("Governor in world %q ready for fresh engagement\n", world)
		return nil
	},
}

// --- sol governor summary ---

var governorSummaryWorld string

var governorSummaryCmd = &cobra.Command{
	Use:          "summary",
	Short:        "Display the governor's world summary",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(governorSummaryWorld)
		if err != nil {
			return err
		}

		summaryPath := governor.WorldSummaryPath(world)
		data, err := os.ReadFile(summaryPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("No world summary found for world %q\n", world)
				return nil
			}
			return fmt.Errorf("failed to read world summary: %w", err)
		}

		fmt.Print(string(data))
		return nil
	},
}

// --- sol governor sync ---

var governorSyncWorld string

var governorSyncCmd = &cobra.Command{
	Use:          "sync",
	Short:        "Sync managed repo the governor reads from",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(governorSyncWorld)
		if err != nil {
			return err
		}

		// Sync managed repo.
		if err := worldsync.SyncRepo(world); err != nil {
			return err
		}

		// Notify governor session if running.
		mgr := session.New()
		if err := worldsync.SyncGovernor(world, mgr); err != nil {
			return err
		}

		fmt.Printf("Synced for governor in world %q\n", world)
		return nil
	},
}

// --- sol governor status ---

var governorStatusWorld string

type governorStatusSummary struct {
	World       string   `json:"world"`
	Running     bool     `json:"running"`
	SessionName string   `json:"session_name"`
	State       string   `json:"state,omitempty"`
	ActiveWrit  string   `json:"active_writ,omitempty"`
	Tethers     []string `json:"tethers,omitempty"`
	BriefAge    string   `json:"brief_age,omitempty"`
}

var governorStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show governor status",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(governorStatusWorld)
		if err != nil {
			return err
		}

		sessName := config.SessionName(world, "governor")
		mgr := session.New()
		running := mgr.Exists(sessName)

		summary := governorStatusSummary{
			World:       world,
			Running:     running,
			SessionName: sessName,
		}

		// Query sphere store for agent state.
		sphereStore, err := store.OpenSphere()
		if err == nil {
			defer sphereStore.Close()
			agentID := world + "/governor"
			agent, err := sphereStore.GetAgent(agentID)
			if err == nil && agent != nil {
				summary.State = agent.State
				summary.ActiveWrit = agent.ActiveWrit
			}
		}

		// Check tether directory.
		tethers, err := tether.List(world, "governor", "governor")
		if err == nil && len(tethers) > 0 {
			summary.Tethers = tethers
		}

		// Check brief age.
		briefPath := governor.BriefPath(world)
		if info, err := os.Stat(briefPath); err == nil {
			summary.BriefAge = time.Since(info.ModTime()).Truncate(time.Second).String()
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printJSON(summary)
		}

		printGovernorStatus(summary)
		return nil
	},
}

func printGovernorStatus(s governorStatusSummary) {
	fmt.Printf("Governor: %s\n\n", s.World)

	if s.Running {
		fmt.Printf("  Process:  running (%s)\n", s.SessionName)
	} else {
		fmt.Printf("  Process:  stopped\n")
	}

	if s.State != "" {
		fmt.Printf("  State:    %s\n", s.State)
	}

	if s.ActiveWrit != "" {
		fmt.Printf("  Active:   %s\n", s.ActiveWrit)
	}

	if len(s.Tethers) > 0 {
		fmt.Printf("  Tethers:  %s\n", strings.Join(s.Tethers, ", "))
	}

	if s.BriefAge != "" {
		fmt.Printf("  Brief:    %s old\n", s.BriefAge)
	}
}

func init() {
	// Register governor role config for startup.Launch and prefect respawn.
	startup.Register("governor", governor.RoleConfig())

	rootCmd.AddCommand(governorCmd)
	governorCmd.AddCommand(governorStartCmd, governorStopCmd, governorRestartCmd,
		governorAttachCmd, governorBriefCmd, governorDebriefCmd,
		governorSummaryCmd, governorSyncCmd, governorStatusCmd)

	// governor start flags
	governorStartCmd.Flags().StringVar(&governorStartWorld, "world", "", "world name")

	// governor stop flags
	governorStopCmd.Flags().StringVar(&governorStopWorld, "world", "", "world name")

	// governor restart flags
	governorRestartCmd.Flags().StringVar(&governorRestartWorld, "world", "", "world name")

	// governor attach flags
	governorAttachCmd.Flags().StringVar(&governorAttachWorld, "world", "", "world name")

	// governor brief flags
	governorBriefCmd.Flags().StringVar(&governorBriefWorld, "world", "", "world name")

	// governor debrief flags
	governorDebriefCmd.Flags().StringVar(&governorDebriefWorld, "world", "", "world name")

	// governor summary flags
	governorSummaryCmd.Flags().StringVar(&governorSummaryWorld, "world", "", "world name")

	// governor sync flags
	governorSyncCmd.Flags().StringVar(&governorSyncWorld, "world", "", "world name")

	// governor status flags
	governorStatusCmd.Flags().StringVar(&governorStatusWorld, "world", "", "world name")
	governorStatusCmd.Flags().Bool("json", false, "output as JSON")
}
