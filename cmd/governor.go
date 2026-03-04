package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/governor"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/worldsync"
	"github.com/spf13/cobra"
)

var governorCmd = &cobra.Command{
	Use:   "governor",
	Short: "Manage the per-world governor coordinator",
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

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := session.New()

		// Install governor CLAUDE.md before starting session.
		govDir := governor.GovernorDir(world)
		if err := os.MkdirAll(govDir, 0o755); err != nil {
			return fmt.Errorf("failed to create governor directory: %w", err)
		}
		if err := protocol.InstallGovernorClaudeMD(govDir, protocol.GovernorClaudeMDContext{
			World:     world,
			SolBinary: "sol",
			MirrorDir: "../repo",
		}); err != nil {
			return fmt.Errorf("failed to install governor CLAUDE.md: %w", err)
		}

		if err := governor.Start(governor.StartOpts{
			World: world,
		}, sphereStore, mgr); err != nil {
			return err
		}

		fmt.Printf("Started governor for world %q\n", world)
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

func init() {
	rootCmd.AddCommand(governorCmd)
	governorCmd.AddCommand(governorStartCmd, governorStopCmd, governorAttachCmd,
		governorBriefCmd, governorDebriefCmd,
		governorSummaryCmd, governorSyncCmd)

	// governor start flags
	governorStartCmd.Flags().StringVar(&governorStartWorld, "world", "", "world name")

	// governor stop flags
	governorStopCmd.Flags().StringVar(&governorStopWorld, "world", "", "world name")

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
}
