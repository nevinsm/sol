package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/protocol"
	"github.com/nevinsm/gt/internal/refinery"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var refineryCmd = &cobra.Command{
	Use:   "refinery",
	Short: "Manage the merge pipeline refinery",
}

var refineryStartCmd = &cobra.Command{
	Use:   "start <rig>",
	Short: "Start the refinery as a Claude session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		sessName := dispatch.SessionName(rig, "refinery")
		mgr := session.New()

		// Check if already running.
		if mgr.Exists(sessName) {
			return fmt.Errorf("refinery already running for rig %q (session %s)", rig, sessName)
		}

		sourceRepo, err := dispatch.DiscoverSourceRepo()
		if err != nil {
			return err
		}

		rigStore, err := store.OpenRig(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		cfg := refinery.DefaultConfig()
		gatesPath := filepath.Join(config.RigDir(rig), "refinery", "quality-gates.txt")
		gates, err := refinery.LoadQualityGates(gatesPath, cfg.QualityGates)
		if err != nil {
			return fmt.Errorf("failed to load quality gates: %w", err)
		}
		cfg.QualityGates = gates

		logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		ref := refinery.New(rig, sourceRepo, rigStore, townStore, cfg, logger)

		// 1. Ensure worktree exists.
		if err := ref.EnsureWorktree(); err != nil {
			return fmt.Errorf("failed to ensure worktree: %w", err)
		}

		// 2. Register agent in town store.
		_, err = townStore.GetAgent(rig + "/refinery")
		if err != nil {
			if _, err := townStore.CreateAgent("refinery", rig, "refinery"); err != nil {
				return fmt.Errorf("failed to register refinery agent: %w", err)
			}
		}

		// 3. Install refinery CLAUDE.md.
		rctx := protocol.RefineryClaudeMDContext{
			Rig:          rig,
			TargetBranch: cfg.TargetBranch,
			WorktreeDir:  ref.WorktreeDir(),
			QualityGates: cfg.QualityGates,
		}
		if err := protocol.InstallRefineryClaudeMD(ref.WorktreeDir(), rctx); err != nil {
			return fmt.Errorf("failed to install refinery CLAUDE.md: %w", err)
		}

		// 4. Install Claude Code hooks.
		if err := protocol.InstallHooks(ref.WorktreeDir(), rig, "refinery"); err != nil {
			return fmt.Errorf("failed to install hooks: %w", err)
		}

		// 5. Start tmux session with claude.
		env := map[string]string{
			"GT_HOME":  config.Home(),
			"GT_RIG":   rig,
			"GT_AGENT": "refinery",
		}
		if err := mgr.Start(sessName, ref.WorktreeDir(),
			"claude --dangerously-skip-permissions", env, "refinery", rig); err != nil {
			return fmt.Errorf("failed to start refinery session: %w", err)
		}

		fmt.Printf("Refinery started for rig %q (Claude session)\n", rig)
		fmt.Printf("  Session:  %s\n", sessName)
		fmt.Printf("  Worktree: %s\n", ref.WorktreeDir())
		fmt.Printf("  Attach:   gt refinery attach %s\n", rig)
		return nil
	},
}

var refineryStopCmd = &cobra.Command{
	Use:   "stop <rig>",
	Short: "Stop the refinery",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		sessName := dispatch.SessionName(rig, "refinery")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no refinery running for rig %q", rig)
		}

		if err := mgr.Stop(sessName, false); err != nil {
			return fmt.Errorf("failed to stop refinery: %w", err)
		}

		fmt.Printf("Refinery stopped for rig %q\n", rig)
		return nil
	},
}

var refineryAttachCmd = &cobra.Command{
	Use:   "attach <rig>",
	Short: "Attach to the refinery tmux session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		sessName := dispatch.SessionName(rig, "refinery")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no refinery session for rig %q (run 'gt refinery start %s' first)", rig, rig)
		}

		return mgr.Attach(sessName)
	},
}

var refineryQueueJSON bool

