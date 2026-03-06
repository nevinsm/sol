package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/quota"
	"github.com/spf13/cobra"
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
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(results)
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

		// Expire any cooldowns that have passed.
		state.ExpireCooldowns()

		if quotaStatusJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(state)
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

func init() {
	rootCmd.AddCommand(quotaCmd)

	quotaCmd.AddCommand(quotaScanCmd)
	quotaScanCmd.Flags().String("world", "", "world name")
	quotaScanCmd.Flags().BoolVar(&quotaScanJSON, "json", false, "output as JSON")

	quotaCmd.AddCommand(quotaStatusCmd)
	quotaStatusCmd.Flags().BoolVar(&quotaStatusJSON, "json", false, "output as JSON")
}
