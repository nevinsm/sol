package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
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
	Short:        "Generate CLI reference documentation (deprecated — use skills)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("sol docs generate is deprecated. Agent command references are now provided via skills.")
		fmt.Println("Skills are installed automatically at agent startup via InstallSkills().")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.AddCommand(docsGenerateCmd)
}
