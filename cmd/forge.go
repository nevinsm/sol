package cmd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/worldsync"
	"github.com/spf13/cobra"
)

var (
	forgeStartWorld           string
	forgeStopWorld            string
	forgeAttachWorld          string
	forgeQueueWorld           string
	forgeSyncWorld            string
	forgeReadyWorld           string
	forgeBlockedWorld         string
	forgeClaimWorld           string
	forgeReleaseWorld         string
	forgeMarkMergedWorld      string
	forgeMarkFailedWorld      string
	forgeCreateResolutionWorld string
	forgeCheckUnblockedWorld  string
	forgeAwaitWorld          string
	forgeAwaitTimeout        int
	forgePauseWorld          string
	forgeResumeWorld         string
)

var forgeCmd = &cobra.Command{
	Use:     "forge",
	Short:   "Manage the merge pipeline forge",
	GroupID: groupProcesses,
}

var forgeStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the forge as a Claude session",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeStartWorld)
		if err != nil {
			return err
		}

		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return err
		}

		if worldCfg.World.Sleeping {
			return fmt.Errorf("world %q is sleeping (wake it with 'sol world wake %s')", world, world)
		}

		sessName := dispatch.SessionName(world, "forge")
		mgr := session.New()

		// Check if already running.
		if mgr.Exists(sessName) {
			return fmt.Errorf("forge already running for world %q (session %s)", world, sessName)
		}

		sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
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

		// Ensure worktree exists (must happen before Launch).
		if err := ref.EnsureWorktree(); err != nil {
			return fmt.Errorf("failed to ensure worktree: %w", err)
		}

		// Launch via startup package.
		sessName, err = startup.Launch(forge.ForgeRoleConfig(), world, "forge", startup.LaunchOpts{})
		if err != nil {
			return err
		}

		fmt.Printf("Forge started for world %q (Claude session)\n", world)
		fmt.Printf("  Session:  %s\n", sessName)
		fmt.Printf("  Worktree: %s\n", ref.WorktreeDir())
		fmt.Printf("  Attach:   sol forge attach --world=%s\n", world)
		return nil
	},
}

var forgeStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the forge",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeStopWorld)
		if err != nil {
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
	Use:          "attach",
	Short:        "Attach to the forge tmux session",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeAttachWorld)
		if err != nil {
			return err
		}

		sessName := dispatch.SessionName(world, "forge")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no forge session for world %q (run 'sol forge start --world=%s' first)", world, world)
		}

		return mgr.Attach(sessName)
	},
}

var forgeStatusCmd = &cobra.Command{
	Use:          "status <world>",
	Short:        "Show forge health summary",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
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

		// Check forge session.
		sessName := dispatch.SessionName(world, "forge")
		mgr := session.New()
		running := mgr.Exists(sessName)

		// Load all MRs for summary.
		mrs, err := worldStore.ListMergeRequests("")
		if err != nil {
			return err
		}

		// Check pause state.
		paused := forge.IsForgePaused(world)

		// Build summary.
		summary := forgeStatusSummary{
			World:       world,
			Running:     running,
			Paused:      paused,
			SessionName: sessName,
		}

		for _, mr := range mrs {
			summary.Total++
			switch mr.Phase {
			case "ready":
				if mr.BlockedBy != "" {
					summary.Blocked++
				} else {
					summary.Ready++
				}
			case "claimed":
				summary.InProgress++
				// Track claimed MR details.
				summary.ClaimedMR = &forgeStatusMR{
					ID:     mr.ID,
					Branch: mr.Branch,
				}
				if mr.ClaimedAt != nil {
					summary.ClaimedMR.Age = time.Since(*mr.ClaimedAt).Truncate(time.Second).String()
				}
				// Look up writ title.
				if item, err := worldStore.GetWrit(mr.WritID); err == nil {
					summary.ClaimedMR.WritID = item.ID
					summary.ClaimedMR.Title = item.Title
				} else {
					summary.ClaimedMR.WritID = mr.WritID
					summary.ClaimedMR.Title = "(unknown)"
				}
			case "failed":
				summary.Failed++
				// Track the most recent failure (by updated_at).
				if summary.LastFailure == nil || mr.UpdatedAt.After(summary.LastFailure.Timestamp) {
					summary.LastFailure = &forgeStatusEvent{
						MRID:      mr.ID,
						Branch:    mr.Branch,
						Timestamp: mr.UpdatedAt,
					}
					if item, err := worldStore.GetWrit(mr.WritID); err == nil {
						summary.LastFailure.Title = item.Title
					}
				}
			case "merged":
				summary.Merged++
				// Track the most recent merge (by merged_at).
				if mr.MergedAt != nil {
					if summary.LastMerge == nil || mr.MergedAt.After(summary.LastMerge.Timestamp) {
						summary.LastMerge = &forgeStatusEvent{
							MRID:      mr.ID,
							Branch:    mr.Branch,
							Timestamp: *mr.MergedAt,
						}
						if item, err := worldStore.GetWrit(mr.WritID); err == nil {
							summary.LastMerge.Title = item.Title
						}
					}
				}
			}
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printJSON(summary)
		}

		printForgeStatus(summary)
		return nil
	},
}

