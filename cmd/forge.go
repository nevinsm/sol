package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var forgeCmd = &cobra.Command{
	Use:   "forge",
	Short: "Manage the merge pipeline forge",
}

var forgeStartCmd = &cobra.Command{
	Use:   "start <world>",
	Short: "Start the forge as a Claude session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return err
		}

		sessName := dispatch.SessionName(world, "forge")
		mgr := session.New()

		// Check if already running.
		if mgr.Exists(sessName) {
			return fmt.Errorf("forge already running for world %q (session %s)", world, sessName)
		}

		sourceRepo, err := dispatch.ResolveSourceRepo(worldCfg)
		if err != nil {
			return err
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		cfg, err := resolveForgeConfig(world, worldCfg)
		if err != nil {
			return err
		}

		logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		ref := forge.New(world, sourceRepo, worldStore, sphereStore, cfg, logger)

		// 1. Ensure worktree exists.
		if err := ref.EnsureWorktree(); err != nil {
			return fmt.Errorf("failed to ensure worktree: %w", err)
		}

		// 2. Register agent in sphere store.
		_, err = sphereStore.GetAgent(world + "/forge")
		if err != nil {
			if _, err := sphereStore.CreateAgent("forge", world, "forge"); err != nil {
				return fmt.Errorf("failed to register forge agent: %w", err)
			}
		}

		// 3. Install forge CLAUDE.md.
		rctx := protocol.ForgeClaudeMDContext{
			World:        world,
			TargetBranch: cfg.TargetBranch,
			WorktreeDir:  ref.WorktreeDir(),
			QualityGates: cfg.QualityGates,
		}
		if err := protocol.InstallForgeClaudeMD(ref.WorktreeDir(), rctx); err != nil {
			return fmt.Errorf("failed to install forge CLAUDE.md: %w", err)
		}

		// 4. Install Claude Code hooks.
		if err := protocol.InstallHooks(ref.WorktreeDir(), world, "forge"); err != nil {
			return fmt.Errorf("failed to install hooks: %w", err)
		}

		// 5. Start tmux session with claude.
		env := map[string]string{
			"SOL_HOME":  config.Home(),
			"SOL_WORLD": world,
			"SOL_AGENT": "forge",
		}
		if err := mgr.Start(sessName, ref.WorktreeDir(),
			"claude --dangerously-skip-permissions", env, "forge", world); err != nil {
			return fmt.Errorf("failed to start forge session: %w", err)
		}

		fmt.Printf("Forge started for world %q (Claude session)\n", world)
		fmt.Printf("  Session:  %s\n", sessName)
		fmt.Printf("  Worktree: %s\n", ref.WorktreeDir())
		fmt.Printf("  Attach:   sol forge attach %s\n", world)
		return nil
	},
}

var forgeStopCmd = &cobra.Command{
	Use:   "stop <world>",
	Short: "Stop the forge",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		sessName := dispatch.SessionName(world, "forge")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no forge running for world %q", world)
		}

		if err := mgr.Stop(sessName, false); err != nil {
			return fmt.Errorf("failed to stop forge: %w", err)
		}

		fmt.Printf("Forge stopped for world %q\n", world)
		return nil
	},
}

var forgeAttachCmd = &cobra.Command{
	Use:   "attach <world>",
	Short: "Attach to the forge tmux session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		sessName := dispatch.SessionName(world, "forge")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no forge session for world %q (run 'sol forge start %s' first)", world, world)
		}

		return mgr.Attach(sessName)
	},
}

var forgeQueueJSON bool

var forgeQueueCmd = &cobra.Command{
	Use:   "queue <world>",
	Short: "Show the merge request queue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		mrs, err := worldStore.ListMergeRequests("")
		if err != nil {
			return err
		}

		if forgeQueueJSON {
			return printJSON(mrs)
		}

		printQueue(world, mrs)
		return nil
	},
}

// resolveForgeConfig builds a forge.Config from WorldConfig with flat file
// fallback for quality gates. Shared by forgeStartCmd and openForge.
func resolveForgeConfig(world string, worldCfg config.WorldConfig) (forge.Config, error) {
	cfg := forge.DefaultConfig()
	if len(worldCfg.Forge.QualityGates) > 0 {
		cfg.QualityGates = worldCfg.Forge.QualityGates
	} else {
		gatesPath := filepath.Join(config.WorldDir(world), "forge", "quality-gates.txt")
		gates, err := forge.LoadQualityGates(gatesPath, cfg.QualityGates)
		if err != nil {
			return cfg, fmt.Errorf("failed to load quality gates: %w", err)
		}
		cfg.QualityGates = gates
	}
	if worldCfg.Forge.TargetBranch != "" {
		cfg.TargetBranch = worldCfg.Forge.TargetBranch
	}
	if worldCfg.Forge.GateTimeout != "" {
		cfg.GateTimeout = worldCfg.Forge.GateTimeout
	}
	return cfg, nil
}

