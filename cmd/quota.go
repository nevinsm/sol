package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

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

var quotaStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show per-account quota state",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		state, err := quota.Load()
		if err != nil {
			return err
		}

		// Expire any limits that have passed.
		state.ExpireLimits()

		if quotaStatusJSON {
			return printJSON(state)
		}

		if len(state.Accounts) == 0 {
			fmt.Println("No quota state recorded.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "ACCOUNT\tSTATUS\tLIMITED AT\tRESETS AT\tLAST USED\n")
		for handle, acct := range state.Accounts {
			limitedAt := "-"
			if acct.LimitedAt != nil {
				limitedAt = acct.LimitedAt.Format("15:04:05")
			}
			resetsAt := "-"
			if acct.ResetsAt != nil {
				resetsAt = acct.ResetsAt.Format("15:04:05")
			}
			lastUsed := "-"
			if acct.LastUsed != nil {
				lastUsed = acct.LastUsed.Format("15:04:05")
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", handle, acct.Status, limitedAt, resetsAt, lastUsed)
		}
		tw.Flush()
		return nil
	},
}

// --- sol quota rotate ---

var quotaRotateCmd = &cobra.Command{
	Use:          "rotate",
	Short:        "Rotate rate-limited agents to available accounts",
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
}