type forgeStatusSummary struct {
	World       string            `json:"world"`
	Running     bool              `json:"running"`
	Paused      bool              `json:"paused"`
	SessionName string            `json:"session_name"`
	Ready       int               `json:"ready"`
	Blocked     int               `json:"blocked"`
	InProgress  int               `json:"in_progress"`
	Failed      int               `json:"failed"`
	Merged      int               `json:"merged"`
	Total       int               `json:"total"`
	ClaimedMR   *forgeStatusMR    `json:"claimed_mr,omitempty"`
	LastMerge   *forgeStatusEvent `json:"last_merge,omitempty"`
	LastFailure *forgeStatusEvent `json:"last_failure,omitempty"`
}

type forgeStatusMR struct {
	ID         string `json:"id"`
	WritID string `json:"writ_id"`
	Title      string `json:"title"`
	Branch     string `json:"branch"`
	Age        string `json:"age"`
}

type forgeStatusEvent struct {
	MRID      string    `json:"mr_id"`
	Title     string    `json:"title,omitempty"`
	Branch    string    `json:"branch"`
	Timestamp time.Time `json:"timestamp"`
}

func printForgeStatus(s forgeStatusSummary) {
	fmt.Printf("Forge: %s\n\n", s.World)

	// Process state.
	if s.Running {
		if s.Paused {
			fmt.Printf("  Process:  paused (%s)\n", s.SessionName)
		} else {
			fmt.Printf("  Process:  running (%s)\n", s.SessionName)
		}
	} else {
		fmt.Printf("  Process:  stopped\n")
	}

	// Queue summary.
	fmt.Printf("  Queue:    %d ready, %d blocked, %d in-progress, %d failed, %d merged (%d total)\n",
		s.Ready, s.Blocked, s.InProgress, s.Failed, s.Merged, s.Total)

	// Currently claimed MR.
	if s.ClaimedMR != nil {
		fmt.Printf("\n  Claimed:  %s  %s\n", s.ClaimedMR.ID, s.ClaimedMR.Branch)
		fmt.Printf("            %s: %s\n", s.ClaimedMR.WritID, s.ClaimedMR.Title)
		if s.ClaimedMR.Age != "" {
			fmt.Printf("            age: %s\n", s.ClaimedMR.Age)
		}
	}

	// Last merge.
	if s.LastMerge != nil {
		ago := time.Since(s.LastMerge.Timestamp).Truncate(time.Second)
		fmt.Printf("\n  Last merge:   %s  %s (%s ago)\n", s.LastMerge.MRID, s.LastMerge.Branch, ago)
		if s.LastMerge.Title != "" {
			fmt.Printf("                %s\n", s.LastMerge.Title)
		}
	}

	// Last failure.
	if s.LastFailure != nil {
		ago := time.Since(s.LastFailure.Timestamp).Truncate(time.Second)
		fmt.Printf("\n  Last failure: %s  %s (%s ago)\n", s.LastFailure.MRID, s.LastFailure.Branch, ago)
		if s.LastFailure.Title != "" {
			fmt.Printf("                %s\n", s.LastFailure.Title)
		}
	}
}

var forgeQueueJSON bool

var forgeQueueCmd = &cobra.Command{
	Use:          "queue",
	Short:        "Show the merge request queue",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeQueueWorld)
		if err != nil {
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
		parsed, _ := time.ParseDuration(worldCfg.Forge.GateTimeout)
		if parsed > 0 {
			cfg.GateTimeout = parsed
		}
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

	sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
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
	Use:          "ready",
	Short:        "List ready (unblocked) merge requests",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeReadyWorld)
		if err != nil {
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
		fmt.Fprintf(tw, "ID\tWRIT\tBRANCH\tPRIORITY\tATTEMPTS\n")
		for _, mr := range mrs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\n",
				mr.ID, mr.WritID, mr.Branch, mr.Priority, mr.Attempts)
		}
		tw.Flush()
		return nil
	},
}

