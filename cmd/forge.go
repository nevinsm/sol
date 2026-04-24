package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	cliforge "github.com/nevinsm/sol/internal/cliapi/forge"
	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/daemon"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/worldsync"
	"github.com/spf13/cobra"
)

// forgeLifecycle returns a daemon.Lifecycle for the forge daemon of the given
// world. The PreStop hook stops the ephemeral merge session before the forge
// process is killed — this mirrors the behavior previously inlined in
// forgeStopCmd / forgeRestartCmd.
func forgeLifecycle(world string) daemon.Lifecycle {
	return daemon.Lifecycle{
		Name:    "forge[" + world + "]",
		PIDPath: func() string { return forge.PIDPath(world) },
		RunArgs: []string{"forge", "run", "--world=" + world},
		LogPath: func() string { return forge.LogPath(world) },
		PreStop: func() error {
			mergeSess := config.SessionName(world, "forge-merge")
			mgr := session.New()
			if err := mgr.Stop(mergeSess, true); err != nil {
				if !errors.Is(err, session.ErrNotFound) {
					return fmt.Errorf("failed to stop merge session: %w", err)
				}
			}
			return nil
		},
	}
}

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
	forgeRunWorld            string
	forgeLogWorld            string
	forgeLogFollow           bool
	forgeStatusWorld         string
)

var forgeCmd = &cobra.Command{
	Use:     "forge",
	Short:   "Manage the merge pipeline forge",
	GroupID: groupProcesses,
}

var forgeStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the forge as a background process",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeStartWorld)
		if err != nil {
			return err
		}

		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return fmt.Errorf("failed to load world config: %w", err)
		}

		if worldCfg.World.Sleeping {
			return fmt.Errorf("world %q is sleeping (wake it with 'sol world wake %s')", world, world)
		}

		sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
		if err != nil {
			return fmt.Errorf("failed to resolve source repo: %w", err)
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer worldStore.Close()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		cfg, err := resolveForgeConfig(world, worldCfg)
		if err != nil {
			return fmt.Errorf("failed to resolve forge config: %w", err)
		}

		logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		ref := forge.New(world, sourceRepo, worldStore, sphereStore, cfg, logger)

		// Ensure worktree exists before spawning the child so the run command
		// can land inside it.
		if err := ref.EnsureWorktree(); err != nil {
			return fmt.Errorf("failed to ensure worktree: %w", err)
		}

		lc := forgeLifecycle(world)
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		res, err := daemon.Start(lc)
		if err != nil {
			return err
		}
		switch res.Status {
		case "running":
			fmt.Printf("Forge already running for world %q (pid %d)\n", world, res.PID)
		case "started":
			fmt.Printf("Forge started for world %q (pid %d)\n", world, res.PID)
			fmt.Printf("  Worktree: %s\n", ref.WorktreeDir())
			fmt.Printf("  Log:      sol forge log --world=%s --follow\n", world)
		}
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
		// daemon.Stop invokes the PreStop hook which tears down the active
		// merge session before SIGTERM — see forgeLifecycle.
		if err := daemon.Stop(forgeLifecycle(world)); err != nil {
			return fmt.Errorf("failed to stop forge: %w", err)
		}
		fmt.Printf("Forge stopped for world %q\n", world)
		return nil
	},
}

var forgeRestartWorld string

var forgeRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the forge (stop then start)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeRestartWorld)
		if err != nil {
			return err
		}
		lc := forgeLifecycle(world)
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		if err := daemon.Restart(lc); err != nil {
			return err
		}
		fmt.Printf("Forge restarted for world %q\n", world)
		return nil
	},
}

