package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/docgen"
	"github.com/nevinsm/sol/internal/docvalidate"
	"github.com/spf13/cobra"
)

var (
	docsOutputStdout bool
	docsCheck        bool
)

var docsCmd = &cobra.Command{
	Use:     "docs",
	Short:   "Documentation tools",
	GroupID: groupPlumbing,
	// Override root PersistentPreRunE — docs commands don't need SOL_HOME.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

var docsGenerateCmd = &cobra.Command{
	Use:          "generate",
	Short:        "Generate CLI reference documentation",
	Long:         "Generate docs/cli.md from the Cobra command tree. Use --check to validate without writing.",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		content := docgen.Generate(rootCmd)

		if docsCheck {
			return runValidation(content)
		}

		if docsOutputStdout {
			fmt.Print(content)
			return nil
		}

		return writeCliMd(content)
	},
}

var docsValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate docs/cli.md and run documentation drift checks",
	Long: `Validate docs/cli.md against the command tree and run a battery of
documentation drift checks against the source tree.

Drift checks (see internal/docvalidate/README.md for details):

  adr-refs            Cite-the-replacement check for superseded ADRs.
  workflow-steps      Manifest [[steps]] count vs. doc "(N steps)" claims.
  recovery-matrix     service.Components vs. failure-modes.md Recovery Matrix.
  heartbeat-paths     internal/*/heartbeat*.go HeartbeatPath() vs. operations.md.
  persona-archetypes  docs/personas.md templates vs. persona.knownDefaults.
  acceptance-tests    LOOP*_ACCEPTANCE.md test references vs. test/integration/.

Exits non-zero if any check produces findings or if docs/cli.md is out of date.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		content := docgen.Generate(rootCmd)
		cliErr := runValidation(content)

		repoRoot, err := findRepoRoot()
		if err != nil {
			if cliErr != nil {
				return cliErr
			}
			return err
		}

		report, runErr := docvalidate.Run(repoRoot)
		if report.HasFailures() {
			fmt.Fprint(os.Stderr, report.Format())
		}

		switch {
		case cliErr != nil:
			return cliErr
		case runErr != nil:
			return runErr
		case report.HasFailures():
			return fmt.Errorf("documentation drift detected: %d finding(s)", len(report.Findings))
		}
		fmt.Println("docs validation passed.")
		return nil
	},
}

func runValidation(generated string) error {
	cliMdPath, err := findCliMd()
	if err != nil {
		return err
	}

	existing, err := os.ReadFile(cliMdPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", cliMdPath, err)
	}

	result := docgen.Validate(rootCmd, string(existing))
	if result.Match {
		fmt.Println("docs/cli.md is up to date.")
		return nil
	}

	fmt.Fprintf(os.Stderr, "docs/cli.md is out of date:\n\n%s", result.Diff)
	return fmt.Errorf("docs/cli.md needs regeneration")
}

func writeCliMd(content string) error {
	cliMdPath, err := findCliMd()
	if err != nil {
		return err
	}

	if err := os.WriteFile(cliMdPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", cliMdPath, err)
	}

	fmt.Fprintf(os.Stderr, "wrote %s\n", cliMdPath)
	return nil
}

// findRepoRoot walks upward from the current working directory looking for a
// directory that contains both go.mod and a docs/ subtree — this is the repo
// root that drift checks need to anchor on. It does NOT fall back to "" so
// callers can distinguish "no repo found" from "found a partial layout".
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	for {
		_, goModErr := os.Stat(filepath.Join(dir, "go.mod"))
		_, docsErr := os.Stat(filepath.Join(dir, "docs"))
		if goModErr == nil && docsErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not locate repo root (go.mod + docs/) above %q", dir)
		}
		dir = parent
	}
}

// findCliMd locates docs/cli.md relative to the current working directory
// or the repository root. It walks upward looking for a docs/ directory.
func findCliMd() (string, error) {
	// Try current directory first.
	if _, err := os.Stat("docs/cli.md"); err == nil {
		return "docs/cli.md", nil
	}

	// Walk up to find the repo root (look for go.mod).
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	for {
		candidate := filepath.Join(dir, "docs", "cli.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		// Check for go.mod to stop at repo root.
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			// We're at repo root, use this even if cli.md doesn't exist yet.
			return filepath.Join(dir, "docs", "cli.md"), nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "docs/cli.md", nil
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.AddCommand(docsGenerateCmd)
	docsCmd.AddCommand(docsValidateCmd)

	docsGenerateCmd.Flags().BoolVar(&docsOutputStdout, "stdout", false, "Write to stdout instead of docs/cli.md")
	docsGenerateCmd.Flags().BoolVar(&docsCheck, "check", false, "Validate docs/cli.md without writing (same as sol docs validate)")
}