var forgeBlockedCmd = &cobra.Command{
	Use:          "blocked",
	Short:        "List blocked merge requests",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeBlockedWorld)
		if err != nil {
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
		fmt.Fprintf(tw, "ID\tWRIT\tBRANCH\tBLOCKED BY\n")
		for _, mr := range mrs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				mr.ID, mr.WritID, mr.Branch, mr.BlockedBy)
		}
		tw.Flush()
		return nil
	},
}

var forgeClaimCmd = &cobra.Command{
	Use:          "claim",
	Short:        "Claim the next ready unblocked merge request",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeClaimWorld)
		if err != nil {
			return err
		}

		if forge.IsForgePaused(world) {
			return fmt.Errorf("forge is paused for world %q (run 'sol forge resume --world=%s' to unpause)", world, world)
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
			"writ_id":     mr.WritID,
			"branch":           mr.Branch,
		})

		if jsonOut {
			return printJSON(mr)
		}

		fmt.Printf("Claimed: %s\n", mr.ID)
		fmt.Printf("  Writ: %s\n", mr.WritID)
		fmt.Printf("  Branch:    %s\n", mr.Branch)
		fmt.Printf("  Priority:  %d\n", mr.Priority)
		fmt.Printf("  Attempts:  %d\n", mr.Attempts)
		return nil
	},
}

var forgeReleaseCmd = &cobra.Command{
	Use:          "release <mr-id>",
	Short:        "Release a claimed merge request back to ready",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mrID := args[0]

		world, err := config.ResolveWorld(forgeReleaseWorld)
		if err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		failed, err := ref.Release(mrID)
		if err != nil {
			return err
		}

		if failed {
			fmt.Printf("Failed (max attempts exceeded): %s\n", mrID)
		} else {
			fmt.Printf("Released: %s\n", mrID)
		}
		return nil
	},
}

var forgeMarkMergedCmd = &cobra.Command{
	Use:          "mark-merged <mr-id>",
	Short:        "Mark a merge request as merged",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mrID := args[0]

		world, err := config.ResolveWorld(forgeMarkMergedWorld)
		if err != nil {
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
	Use:          "mark-failed <mr-id>",
	Short:        "Mark a merge request as failed",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mrID := args[0]

		world, err := config.ResolveWorld(forgeMarkFailedWorld)
		if err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		// Look up MR before MarkFailed to capture writ_id for the event.
		mr, mrErr := ref.GetMergeRequest(mrID)

		if err := ref.MarkFailed(mrID); err != nil {
			return err
		}

		payload := map[string]string{
			"merge_request_id": mrID,
			"action":           "reopened",
		}
		if mrErr == nil {
			payload["writ_id"] = mr.WritID
		}

		eventLog := events.NewLogger(config.Home())
		eventLog.Emit(events.EventMergeFailed, "forge", "forge", "both", payload)

		fmt.Printf("Failed: %s\n", mrID)
		return nil
	},
}

var forgeCreateResolutionCmd = &cobra.Command{
	Use:          "create-resolution <mr-id>",
	Short:        "Create a conflict resolution task and block the MR",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mrID := args[0]

		world, err := config.ResolveWorld(forgeCreateResolutionWorld)
		if err != nil {
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

		// Best-effort immediate dispatch of the resolution writ.
		var dispatchedAgent string
		if dispatchErr := func() error {
			worldCfg, err := config.LoadWorldConfig(world)
			if err != nil {
				return err
			}
			sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
			if err != nil {
				return err
			}
			mgr := dispatch.NewSessionManager()
			logger := events.NewLogger(config.Home())
			result, err := dispatch.Cast(dispatch.CastOpts{
				WritID:     taskID,
				World:      world,
				SourceRepo: sourceRepo,
			}, worldStore, sphereStore, mgr, logger)
			if err != nil {
				return err
			}
			dispatchedAgent = result.AgentName
			return nil
		}(); dispatchErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: created resolution writ %s but auto-dispatch failed: %v\n", taskID, dispatchErr)
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			out := map[string]string{
				"mr_id":   mrID,
				"task_id": taskID,
			}
			if dispatchedAgent != "" {
				out["dispatched_agent"] = dispatchedAgent
			}
			return printJSON(out)
		}

		fmt.Printf("Created resolution task: %s\n", taskID)
		fmt.Printf("  MR %s is now blocked\n", mrID)
		if dispatchedAgent != "" {
			fmt.Printf("Dispatched %s -> %s\n", taskID, dispatchedAgent)
		}
		return nil
	},
}