var forgeAttachCmd = &cobra.Command{
	Use:          "attach",
	Short:        "Attach to the forge merge session (if active)",
	Long: `Attach to the ephemeral forge merge session (sol-{world}-forge-merge).

The forge process itself runs as a direct background process (not in tmux).
Use 'sol forge log --follow' to watch forge output. This command attaches
to the merge session, which only exists while a merge is in progress.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeAttachWorld)
		if err != nil {
			return err
		}

		mergeSessName := config.SessionName(world, "forge-merge")
		mgr := session.New()

		if !mgr.Exists(mergeSessName) {
			return fmt.Errorf("no active merge session for world %q (merge session only exists during merge execution)\nUse 'sol forge log --world=%s --follow' to watch forge output", world, world)
		}

		return mgr.Attach(mergeSessName)
	},
}

var forgeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show forge health summary",
	Long: `Show whether the forge process is running and its merge queue health.

Exit codes:
  0 - Forge is running
  1 - Forge is not running`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeStatusWorld)
		if err != nil {
			return err
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer worldStore.Close()

		// Check forge process via PID file.
		pid := forge.ReadPID(world)
		running := pid > 0 && forge.IsRunning(pid)

		// Load all MRs for summary.
		mrs, err := worldStore.ListMergeRequests("")
		if err != nil {
			return fmt.Errorf("failed to list merge requests: %w", err)
		}

		// Check pause state.
		paused := forge.IsForgePaused(world)

		// Check if a merge session is active.
		mergeSessName := config.SessionName(world, "forge-merge")
		mgr := session.New()
		merging := mgr.Exists(mergeSessName)

		// Build summary.
		summary := cliforge.ForgeStatusResponse{
			World:   world,
			Running: running,
			Paused:  paused,
			PID:     pid,
			Merging: merging,
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
				summary.ClaimedMR = &cliforge.ForgeStatusMR{
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
					summary.LastFailure = &cliforge.ForgeStatusEvent{
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
						summary.LastMerge = &cliforge.ForgeStatusEvent{
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
			if err := printJSON(summary); err != nil {
				return err
			}
		} else {
			printForgeStatus(summary)
		}
		if !running {
			return &exitError{code: 1}
		}
		return nil
	},
}

func printForgeStatus(s cliforge.ForgeStatusResponse) {
	fmt.Printf("Forge: %s\n\n", s.World)

	// Process state.
	if s.Running {
		if s.Paused {
			fmt.Printf("  Process:  paused (pid %d)\n", s.PID)
		} else {
			fmt.Printf("  Process:  running (pid %d)\n", s.PID)
		}
		if s.Merging {
			fmt.Printf("  Merge:    active\n")
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

var (
	forgeQueueJSON   bool
	forgeQueueAll    bool
	forgeQueueStatus string
)

// defaultQueueStatuses is the active-status filter applied by `sol forge queue`
// when neither --all nor --status is provided. Merged MRs are intentionally
// excluded — use `sol forge history` to browse historical merges.
var defaultQueueStatuses = []string{store.MRReady, store.MRClaimed, store.MRFailed}

var forgeQueueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Show the active merge request queue",
	Long: `Show the active merge request queue.

By default, only active MRs are shown (status: ready, claimed, failed). Merged
MRs are excluded — use 'sol forge history' to browse historical merges. Pass
--all to include merged MRs in the listing, or --status to filter to an
explicit comma-separated set of statuses (ready,claimed,failed,merged,superseded).`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeQueueWorld)
		if err != nil {
			return err
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer worldStore.Close()

		mrs, err := worldStore.ListMergeRequests("")
		if err != nil {
			return fmt.Errorf("failed to list merge requests: %w", err)
		}

		mrs = filterQueueByStatus(mrs, forgeQueueAll, forgeQueueStatus)

		if forgeQueueJSON {
			out := make([]cliforge.MergeRequest, len(mrs))
			for i, mr := range mrs {
				out[i] = cliforge.FromStoreMR(mr)
			}
			return printJSON(out)
		}

		printMRTable(world, "Merge Queue", mrs, time.Now())
		return nil
	},
}

var (
	forgeHistoryWorld string
	forgeHistorySince string
	forgeHistoryUntil string
	forgeHistoryLimit int
	forgeHistoryJSON  bool
)

var forgeHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show historical (merged) merge requests",
	Long: `List merge requests that have been merged, ordered newest-first.

By default the most recent 20 merges are shown. Use --since/--until to bound
the time range; both accept relative durations ('7d', '24h', '30m') and
absolute dates ('2026-04-01') or full RFC3339 timestamps, matching the syntax
of 'sol cost --since'. --limit caps the number of rows returned.

Use 'sol forge queue' for active (non-merged) merge requests.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeHistoryWorld)
		if err != nil {
			return err
		}

		var since, until *time.Time
		if forgeHistorySince != "" {
			t, err := parseSinceFlag(forgeHistorySince)
			if err != nil {
				return err
			}
			since = &t
		}
		if forgeHistoryUntil != "" {
			t, err := parseSinceFlag(forgeHistoryUntil)
			if err != nil {
				return fmt.Errorf("invalid --until: %w", err)
			}
			until = &t
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer worldStore.Close()

		mrs, err := worldStore.ListMergeRequests(store.MRMerged)
		if err != nil {
			return fmt.Errorf("failed to list merged merge requests: %w", err)
		}

		mrs = filterHistory(mrs, since, until, forgeHistoryLimit)

		if forgeHistoryJSON {
			out := make([]cliforge.MergeRequest, len(mrs))
			for i, mr := range mrs {
				out[i] = cliforge.FromStoreMR(mr)
			}
			return printJSON(out)
		}

		printMRTable(world, "Merge History", mrs, time.Now())
		return nil
	},
}