var refineryQueueCmd = &cobra.Command{
	Use:   "queue <rig>",
	Short: "Show the merge request queue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]

		rigStore, err := store.OpenRig(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()

		mrs, err := rigStore.ListMergeRequests("")
		if err != nil {
			return err
		}

		if refineryQueueJSON {
			return printJSON(mrs)
		}

		printQueue(rig, mrs)
		return nil
	},
}

// --- Toolbox subcommands (backing Claude's refinery operations) ---

// openRefinery is a helper to create a Refinery for toolbox subcommands.
func openRefinery(rig string) (*refinery.Refinery, *store.Store, *store.Store, error) {
	rigStore, err := store.OpenRig(rig)
	if err != nil {
		return nil, nil, nil, err
	}

	townStore, err := store.OpenTown()
	if err != nil {
		rigStore.Close()
		return nil, nil, nil, err
	}

	sourceRepo, err := dispatch.DiscoverSourceRepo()
	if err != nil {
		rigStore.Close()
		townStore.Close()
		return nil, nil, nil, err
	}

	cfg := refinery.DefaultConfig()
	gatesPath := filepath.Join(config.RigDir(rig), "refinery", "quality-gates.txt")
	gates, err := refinery.LoadQualityGates(gatesPath, cfg.QualityGates)
	if err != nil {
		rigStore.Close()
		townStore.Close()
		return nil, nil, nil, fmt.Errorf("failed to load quality gates: %w", err)
	}
	cfg.QualityGates = gates

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ref := refinery.New(rig, sourceRepo, rigStore, townStore, cfg, logger)
	return ref, rigStore, townStore, nil
}

var refineryToolboxJSON bool

