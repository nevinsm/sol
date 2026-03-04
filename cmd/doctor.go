package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/doctor"
	"github.com/spf13/cobra"
)

var doctorJSON bool

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Short:   "Check system prerequisites",
	GroupID: groupSetup,
	Long: `Validate that all prerequisites for running sol are met.

Checks: tmux, git, claude CLI, SOL_HOME directory, SQLite WAL support.

Exit code 0 if all checks pass, 1 if any check fails.`,
	SilenceErrors: true,
	SilenceUsage:  true,
	Args:          cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		report := doctor.RunAll()

		if doctorJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(report)
		}

		// Human-readable output.
		for _, check := range report.Checks {
			if check.Passed {
				fmt.Printf("  ✓ %-12s %s\n", check.Name, check.Message)
			} else {
				fmt.Printf("  ✗ %-12s %s\n", check.Name, check.Message)
				if check.Fix != "" {
					fmt.Printf("    → %s\n", check.Fix)
				}
			}
		}

		fmt.Println()
		if report.AllPassed() {
			fmt.Println("All checks passed. Ready to run sol.")
			return nil
		}

		fmt.Printf("%d check(s) failed.\n", report.FailedCount())
		return &exitError{code: 1}
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "output as JSON")
}