// filterQueueByStatus applies the `sol forge queue` status filter semantics:
//
//   - If all=true, return mrs unchanged.
//   - Else, if status is non-empty, parse it as a comma-separated list and
//     keep MRs whose Phase is in the set.
//   - Else, keep MRs whose Phase is in defaultQueueStatuses.
func filterQueueByStatus(mrs []store.MergeRequest, all bool, status string) []store.MergeRequest {
	if all {
		return mrs
	}
	set := map[string]bool{}
	if status != "" {
		for _, s := range strings.Split(status, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				set[s] = true
			}
		}
	} else {
		for _, s := range defaultQueueStatuses {
			set[s] = true
		}
	}
	filtered := mrs[:0:0]
	for _, mr := range mrs {
		if set[mr.Phase] {
			filtered = append(filtered, mr)
		}
	}
	return filtered
}

// filterHistory sorts newest-first, applies since/until bounds on the history
// timestamp (MergedAt fallback UpdatedAt), and trims to limit. limit<=0 means
// no limit.
func filterHistory(mrs []store.MergeRequest, since, until *time.Time, limit int) []store.MergeRequest {
	sorted := append([]store.MergeRequest(nil), mrs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return historyTimestamp(sorted[i]).After(historyTimestamp(sorted[j]))
	})
	if since != nil || until != nil {
		filtered := sorted[:0:0]
		for _, mr := range sorted {
			ts := historyTimestamp(mr)
			if since != nil && ts.Before(*since) {
				continue
			}
			if until != nil && ts.After(*until) {
				continue
			}
			filtered = append(filtered, mr)
		}
		sorted = filtered
	}
	if limit > 0 && len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

// historyTimestamp returns the best-available "when did this merge land"
// timestamp for an MR: MergedAt if set, otherwise UpdatedAt.
func historyTimestamp(mr store.MergeRequest) time.Time {
	if mr.MergedAt != nil {
		return *mr.MergedAt
	}
	return mr.UpdatedAt
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
	if worldCfg.World.Branch != "" {
		cfg.TargetBranch = worldCfg.World.Branch
	}
	return cfg, nil
}

// --- Toolbox subcommands (backing Claude's forge operations) ---

