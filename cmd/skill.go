package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/skills"
	"github.com/spf13/cobra"
)

var skillExportOutput string

var skillCmd = &cobra.Command{
	Use:     "skill",
	Short:   "Skill management",
	GroupID: groupSetup,
	// Override root PersistentPreRunE — skill commands don't need SOL_HOME.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

var skillExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the sol-integration skill for external collaborators",
	Long: `Export the sol-integration skill directory for use in any Claude Code session.

The exported skill teaches an external agent how to interact with sol: create
writs, dispatch work, check status, manage caravans, and communicate via mail.

Copy the exported sol-integration/ directory into your project's .claude/skills/
to give any Claude Code session the ability to collaborate with sol.

Exit codes:
  0  success
  1  failure`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		output := skillExportOutput
		if output == "" {
			var err error
			output, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}
		}

		destDir := filepath.Join(output, "sol-integration")

		// Check if target already exists.
		if info, err := os.Stat(destDir); err == nil && info.IsDir() {
			fmt.Fprintf(os.Stderr, "note: overwriting existing %s\n", destDir)
			if err := os.RemoveAll(destDir); err != nil {
				return fmt.Errorf("failed to remove existing directory: %w", err)
			}
		}

		// Walk the embedded filesystem and copy files.
		root := "sol-integration"
		err := fs.WalkDir(skills.FS, root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Compute relative path from the embed root.
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}

			dest := filepath.Join(destDir, rel)

			if d.IsDir() {
				return os.MkdirAll(dest, 0755)
			}

			data, err := skills.FS.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read embedded file %s: %w", path, err)
			}

			// Preserve executable bit for scripts.
			perm := os.FileMode(0644)
			if filepath.Ext(path) == ".sh" {
				perm = 0755
			}

			return os.WriteFile(dest, data, perm)
		})
		if err != nil {
			return fmt.Errorf("failed to export skill: %w", err)
		}

		fmt.Printf("Exported sol-integration skill to %s\n", destDir)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(skillCmd)
	skillCmd.AddCommand(skillExportCmd)
	skillExportCmd.Flags().StringVarP(&skillExportOutput, "output", "o", "",
		"output directory (default: current directory)")
}