// --- Toolbox subcommands (backing Claude's forge operations) ---

// openForge is a helper to create a Forge for toolbox subcommands.
func openForge(world string) (*forge.Forge, *store.Store, *store.Store, error) {
	worldCfg, err := config.LoadWorldConfig(world)
	if err != nil {
		return nil, nil, nil, err
	}

	worldStore, err := store.OpenWorld(world)
	if err != nil {
		return nil, nil, nil, err
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		worldStore.Close()
		return nil, nil, nil, err
	}

	sourceRepo, err := dispatch.ResolveSourceRepo(worldCfg)
	if err != nil {
		worldStore.Close()
		sphereStore.Close()
		return nil, nil, nil, err
	}

	cfg, err := resolveForgeConfig(world, worldCfg)
	if err != nil {
		worldStore.Close()
		sphereStore.Close()
		return nil, nil, nil, err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ref := forge.New(world, sourceRepo, worldStore, sphereStore, cfg, logger)
	return ref, worldStore, sphereStore, nil
}

var forgeReadyCmd = &cobra.Command{
	Use:   "ready <world>",
	Short: "List ready (unblocked) merge requests",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		mrs, err := ref.ListReady()
		if err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printJSON(mrs)
		}

		if len(mrs) == 0 {
			fmt.Println("No ready merge requests")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tWORK ITEM\tBRANCH\tPRIORITY\tATTEMPTS\n")
		for _, mr := range mrs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\n",
				mr.ID, mr.WorkItemID, mr.Branch, mr.Priority, mr.Attempts)
		}
		tw.Flush()
		return nil
	},
}

var forgeBlockedCmd = &cobra.Command{
	Use:   "blocked <world>",
	Short: "List blocked merge requests",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		mrs, err := ref.ListBlocked()
		if err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printJSON(mrs)
		}

		if len(mrs) == 0 {
			fmt.Println("No blocked merge requests")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tWORK ITEM\tBRANCH\tBLOCKED BY\n")
		for _, mr := range mrs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				mr.ID, mr.WorkItemID, mr.Branch, mr.BlockedBy)
		}
		tw.Flush()
		return nil
	},
}

var forgeClaimCmd = &cobra.Command{
	Use:   "claim <world>",
	Short: "Claim the next ready unblocked merge request",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		mr, err := ref.Claim()
		if err != nil {
			return err
		}
		jsonOut, _ := cmd.Flags().GetBool("json")
		if mr == nil {
			if jsonOut {
				fmt.Println("null")
			} else {
				fmt.Println("No ready merge requests to claim")
			}
			return nil
		}

		eventLog := events.NewLogger(config.Home())
		eventLog.Emit(events.EventMergeClaimed, "forge", "forge", "both", map[string]string{
			"merge_request_id": mr.ID,
			"work_item_id":     mr.WorkItemID,
			"branch":           mr.Branch,
		})

		if jsonOut {
			return printJSON(mr)
		}

		fmt.Printf("Claimed: %s\n", mr.ID)
		fmt.Printf("  Work item: %s\n", mr.WorkItemID)
		fmt.Printf("  Branch:    %s\n", mr.Branch)
		fmt.Printf("  Priority:  %d\n", mr.Priority)
		fmt.Printf("  Attempts:  %d\n", mr.Attempts)
		return nil
	},
}

var forgeReleaseCmd = &cobra.Command{
	Use:   "release <world> <mr-id>",
	Short: "Release a claimed merge request back to ready",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		world, mrID := args[0], args[1]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		if err := ref.Release(mrID); err != nil {
			return err
		}

		fmt.Printf("Released: %s\n", mrID)
		return nil
	},
}

var forgeRunGatesCmd = &cobra.Command{
	Use:          "run-gates <world>",
	Short:        "Run quality gates in the forge worktree",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		results, err := ref.RunGates()
		if err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printJSON(results)
		}

		allPassed := true
		for _, r := range results {
			status := "PASS"
			if !r.Passed {
				status = "FAIL"
				allPassed = false
			}
			fmt.Printf("[%s] %s (%s)\n", status, r.Gate, r.Elapsed.Round(time.Millisecond))
			if !r.Passed && r.Output != "" {
				fmt.Printf("  Output: %s\n", r.Output)
			}
		}

		if !allPassed {
			return &exitError{code: 1}
		}
		return nil
	},
}

var forgePushCmd = &cobra.Command{
	Use:   "push <world>",
	Short: "Push HEAD to target branch (acquires merge slot)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		if err := ref.Push(); err != nil {
			return err
		}

		fmt.Printf("Pushed HEAD to %s\n", ref.TargetBranch())
		return nil
	},
}

