package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/docgen"
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
	Use:          "validate",
	Short:        "Validate docs/cli.md against the command tree",
	Long:         "Compare docs/cli.md against what the command tree would generate. Exits non-zero if discrepancies are found.",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		content := docgen.Generate(rootCmd)
		return runValidation(content)
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
