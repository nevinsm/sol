package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	cliapidoctor "github.com/nevinsm/sol/internal/cliapi/doctor"
	"github.com/nevinsm/sol/internal/doctor"
	"github.com/spf13/cobra"
)

var (
	doctorJSON   bool
	doctorFix    bool
	doctorYes    bool
	doctorDryRun bool
)

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Short:   "Check system prerequisites",
	GroupID: groupSetup,
	Long: `Validate that all prerequisites for running sol are met.

Checks: tmux, git, claude CLI, jq, SOL_HOME directory, SQLite WAL support,
env files, runtime binaries, pending migrations, credential symlinks,
obsolete account directories, dead config keys, defunct config dirs.

Exit code 0 if all checks pass, 1 if any check fails.

Upgrade path:
  sol doctor             -- detect stale state from pre-simplification installs
  sol doctor --fix       -- detect and interactively apply safe remediations
  sol doctor --fix --yes -- detect and apply remediations without prompting
  sol doctor --fix --dry-run -- show what --fix would do, without doing it`,
	SilenceErrors: true,
	SilenceUsage:  true,
	Args:          cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if doctorDryRun && !doctorFix {
			return fmt.Errorf("--dry-run requires --fix")
		}

		report := doctor.RunAll()

		if doctorJSON {
			resp := cliapidoctor.FromReport(report)
			if err := printJSON(resp); err != nil {
				return err
			}
			if !report.AllPassed() {
				return &exitError{code: 1}
			}
			return nil
		}

		// Human-readable output.
		for _, check := range report.Checks {
			switch {
			case check.Passed && check.Warning:
				fmt.Printf("  ⚠ %-12s %s\n", check.Name, check.Message)
			case check.Passed:
				fmt.Printf("  ✓ %-12s %s\n", check.Name, check.Message)
			default:
				fmt.Printf("  ✗ %-12s %s\n", check.Name, check.Message)
				if check.Fix != "" {
					fmt.Printf("    → %s\n", check.Fix)
				}
			}
		}

		fmt.Println()
		if report.AllPassed() {
			fmt.Println("All checks passed. Ready to run sol.")
		} else {
			fmt.Printf("%d check(s) failed.\n", report.FailedCount())
		}

		// Handle --fix.
		if doctorFix {
			if err := applyFixes(report, doctorYes, doctorDryRun); err != nil {
				return err
			}
		}

		if !report.AllPassed() {
			return &exitError{code: 1}
		}
		return nil
	},
}

// applyFixes applies (or previews) the remediations for all fixable checks in
// the report. If dryRun is true, it only prints what would be done. If yes is
// false, it prompts the operator for confirmation before applying.
func applyFixes(report *doctor.Report, yes, dryRun bool) error {
	fixable := report.FixableChecks()
	if len(fixable) == 0 {
		fmt.Println("\nNo fixable issues found.")
		return nil
	}

	fmt.Printf("\n%d fixable issue(s) found:\n", len(fixable))
	for _, c := range fixable {
		indicator := "⚠"
		if !c.Passed {
			indicator = "✗"
		}
		fmt.Printf("  %s %s\n", indicator, c.Name)
		if c.Fix != "" {
			// Print only the first line of the fix hint for brevity.
			firstLine, _, _ := strings.Cut(c.Fix, "\n")
			fmt.Printf("    %s\n", firstLine)
		}
	}

	if dryRun {
		fmt.Println("\n--dry-run: no changes applied.")
		return nil
	}

	if !yes {
		fmt.Printf("\nApply %d remediation(s)? [y/N] ", len(fixable))
		reader := bufio.NewReader(os.Stdin)
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	fmt.Println("\nApplying remediations...")
	var errs []string
	for _, c := range fixable {
		fmt.Printf("  Fixing %s...\n", c.Name)
		if err := c.Remediate(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", c.Name, err))
			fmt.Printf("  ✗ %s: %v\n", c.Name, err)
		} else {
			fmt.Printf("  ✓ %s: done\n", c.Name)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%d remediation(s) failed:\n  %s", len(errs), strings.Join(errs, "\n  "))
	}

	fmt.Println("\nAll remediations applied. Run 'sol doctor' to verify.")
	return nil
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "output as JSON")
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "apply safe remediations for detected issues")
	doctorCmd.Flags().BoolVar(&doctorYes, "yes", false, "apply remediations without interactive confirmation (requires --fix)")
	doctorCmd.Flags().BoolVar(&doctorDryRun, "dry-run", false, "show what --fix would do without applying changes (requires --fix)")
}