var forgeMarkMergedCmd = &cobra.Command{
	Use:   "mark-merged <world> <mr-id>",
	Short: "Mark a merge request as merged",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		world, mrID := args[0], args[1]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		if err := ref.MarkMerged(mrID); err != nil {
			return err
		}

		eventLog := events.NewLogger(config.Home())
		eventLog.Emit(events.EventMerged, "forge", "forge", "both", map[string]string{
			"merge_request_id": mrID,
		})

		fmt.Printf("Merged: %s\n", mrID)
		return nil
	},
}

var forgeMarkFailedCmd = &cobra.Command{
	Use:   "mark-failed <world> <mr-id>",
	Short: "Mark a merge request as failed",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		world, mrID := args[0], args[1]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		if err := ref.MarkFailed(mrID); err != nil {
			return err
		}

		eventLog := events.NewLogger(config.Home())
		eventLog.Emit(events.EventMergeFailed, "forge", "forge", "both", map[string]string{
			"merge_request_id": mrID,
		})

		fmt.Printf("Failed: %s\n", mrID)
		return nil
	},
}

var forgeCreateResolutionCmd = &cobra.Command{
	Use:   "create-resolution <world> <mr-id>",
	Short: "Create a conflict resolution task and block the MR",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		world, mrID := args[0], args[1]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		mr, err := ref.GetMergeRequest(mrID)
		if err != nil {
			return err
		}

		taskID, err := ref.CreateResolutionTask(mr)
		if err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printJSON(map[string]string{
				"mr_id":   mrID,
				"task_id": taskID,
			})
		}

		fmt.Printf("Created resolution task: %s\n", taskID)
		fmt.Printf("  MR %s is now blocked\n", mrID)
		return nil
	},
}

var forgeCheckUnblockedCmd = &cobra.Command{
	Use:   "check-unblocked <world>",
	Short: "Check for resolved blockers and unblock MRs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		unblocked, err := ref.CheckUnblocked()
		if err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printJSON(unblocked)
		}

		if len(unblocked) == 0 {
			fmt.Println("No MRs unblocked")
		} else {
			for _, id := range unblocked {
				fmt.Printf("Unblocked: %s\n", id)
			}
		}
		return nil
	},
}

func printQueue(world string, mrs []store.MergeRequest) {
	if len(mrs) == 0 {
		fmt.Printf("Merge Queue: %s (empty)\n", world)
		return
	}

	fmt.Printf("Merge Queue: %s (%d items)\n\n", world, len(mrs))

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\tWORK ITEM\tBRANCH\tPHASE\tBLOCKED BY\tATTEMPTS\n")
	for _, mr := range mrs {
		blocked := ""
		if mr.BlockedBy != "" {
			blocked = mr.BlockedBy
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\n",
			mr.ID, mr.WorkItemID, mr.Branch, mr.Phase, blocked, mr.Attempts)
	}
	tw.Flush()

	// Summary counts.
	counts := map[string]int{}
	blockedCount := 0
	for _, mr := range mrs {
		counts[mr.Phase]++
		if mr.BlockedBy != "" {
			blockedCount++
		}
	}
	fmt.Printf("\nSummary: %d ready, %d blocked, %d in progress, %d merged\n",
		counts["ready"]-blockedCount, blockedCount, counts["claimed"], counts["merged"])
}

func init() {
	rootCmd.AddCommand(forgeCmd)
	forgeCmd.AddCommand(forgeStartCmd)
	forgeCmd.AddCommand(forgeStopCmd)
	forgeCmd.AddCommand(forgeQueueCmd)
	forgeCmd.AddCommand(forgeAttachCmd)
	forgeCmd.AddCommand(forgeReadyCmd)
	forgeCmd.AddCommand(forgeBlockedCmd)
	forgeCmd.AddCommand(forgeClaimCmd)
	forgeCmd.AddCommand(forgeReleaseCmd)
	forgeCmd.AddCommand(forgeRunGatesCmd)
	forgeCmd.AddCommand(forgePushCmd)
	forgeCmd.AddCommand(forgeMarkMergedCmd)
	forgeCmd.AddCommand(forgeMarkFailedCmd)
	forgeCmd.AddCommand(forgeCreateResolutionCmd)
	forgeCmd.AddCommand(forgeCheckUnblockedCmd)

	// --json flag for commands that support it.
	forgeQueueCmd.Flags().BoolVar(&forgeQueueJSON, "json", false, "output as JSON")
	for _, cmd := range []*cobra.Command{
		forgeReadyCmd, forgeBlockedCmd, forgeClaimCmd,
		forgeRunGatesCmd, forgeCreateResolutionCmd, forgeCheckUnblockedCmd,
	} {
		cmd.Flags().Bool("json", false, "output as JSON")
	}
}