var forgeCheckUnblockedCmd = &cobra.Command{
	Use:          "check-unblocked",
	Short:        "Check for resolved blockers and unblock MRs",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeCheckUnblockedWorld)
		if err != nil {
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

var forgeSyncCmd = &cobra.Command{
	Use:          "sync",
	Short:        "Sync forge worktree: fetch origin, reset to target branch",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeSyncWorld)
		if err != nil {
			return err
		}

		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return err
		}

		cfg, err := resolveForgeConfig(world, worldCfg)
		if err != nil {
			return err
		}

		// Sync managed repo first.
		if err := worldsync.SyncRepo(world); err != nil {
			return fmt.Errorf("failed to sync managed repo: %w", err)
		}

		// Sync forge worktree.
		if err := worldsync.SyncForge(world, cfg.TargetBranch); err != nil {
			return err
		}

		fmt.Printf("Forge synced for world %q\n", world)
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
	fmt.Fprintf(tw, "ID\tWRIT\tBRANCH\tPHASE\tBLOCKED BY\tATTEMPTS\n")
	for _, mr := range mrs {
		blocked := ""
		if mr.BlockedBy != "" {
			blocked = mr.BlockedBy
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\n",
			mr.ID, mr.WritID, mr.Branch, mr.Phase, blocked, mr.Attempts)
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

// forgeAwaitResult is the JSON output of forge await.
type forgeAwaitResult struct {
	Woke          bool            `json:"woke"`
	Messages      []nudge.Message `json:"messages"`
	WaitedSeconds float64         `json:"waited_seconds"`
}

var forgeAwaitCmd = &cobra.Command{
	Use:          "await",
	Short:        "Block until a nudge arrives or timeout expires",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeAwaitWorld)
		if err != nil {
			return err
		}

		sessName := dispatch.SessionName(world, "forge")
		timeout := time.Duration(forgeAwaitTimeout) * time.Second
		start := time.Now()

		// Phase 1: drain any already-pending nudges.
		messages, err := nudge.Drain(sessName)
		if err != nil {
			return err
		}
		if len(messages) > 0 {
			waited := time.Since(start).Seconds()
			data, _ := json.Marshal(forgeAwaitResult{
				Woke:          true,
				Messages:      messages,
				WaitedSeconds: math.Round(waited*10) / 10,
			})
			fmt.Println(string(data))
			return nil
		}

		// Phase 2: poll at 1s intervals until nudge arrives or timeout.
		deadline := start.Add(timeout)
		for time.Now().Before(deadline) {
			time.Sleep(1 * time.Second)

			messages, err = nudge.Drain(sessName)
			if err != nil {
				return err
			}
			if len(messages) > 0 {
				waited := time.Since(start).Seconds()
				data, _ := json.Marshal(forgeAwaitResult{
					Woke:          true,
					Messages:      messages,
					WaitedSeconds: math.Round(waited*10) / 10,
				})
				fmt.Println(string(data))
				return nil
			}
		}

		// Timeout — no nudges arrived.
		waited := time.Since(start).Seconds()
		data, _ := json.Marshal(forgeAwaitResult{
			Woke:          false,
			Messages:      []nudge.Message{},
			WaitedSeconds: math.Round(waited*10) / 10,
		})
		fmt.Println(string(data))
		return nil
	},
}

var forgePauseCmd = &cobra.Command{
	Use:          "pause",
	Short:        "Pause the forge — stop claiming new MRs",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgePauseWorld)
		if err != nil {
			return err
		}

		if forge.IsForgePaused(world) {
			fmt.Printf("Forge already paused for world %q\n", world)
			return nil
		}

		if err := forge.SetForgePaused(world); err != nil {
			return fmt.Errorf("failed to pause forge: %w", err)
		}

		// Nudge the forge session so it notices the pause promptly.
		sessName := dispatch.SessionName(world, "forge")
		mgr := session.New()
		if mgr.Exists(sessName) {
			if err := nudge.Enqueue(sessName, nudge.Message{
				Sender:   "operator",
				Type:     "FORGE_PAUSED",
				Subject:  "Forge paused by operator",
				Body:     fmt.Sprintf(`{"world":%q}`, world),
				Priority: "urgent",
			}); err != nil {
				// Best-effort — log but don't fail.
				fmt.Fprintf(os.Stderr, "warning: failed to nudge forge session: %v\n", err)
			}
			nudge.Poke(sessName)
		}

		fmt.Printf("Forge paused for world %q\n", world)
		return nil
	},
}

