package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

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
			return printJSON(results)
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

// quotaStatusJSONAccount is the per-account JSON row emitted by
// `sol quota status --json`. It wraps quota.AccountState with CLI-layer
// fields (Handle, Window, Remaining) that are not persisted in quota state.
//
// TODO(sol-7fae31b279f38779): Window and Remaining are currently always
// rendered as cliformat.EmptyMarker because neither the broker nor the quota
// subsystem exposes per-account rate-limit window metadata (e.g. "5h" for
// Claude OAuth, "rpm" for API keys) or remaining-request counts. When that
// plumbing lands in internal/quota or internal/broker, populate these fields
// here. See W1.8 scope — broker changes were intentionally excluded.
type quotaStatusJSONAccount struct {
	Handle    string              `json:"handle"`
	Status    quota.Status        `json:"status"`
	LimitedAt *time.Time          `json:"limited_at,omitempty"`
	ResetsAt  *time.Time          `json:"resets_at,omitempty"`
	LastUsed  *time.Time          `json:"last_used,omitempty"`
	Window    string              `json:"window,omitempty"`
	Remaining *int                `json:"remaining,omitempty"`
}

type quotaStatusJSONOutput struct {
	Accounts []quotaStatusJSONAccount `json:"accounts"`
}

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
			out := quotaStatusJSONOutput{
				Accounts: make([]quotaStatusJSONAccount, 0, len(handles)),
			}
			for _, handle := range handles {
				acct := state.Accounts[handle]
				out.Accounts = append(out.Accounts, quotaStatusJSONAccount{
					Handle:    handle,
					Status:    acct.Status,
					LimitedAt: acct.LimitedAt,
					ResetsAt:  acct.ResetsAt,
					LastUsed:  acct.LastUsed,
					// Window/Remaining intentionally omitted — see TODO above.
				})
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
				// TODO(sol-7fae31b279f38779): surface real window from broker.
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

	// Deprecated --dry-run flag (no-op since dry-run is the default; kept for backward compatibility).
	quotaRotateCmd.Flags().Bool("dry-run", false, "deprecated: dry-run is now the default; use --confirm to execute")
	quotaRotateCmd.Flags().MarkDeprecated("dry-run", "dry-run is now the default behavior; use --confirm to execute")
}
