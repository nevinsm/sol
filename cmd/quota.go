package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	cliquota "github.com/nevinsm/sol/internal/cliapi/quota"
	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/quota"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	quotaRotateWorld   string
	quotaRotateConfirm bool
	quotaRotateJSON    bool
)

var quotaCmd = &cobra.Command{
	Use:     "quota",
	Short:   "Manage account rate limit state",
	GroupID: groupSetup,
}

// --- sol quota scan ---

var quotaScanJSON bool

var quotaScanCmd = &cobra.Command{
	Use:          "scan",
	Short:        "Scan agent sessions for rate limit errors",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		results, err := quota.ScanWorld(world)
		if err != nil {
			return err
		}

		if quotaScanJSON {
			return printJSON(cliquota.NewScanSessions(results))
		}

		if len(results) == 0 {
			fmt.Println("No sessions found to scan.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "SESSION\tACCOUNT\tLIMITED\n")
		for _, r := range results {
			acct := r.Account
			if acct == "" {
				acct = "-"
			}
			limited := "no"
			if r.Limited {
				limited = "YES"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Session, acct, limited)
		}
		tw.Flush()
		return nil
	},
}

// --- sol quota status ---

var quotaStatusJSON bool

var quotaStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show per-account quota state",
	Long: `Show per-account quota state.

This view is sphere-wide; the --world flag is not accepted.

Columns:
  ACCOUNT     Account handle (Claude OAuth or API key name)
  STATUS      available / limited / assigned
  WINDOW      Rate-limit window (e.g. '5h' for Claude, 'rpm' for API keys)
  LIMITED AT  When the account was marked limited (RFC3339 UTC, or relative)
  RESETS AT   When the limit will reset (RFC3339 UTC, or relative)
  LAST USED   When the account was last assigned to an agent`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := quota.Load()
		if err != nil {
			return err
		}

		// Expire any limits that have passed.
		state.ExpireLimits()

		now := time.Now()

		// Stable ordering for both table and JSON output.
		handles := make([]string, 0, len(state.Accounts))
		for handle := range state.Accounts {
			handles = append(handles, handle)
		}
		sort.Strings(handles)

		if quotaStatusJSON {
			out := cliquota.StatusResponse{
				Accounts: make([]cliquota.StatusAccount, 0, len(handles)),
			}
			for _, handle := range handles {
				acct := state.Accounts[handle]
				out.Accounts = append(out.Accounts, cliquota.NewStatusAccount(handle, *acct))
			}
			return printJSON(out)
		}

		if len(state.Accounts) == 0 {
			fmt.Println("No quota state recorded.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "ACCOUNT\tSTATUS\tWINDOW\tLIMITED AT\tRESETS AT\tLAST USED\n")
		for _, handle := range handles {
			acct := state.Accounts[handle]
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				handle,
				acct.Status,
				// WINDOW source: the broker observes per-account ResetsIn on
				// detection signals, but that value is the remaining time
				// until reset (not a stable window) and AccountState has
				// no runtime field to look up the provider's known window.
				// A proper fix needs a Provider.Window() accessor plus a
				// way to associate accounts with runtimes. Tracked as
				// sol-23a1f5fc82020c84 ("Implement quota broker window
				// display").
				cliformat.EmptyMarker,
				formatQuotaTime(acct.LimitedAt, now),
				formatQuotaTime(acct.ResetsAt, now),
				formatQuotaTime(acct.LastUsed, now),
			)
		}
		tw.Flush()
		fmt.Fprintln(os.Stdout, cliformat.FormatCount(len(state.Accounts), "account", "accounts"))
		return nil
	},
}

// formatQuotaTime renders an optional timestamp using the shared
// cliformat helper. A nil pointer renders as cliformat.EmptyMarker.
func formatQuotaTime(t *time.Time, now time.Time) string {
	if t == nil {
		return cliformat.EmptyMarker
	}
	return cliformat.FormatTimestampOrRelative(*t, now)
}

// --- sol quota rotate ---

var quotaRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Rotate rate-limited agents to available accounts",
	Long: `Rotate rate-limited agents off their current account onto an available
account. By default this is a preview only — pass --confirm to actually
perform the rotation.

Exit codes:
  0 - Rotation executed successfully (--confirm), or no rotation needed
  1 - Preview mode (--confirm not provided), or an error occurred`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(quotaRotateWorld)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := session.New()
		logger := events.NewLogger(config.Home())

		dryRun := !quotaRotateConfirm

		result, err := quota.Rotate(quota.RotateOpts{
			World:  world,
			DryRun: dryRun,
		}, sphereStore, mgr, logger)
		if err != nil {
			return err
		}

		if quotaRotateJSON {
			resp := cliquota.NewRotateResponse(result, dryRun)
			if err := printJSON(resp); err != nil {
				return err
			}
			if dryRun {
				return &exitError{code: 1}
			}
			return nil
		}

		// Print expired limits.
		for _, handle := range result.Expired {
			fmt.Printf("  expired: %s (limit reset, now available)\n", handle)
		}

		// Print actions.
		if len(result.Actions) == 0 {
			fmt.Println("No rotation needed.")
			return nil
		}

		prefix := ""
		if dryRun {
			prefix = "[preview] "
		}

		for _, action := range result.Actions {
			if action.Paused {
				fmt.Printf("%s  paused: %s (no available accounts)\n", prefix, action.AgentName)
			} else {
				fmt.Printf("%s  rotated: %s  %s → %s\n", prefix, action.AgentName, action.FromAccount, action.ToAccount)
			}
		}

		rotated := 0
		paused := 0
		for _, a := range result.Actions {
			if a.Paused {
				paused++
			} else {
				rotated++
			}
		}

		fmt.Printf("%s%d rotated, %d paused\n", prefix, rotated, paused)

		if dryRun {
			fmt.Println("\nRun with --confirm to execute.")
			return &exitError{code: 1}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(quotaCmd)

	quotaCmd.AddCommand(quotaScanCmd)
	quotaScanCmd.Flags().String("world", "", "world name")
	quotaScanCmd.Flags().BoolVar(&quotaScanJSON, "json", false, "output as JSON")

	quotaCmd.AddCommand(quotaStatusCmd)
	quotaStatusCmd.Flags().BoolVar(&quotaStatusJSON, "json", false, "output as JSON")

	quotaCmd.AddCommand(quotaRotateCmd)
	quotaRotateCmd.Flags().StringVar(&quotaRotateWorld, "world", "", "world name")
	quotaRotateCmd.Flags().BoolVar(&quotaRotateConfirm, "confirm", false, "execute rotations (default is preview-only)")
	quotaRotateCmd.Flags().BoolVar(&quotaRotateJSON, "json", false, "output as JSON")

	// Deprecated --dry-run flag (no-op since dry-run is the default; kept for backward compatibility).
	quotaRotateCmd.Flags().Bool("dry-run", false, "deprecated: dry-run is now the default; use --confirm to execute")
	quotaRotateCmd.Flags().MarkDeprecated("dry-run", "dry-run is now the default behavior; use --confirm to execute")
}