var forgeResumeCmd = &cobra.Command{
	Use:          "resume",
	Short:        "Resume the forge — start claiming MRs again",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeResumeWorld)
		if err != nil {
			return err
		}

		if !forge.IsForgePaused(world) {
			fmt.Printf("Forge not paused for world %q\n", world)
			return nil
		}

		if err := forge.ClearForgePaused(world); err != nil {
			return fmt.Errorf("failed to resume forge: %w", err)
		}

		// Nudge the forge session so it resumes promptly.
		sessName := dispatch.SessionName(world, "forge")
		mgr := session.New()
		if mgr.Exists(sessName) {
			if err := nudge.Enqueue(sessName, nudge.Message{
				Sender:   "operator",
				Type:     "FORGE_RESUMED",
				Subject:  "Forge resumed by operator",
				Body:     fmt.Sprintf(`{"world":%q}`, world),
				Priority: "urgent",
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to nudge forge session: %v\n", err)
			}
			nudge.Poke(sessName)
		}

		fmt.Printf("Forge resumed for world %q\n", world)
		return nil
	},
}

func init() {
	// Register forge role config for startup.Launch and prefect respawn.
	startup.Register("forge", forge.ForgeRoleConfig())

	rootCmd.AddCommand(forgeCmd)
	forgeCmd.AddCommand(forgeStartCmd)
	forgeCmd.AddCommand(forgeStopCmd)
	forgeCmd.AddCommand(forgeSyncCmd)
	forgeCmd.AddCommand(forgeStatusCmd)
	forgeCmd.AddCommand(forgeQueueCmd)
	forgeCmd.AddCommand(forgeAttachCmd)
	forgeCmd.AddCommand(forgeReadyCmd)
	forgeCmd.AddCommand(forgeBlockedCmd)
	forgeCmd.AddCommand(forgeClaimCmd)
	forgeCmd.AddCommand(forgeReleaseCmd)
	forgeCmd.AddCommand(forgeMarkMergedCmd)
	forgeCmd.AddCommand(forgeMarkFailedCmd)
	forgeCmd.AddCommand(forgeCreateResolutionCmd)
	forgeCmd.AddCommand(forgeCheckUnblockedCmd)
	forgeCmd.AddCommand(forgeAwaitCmd)
	forgeCmd.AddCommand(forgePauseCmd)
	forgeCmd.AddCommand(forgeResumeCmd)

	// --world flag for all subcommands.
	forgeStartCmd.Flags().StringVar(&forgeStartWorld, "world", "", "world name")
	forgeStopCmd.Flags().StringVar(&forgeStopWorld, "world", "", "world name")
	forgeAttachCmd.Flags().StringVar(&forgeAttachWorld, "world", "", "world name")
	forgeQueueCmd.Flags().StringVar(&forgeQueueWorld, "world", "", "world name")
	forgeSyncCmd.Flags().StringVar(&forgeSyncWorld, "world", "", "world name")
	forgeReadyCmd.Flags().StringVar(&forgeReadyWorld, "world", "", "world name")
	forgeBlockedCmd.Flags().StringVar(&forgeBlockedWorld, "world", "", "world name")
	forgeClaimCmd.Flags().StringVar(&forgeClaimWorld, "world", "", "world name")
	forgeReleaseCmd.Flags().StringVar(&forgeReleaseWorld, "world", "", "world name")
	forgeMarkMergedCmd.Flags().StringVar(&forgeMarkMergedWorld, "world", "", "world name")
	forgeMarkFailedCmd.Flags().StringVar(&forgeMarkFailedWorld, "world", "", "world name")
	forgeCreateResolutionCmd.Flags().StringVar(&forgeCreateResolutionWorld, "world", "", "world name")
	forgeCheckUnblockedCmd.Flags().StringVar(&forgeCheckUnblockedWorld, "world", "", "world name")
	forgeAwaitCmd.Flags().StringVar(&forgeAwaitWorld, "world", "", "world name")
	forgeAwaitCmd.Flags().IntVar(&forgeAwaitTimeout, "timeout", 120, "max seconds to wait")
	forgePauseCmd.Flags().StringVar(&forgePauseWorld, "world", "", "world name")
	forgeResumeCmd.Flags().StringVar(&forgeResumeWorld, "world", "", "world name")

	// --json flag for commands that support it.
	forgeQueueCmd.Flags().BoolVar(&forgeQueueJSON, "json", false, "output as JSON")
	forgeStatusCmd.Flags().Bool("json", false, "output as JSON")
	for _, cmd := range []*cobra.Command{
		forgeReadyCmd, forgeBlockedCmd, forgeClaimCmd,
		forgeCreateResolutionCmd, forgeCheckUnblockedCmd,
	} {
		cmd.Flags().Bool("json", false, "output as JSON")
	}
}
