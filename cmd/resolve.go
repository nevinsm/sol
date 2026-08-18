package cmd

import (
	"fmt"

	dispatchapi "github.com/nevinsm/sol/internal/cliapi/dispatch"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	resolveWorld string
	resolveAgent string
	resolveJSON  bool
)

var resolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Signal work completion — code writs push branch and create MR; non-code writs close directly",
	Long: `Mark the current writ as done and clean up the agent's tether.

For code writs: pushes the worktree branch, creates a merge request in the
forge queue, and sets the writ to "done" (awaiting merge).

For non-code writs: closes the writ directly with no branch push.

In both cases, clears the agent's tether and returns it to idle (unless the
session is configured to stay alive for further dispatch).

If the worktree has no .resolution.md at its root when resolve runs (either
because none was written, or because one was found but is git-tracked and
therefore treated as a leaked artifact from an earlier writ rather than a
real report), resolve still succeeds but prints a one-line warning that no
resolution report was captured.

Typically called from within an agent session. Uses SOL_WORLD and SOL_AGENT
environment variables when --world and --agent are not provided.`,
	GroupID:      groupDispatch,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(resolveWorld)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(resolveAgent)
		if err != nil {
			return err
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

		mgr := dispatch.NewSessionManager()
		logger := events.NewLogger(config.Home())

		result, err := dispatch.Resolve(cmd.Context(), dispatch.ResolveOpts{
			World:     world,
			AgentName: agent,
		}, worldStore, sphereStore, mgr, logger)
		if err != nil {
			// dispatch.Resolve's errors are already fully contextualized
			// (e.g. "failed to get agent %q: ...", "no work tethered for
			// agent %q ...") — wrapping again here just double-prefixes and,
			// for the agent-lookup case specifically, misleadingly frames an
			// agent-not-found error as a writ-resolution failure before any
			// writ was ever looked up. See confirmed fix #8, sol-8d4afcfa0390dd73.
			return err
		}

		if resolveJSON {
			// Look up the writ kind for the API response.
			writ, err := worldStore.GetWrit(result.WritID)
			if err != nil {
				return fmt.Errorf("failed to look up resolved writ: %w", err)
			}
			kind := writ.Kind

			// Determine target branch from world config (only relevant for code writs).
			var targetBranch string
			if kind == "" || kind == "code" {
				cfg, err := config.LoadWorldConfig(world)
				if err == nil {
					targetBranch = cfg.World.Branch
				}
				if targetBranch == "" {
					targetBranch = "main"
				}
			}

			apiResult := dispatchapi.FromResolveResult(result, kind, targetBranch)
			if err := printJSON(apiResult); err != nil {
				return err
			}
			if result.PushFailed {
				return fmt.Errorf("push failed: writ %s left tethered for retry", result.WritID)
			}
			return nil
		}

		if result.PushFailed {
			fmt.Printf("Push failed: writ %s (%s) left tethered for retry.\n", result.WritID, result.Title)
			fmt.Printf("  Fix the push issue and run 'sol resolve' again.\n")
			return fmt.Errorf("push failed: writ %s left tethered for retry", result.WritID)
		}

		fmt.Printf("Done: %s (%s)\n", result.WritID, result.Title)
		if result.BranchName != "" {
			fmt.Printf("  Branch: %s\n", result.BranchName)
		}
		if result.MergeRequestID != "" {
			fmt.Printf("  Merge request: %s (queued)\n", result.MergeRequestID)
		}
		if result.ReportChecked && !result.ReportCaptured {
			fmt.Println("  Warning: no resolution report found (expected at worktree root as .resolution.md)")
		}
		if result.SessionKept {
			fmt.Printf("  Agent %s resolved %q — session kept alive\n", result.AgentName, result.Title)
		} else {
			fmt.Printf("  Agent %s is now idle.\n", result.AgentName)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(resolveCmd)
	resolveCmd.Flags().StringVar(&resolveWorld, "world", "", "world name")
	resolveCmd.Flags().StringVar(&resolveAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")
	resolveCmd.Flags().BoolVar(&resolveJSON, "json", false, "output as JSON")
}