var refineryReadyCmd = &cobra.Command{
	Use:   "ready <rig>",
	Short: "List ready (unblocked) merge requests",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		ref, rigStore, townStore, err := openRefinery(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()
		defer townStore.Close()

		mrs, err := ref.ListReady()
		if err != nil {
			return err
		}

		if refineryToolboxJSON {
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

var refineryBlockedCmd = &cobra.Command{
	Use:   "blocked <rig>",
	Short: "List blocked merge requests",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		ref, rigStore, townStore, err := openRefinery(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()
		defer townStore.Close()

		mrs, err := ref.ListBlocked()
		if err != nil {
			return err
		}

		if refineryToolboxJSON {
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

var refineryClaimCmd = &cobra.Command{
	Use:   "claim <rig>",
	Short: "Claim the next ready unblocked merge request",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		ref, rigStore, townStore, err := openRefinery(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()
		defer townStore.Close()

		mr, err := ref.Claim()
		if err != nil {
			return err
		}
		if mr == nil {
			if refineryToolboxJSON {
				fmt.Println("null")
			} else {
				fmt.Println("No ready merge requests to claim")
			}
			return nil
		}

		eventLog := events.NewLogger(config.Home())
		eventLog.Emit(events.EventMergeClaimed, "refinery", "refinery", "both", map[string]string{
			"merge_request_id": mr.ID,
			"work_item_id":     mr.WorkItemID,
			"branch":           mr.Branch,
		})

		if refineryToolboxJSON {
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

var refineryReleaseCmd = &cobra.Command{
	Use:   "release <rig> <mr-id>",
	Short: "Release a claimed merge request back to ready",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig, mrID := args[0], args[1]
		ref, rigStore, townStore, err := openRefinery(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()
		defer townStore.Close()

		if err := ref.Release(mrID); err != nil {
			return err
		}

		fmt.Printf("Released: %s\n", mrID)
		return nil
	},
}

var refineryRunGatesCmd = &cobra.Command{
	Use:   "run-gates <rig>",
	Short: "Run quality gates in the refinery worktree",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		ref, rigStore, townStore, err := openRefinery(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()
		defer townStore.Close()

		results, err := ref.RunGates()
		if err != nil {
			return err
		}

		if refineryToolboxJSON {
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
			os.Exit(1)
		}
		return nil
	},
}

var refineryPushCmd = &cobra.Command{
	Use:   "push <rig>",
	Short: "Push HEAD to target branch (acquires merge slot)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		ref, rigStore, townStore, err := openRefinery(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()
		defer townStore.Close()

		if err := ref.Push(); err != nil {
			return err
		}

		fmt.Printf("Pushed HEAD to %s\n", ref.TargetBranch())
		return nil
	},
}

var refineryMarkMergedCmd = &cobra.Command{
	Use:   "mark-merged <rig> <mr-id>",
	Short: "Mark a merge request as merged",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig, mrID := args[0], args[1]
		ref, rigStore, townStore, err := openRefinery(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()
		defer townStore.Close()

		if err := ref.MarkMerged(mrID); err != nil {
			return err
		}

		eventLog := events.NewLogger(config.Home())
		eventLog.Emit(events.EventMerged, "refinery", "refinery", "both", map[string]string{
			"merge_request_id": mrID,
		})

		fmt.Printf("Merged: %s\n", mrID)
		return nil
	},
}

var refineryMarkFailedCmd = &cobra.Command{
	Use:   "mark-failed <rig> <mr-id>",
	Short: "Mark a merge request as failed",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig, mrID := args[0], args[1]
		ref, rigStore, townStore, err := openRefinery(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()
		defer townStore.Close()

		if err := ref.MarkFailed(mrID); err != nil {
			return err
		}

		eventLog := events.NewLogger(config.Home())
		eventLog.Emit(events.EventMergeFailed, "refinery", "refinery", "both", map[string]string{
			"merge_request_id": mrID,
		})

		fmt.Printf("Failed: %s\n", mrID)
		return nil
	},
}

var refineryCreateResolutionCmd = &cobra.Command{
	Use:   "create-resolution <rig> <mr-id>",
	Short: "Create a conflict resolution task and block the MR",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig, mrID := args[0], args[1]
		ref, rigStore, townStore, err := openRefinery(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()
		defer townStore.Close()

		mr, err := ref.GetMergeRequest(mrID)
		if err != nil {
			return err
		}

		taskID, err := ref.CreateResolutionTask(mr)
		if err != nil {
			return err
		}

		if refineryToolboxJSON {
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

var refineryCheckUnblockedCmd = &cobra.Command{
	Use:   "check-unblocked <rig>",
	Short: "Check for resolved blockers and unblock MRs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		ref, rigStore, townStore, err := openRefinery(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()
		defer townStore.Close()

		unblocked, err := ref.CheckUnblocked()
		if err != nil {
			return err
		}

		if refineryToolboxJSON {
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

func printQueue(rig string, mrs []store.MergeRequest) {
	if len(mrs) == 0 {
		fmt.Printf("Merge Queue: %s (empty)\n", rig)
		return
	}

	fmt.Printf("Merge Queue: %s (%d items)\n\n", rig, len(mrs))

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
	rootCmd.AddCommand(refineryCmd)
	refineryCmd.AddCommand(refineryStartCmd)
	refineryCmd.AddCommand(refineryStopCmd)
	refineryCmd.AddCommand(refineryQueueCmd)
	refineryCmd.AddCommand(refineryAttachCmd)
	refineryCmd.AddCommand(refineryReadyCmd)
	refineryCmd.AddCommand(refineryBlockedCmd)
	refineryCmd.AddCommand(refineryClaimCmd)
	refineryCmd.AddCommand(refineryReleaseCmd)
	refineryCmd.AddCommand(refineryRunGatesCmd)
	refineryCmd.AddCommand(refineryPushCmd)
	refineryCmd.AddCommand(refineryMarkMergedCmd)
	refineryCmd.AddCommand(refineryMarkFailedCmd)
	refineryCmd.AddCommand(refineryCreateResolutionCmd)
	refineryCmd.AddCommand(refineryCheckUnblockedCmd)

	// --json flag for commands that support it.
	refineryQueueCmd.Flags().BoolVar(&refineryQueueJSON, "json", false, "output as JSON")
	for _, cmd := range []*cobra.Command{
		refineryReadyCmd, refineryBlockedCmd, refineryClaimCmd,
		refineryRunGatesCmd, refineryCreateResolutionCmd, refineryCheckUnblockedCmd,
	} {
		cmd.Flags().BoolVar(&refineryToolboxJSON, "json", false, "output as JSON")
	}
}