// openForge is a helper to create a Forge for toolbox subcommands.
// Callers must defer Close() on the returned stores.
func openForge(world string) (*forge.Forge, *store.WorldStore, *store.SphereStore, error) {
	worldCfg, err := config.LoadWorldConfig(world)
	if err != nil {
		return nil, nil, nil, err
	}

	worldStore, err := store.OpenWorld(world)
	if err != nil {
		return nil, nil, nil, err
	}

	success := false
	defer func() {
		if !success {
			worldStore.Close()
		}
	}()

	sphereStore, err := store.OpenSphere()
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() {
		if !success {
			sphereStore.Close()
		}
	}()

	sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
	if err != nil {
		return nil, nil, nil, err
	}

	cfg, err := resolveForgeConfig(world, worldCfg)
	if err != nil {
		return nil, nil, nil, err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ref := forge.New(world, sourceRepo, worldStore, sphereStore, cfg, logger)
	success = true
	return ref, worldStore, sphereStore, nil
}

var forgeReadyCmd = &cobra.Command{
	Use:          "ready",
	Short:        "List ready (unblocked) merge requests",
	Hidden:       true,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeReadyWorld)
		if err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return fmt.Errorf("failed to open forge: %w", err)
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		mrs, err := ref.ListReady()
		if err != nil {
			return fmt.Errorf("failed to list ready merge requests: %w", err)
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
	Hidden:       true,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeBlockedWorld)
		if err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return fmt.Errorf("failed to open forge: %w", err)
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		mrs, err := ref.ListBlocked()
		if err != nil {
			return fmt.Errorf("failed to list blocked merge requests: %w", err)
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
	Hidden:       true,
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
			return fmt.Errorf("failed to open forge: %w", err)
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		mr, err := ref.Claim()
		if err != nil {
			return fmt.Errorf("failed to claim merge request: %w", err)
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
	Use:    "release <mr-id>",
	Short:  "Release a claimed merge request back to ready",
	Hidden: true,
	Long: `Release a claimed merge request, returning it to "ready" state for re-attempt.

When forge claims an MR for processing, it transitions the MR to "claimed" state.
If processing fails or is interrupted, "release" returns the MR to "ready" so it
can be dispatched again on the next forge cycle.

If the MR has exhausted its maximum attempt count, it is permanently marked
"failed" instead of being returned to "ready". In this case the command prints
a failure message and exits 1 so callers can distinguish the two outcomes.

Exit codes:
  0  MR returned to "ready" state (will be retried)
  1  MR permanently failed (max attempts exceeded, will not be retried)`,
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
			return fmt.Errorf("failed to open forge: %w", err)
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		failed, err := ref.Release(mrID)
		if err != nil {
			return fmt.Errorf("failed to release merge request: %w", err)
		}

		if failed {
			fmt.Printf("Failed (max attempts exceeded): %s\n", mrID)
			return &exitError{code: 1}
		}
		fmt.Printf("Released: %s\n", mrID)
		return nil
	},
}

var forgeMarkMergedCmd = &cobra.Command{
	Use:          "mark-merged <mr-id>",
	Short:        "Mark a merge request as merged",
	Hidden:       true,
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
			return fmt.Errorf("failed to open forge: %w", err)
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		if err := ref.MarkMerged(mrID); err != nil {
			return fmt.Errorf("failed to mark merge request as merged: %w", err)
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
	Hidden:       true,
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
			return fmt.Errorf("failed to open forge: %w", err)
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		// Look up MR before MarkFailed to capture writ_id for the event.
		mr, mrErr := ref.GetMergeRequest(mrID)

		if err := ref.MarkFailed(mrID); err != nil {
			return fmt.Errorf("failed to mark merge request as failed: %w", err)
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
	Use:    "create-resolution <mr-id>",
	Short:  "Create a conflict resolution task and block the MR",
	Hidden: true,
	Long: `Create a resolution writ for a merge request that has conflicts, then block
the MR until the resolution is complete. Attempts to auto-dispatch the
resolution writ to an idle agent immediately.

Used by the forge session when it encounters merge conflicts that need
manual resolution.`,
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
			return fmt.Errorf("failed to open forge: %w", err)
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		mr, err := ref.GetMergeRequest(mrID)
		if err != nil {
			return fmt.Errorf("failed to get merge request: %w", err)
		}

		taskID, err := ref.CreateResolutionTask(mr)
		if err != nil {
			return fmt.Errorf("failed to create resolution task: %w", err)
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
			result, err := dispatch.Cast(cmd.Context(), dispatch.CastOpts{
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
	Hidden:       true,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeCheckUnblockedWorld)
		if err != nil {
			return err
		}

		ref, worldStore, sphereStore, err := openForge(world)
		if err != nil {
			return fmt.Errorf("failed to open forge: %w", err)
		}
		defer worldStore.Close()
		defer sphereStore.Close()

		unblocked, err := ref.CheckUnblocked()
		if err != nil {
			return fmt.Errorf("failed to check unblocked merge requests: %w", err)
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
			return fmt.Errorf("failed to load world config: %w", err)
		}

		cfg, err := resolveForgeConfig(world, worldCfg)
		if err != nil {
			return fmt.Errorf("failed to resolve forge config: %w", err)
		}

		// Sync managed repo first.
		outcome, err := worldsync.SyncRepo(world)
		if err != nil {
			return fmt.Errorf("failed to sync managed repo: %w", err)
		}

		// Sync forge worktree.
		if err := worldsync.SyncForge(world, cfg.TargetBranch); err != nil {
			return fmt.Errorf("failed to sync forge worktree: %w", err)
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printJSON(cliforge.ForgeSyncResponse{
				World:      world,
				Fetched:    outcome.Advanced,
				HeadCommit: outcome.NewHead,
			})
		}
		fmt.Printf("Forge synced for world %q\n", world)
		return nil
	},
}

// printMRTable renders a merge-request list view with a shared column shape
// used by both `sol forge queue` and `sol forge history`. Columns:
//
//	ID  WRIT  BRANCH  PHASE  AGE  BLOCKED BY  ATTEMPTS
//
// AGE is a relative duration since the MR entered the queue (created_at);
// BLOCKED BY is the writ ID blocking the MR (empty → EmptyMarker). The footer
// shows a pluralised count of MRs.
func printMRTable(world, title string, mrs []store.MergeRequest, now time.Time) {
	if len(mrs) == 0 {
		fmt.Printf("%s: %s (empty)\n", title, world)
		return
	}

	fmt.Printf("%s: %s (%s)\n\n", title, world, cliformat.FormatCount(len(mrs), "MR", "MRs"))

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\tWRIT\tBRANCH\tPHASE\tAGE\tBLOCKED BY\tATTEMPTS\n")
	for _, mr := range mrs {
		blocked := mr.BlockedBy
		if blocked == "" {
			blocked = cliformat.EmptyMarker
		}
		age := cliformat.FormatRelative(mr.CreatedAt, now)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			mr.ID, mr.WritID, mr.Branch, mr.Phase, age, blocked, mr.Attempts)
	}
	tw.Flush()
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

		sessName := config.SessionName(world, "forge")
		timeout := time.Duration(forgeAwaitTimeout) * time.Second
		start := time.Now()

		// Phase 1: drain any already-pending nudges.
		messages, err := nudge.Drain(sessName)
		if err != nil {
			return fmt.Errorf("failed to drain nudges: %w", err)
		}
		if len(messages) > 0 {
			waited := time.Since(start).Seconds()
			data, _ := json.Marshal(cliforge.ForgeAwaitResponse{
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
				return fmt.Errorf("failed to drain nudges: %w", err)
			}
			if len(messages) > 0 {
				waited := time.Since(start).Seconds()
				data, _ := json.Marshal(cliforge.ForgeAwaitResponse{
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
		data, _ := json.Marshal(cliforge.ForgeAwaitResponse{
			Woke:          false,
			Messages:      []nudge.Message{},
			WaitedSeconds: math.Round(waited*10) / 10,
		})
		fmt.Println(string(data))
		return nil
	},
}

// forgeRunCmd is an internal subcommand that runs the forge patrol loop.
// It is launched by `forge start` inside a tmux session and is not user-facing.
var forgeRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run the forge patrol loop (internal — launched by forge start)",
	Hidden:       true,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeRunWorld)
		if err != nil {
			return err
		}

		// Flock-authoritative pidfile bootstrap. This is the first time forge
		// has a self-WritePID; previously only the parent wrote the forge pid
		// from forgeStartCmd, which meant two concurrent `sol forge start
		// --world=X` could race. A second instance will now exit here with a
		// clear error.
		release, err := daemon.RunBootstrap(forgeLifecycle(world))
		if err != nil {
			return fmt.Errorf("forge run: %w", err)
		}
		defer release()

		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return err
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
		mgr := session.New()
		ref := forge.New(world, sourceRepo, worldStore, sphereStore, cfg, logger, mgr)

		// Build patrol config from world config.
		pcfg := forge.DefaultPatrolConfig(world)
		if worldCfg.Forge.GateTimeout != "" {
			if parsed, parseErr := time.ParseDuration(worldCfg.Forge.GateTimeout); parseErr == nil && parsed > 0 {
				pcfg.AssessTimeout = parsed
			}
		}

		// Run the patrol loop (blocks until context is cancelled).
		ctx := cmd.Context()
		return ref.Run(ctx, pcfg)
	},
}

// forgeLogCmd shows the forge log file.
var forgeLogCmd = &cobra.Command{
	Use:          "log",
	Short:        "Show the forge log file",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeLogWorld)
		if err != nil {
			return err
		}

		logPath := forge.LogPath(world)

		if forgeLogFollow {
			// Exec tail -f (replaces this process).
			tailCmd := exec.Command("tail", "-f", logPath)
			tailCmd.Stdout = os.Stdout
			tailCmd.Stderr = os.Stderr
			return tailCmd.Run()
		}

		// Cat the log file.
		data, err := os.ReadFile(logPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("no forge log for world %q (is the forge running?)", world)
			}
			return err
		}
		fmt.Print(string(data))
		return nil
	},
}

var forgePauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause the forge — stop claiming new MRs",
	Long: `Set the forge pause flag for the world. A paused forge will not claim new
merge requests from the queue, but the forge session stays running.

Nudges the forge session so it notices the pause promptly. Resume with
sol forge resume.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgePauseWorld)
		if err != nil {
			return err
		}

		if forge.IsForgePaused(world) {
			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				return printJSON(cliforge.ForgeStatus{
					Running: forge.ReadPID(world) > 0 && forge.IsRunning(forge.ReadPID(world)),
					Paused:  true,
				})
			}
			fmt.Printf("Forge already paused for world %q\n", world)
			return nil
		}

		if err := forge.SetForgePaused(world); err != nil {
			return fmt.Errorf("failed to pause forge: %w", err)
		}

		// Nudge the forge session so it notices the pause promptly.
		sessName := config.SessionName(world, "forge")
		if err := nudge.Deliver(sessName, nudge.Message{
			Sender:   config.Autarch,
			Type:     "FORGE_PAUSED",
			Subject:  "Forge paused by autarch",
			Body:     fmt.Sprintf(`{"world":%q}`, world),
			Priority: "urgent",
		}); err != nil {
			// Best-effort — log but don't fail.
			fmt.Fprintf(os.Stderr, "warning: failed to nudge forge session: %v\n", err)
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			pid := forge.ReadPID(world)
			return printJSON(cliforge.ForgeStatus{
				Running: pid > 0 && forge.IsRunning(pid),
				Paused:  true,
			})
		}
		fmt.Printf("Forge paused for world %q\n", world)
		return nil
	},
}

var forgeResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume the forge — start claiming MRs again",
	Long: `Clear the forge pause flag and nudge the session to resume claiming merge
requests from the queue immediately.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(forgeResumeWorld)
		if err != nil {
			return err
		}

		if !forge.IsForgePaused(world) {
			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				pid := forge.ReadPID(world)
				return printJSON(cliforge.ForgeStatus{
					Running: pid > 0 && forge.IsRunning(pid),
					Paused:  false,
				})
			}
			fmt.Printf("Forge not paused for world %q\n", world)
			return nil
		}

		if err := forge.ClearForgePaused(world); err != nil {
			return fmt.Errorf("failed to resume forge: %w", err)
		}

		// Nudge the forge session so it resumes promptly.
		sessName := config.SessionName(world, "forge")
		if err := nudge.Deliver(sessName, nudge.Message{
			Sender:   config.Autarch,
			Type:     "FORGE_RESUMED",
			Subject:  "Forge resumed by autarch",
			Body:     fmt.Sprintf(`{"world":%q}`, world),
			Priority: "urgent",
		}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to nudge forge session: %v\n", err)
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			pid := forge.ReadPID(world)
			return printJSON(cliforge.ForgeStatus{
				Running: pid > 0 && forge.IsRunning(pid),
				Paused:  false,
			})
		}
		fmt.Printf("Forge resumed for world %q\n", world)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(forgeCmd)
	forgeCmd.AddCommand(forgeStartCmd)
	forgeCmd.AddCommand(forgeStopCmd)
	forgeCmd.AddCommand(forgeRestartCmd)
	forgeCmd.AddCommand(forgeSyncCmd)
	forgeCmd.AddCommand(forgeStatusCmd)
	forgeCmd.AddCommand(forgeQueueCmd)
	forgeCmd.AddCommand(forgeHistoryCmd)
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
	forgeCmd.AddCommand(forgeRunCmd)
	forgeCmd.AddCommand(forgeLogCmd)

	// --world flag for all subcommands.
	forgeStartCmd.Flags().StringVar(&forgeStartWorld, "world", "", "world name")
	forgeStopCmd.Flags().StringVar(&forgeStopWorld, "world", "", "world name")
	forgeRestartCmd.Flags().StringVar(&forgeRestartWorld, "world", "", "world name")
	forgeAttachCmd.Flags().StringVar(&forgeAttachWorld, "world", "", "world name")
	forgeQueueCmd.Flags().StringVar(&forgeQueueWorld, "world", "", "world name (defaults to $SOL_WORLD or detected from current worktree)")
	forgeQueueCmd.Flags().BoolVar(&forgeQueueAll, "all", false, "include merged MRs (default shows only active: ready, claimed, failed)")
	forgeQueueCmd.Flags().StringVar(&forgeQueueStatus, "status", "", "comma-separated status filter (ready,claimed,failed,merged,superseded); overrides the active-only default")

	forgeHistoryCmd.Flags().StringVar(&forgeHistoryWorld, "world", "", "world name (defaults to $SOL_WORLD or detected from current worktree)")
	forgeHistoryCmd.Flags().StringVar(&forgeHistorySince, "since", "", "lower bound: duration (7d, 24h) or date (2006-01-02)")
	forgeHistoryCmd.Flags().StringVar(&forgeHistoryUntil, "until", "", "upper bound: duration (7d, 24h) or date (2006-01-02)")
	forgeHistoryCmd.Flags().IntVar(&forgeHistoryLimit, "limit", 20, "maximum number of rows to return (0 = unlimited)")
	forgeHistoryCmd.Flags().BoolVar(&forgeHistoryJSON, "json", false, "output as JSON")
	forgeSyncCmd.Flags().StringVar(&forgeSyncWorld, "world", "", "world name")
	forgeSyncCmd.Flags().Bool("json", false, "output as JSON")
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
	forgePauseCmd.Flags().Bool("json", false, "output as JSON")
	forgeResumeCmd.Flags().StringVar(&forgeResumeWorld, "world", "", "world name")
	forgeResumeCmd.Flags().Bool("json", false, "output as JSON")
	forgeRunCmd.Flags().StringVar(&forgeRunWorld, "world", "", "world name")
	forgeLogCmd.Flags().StringVar(&forgeLogWorld, "world", "", "world name")
	forgeLogCmd.Flags().BoolVar(&forgeLogFollow, "follow", false, "follow the log file (like tail -f)")

	// --json flag for commands that support it.
	forgeQueueCmd.Flags().BoolVar(&forgeQueueJSON, "json", false, "output as JSON")
	forgeStatusCmd.Flags().StringVar(&forgeStatusWorld, "world", "", "world name")
	forgeStatusCmd.Flags().Bool("json", false, "output as JSON")
	for _, cmd := range []*cobra.Command{
		forgeReadyCmd, forgeBlockedCmd, forgeClaimCmd,
		forgeCreateResolutionCmd, forgeCheckUnblockedCmd,
	} {
		cmd.Flags().Bool("json", false, "output as JSON")
	}
}
