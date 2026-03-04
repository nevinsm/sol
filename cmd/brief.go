package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/brief"
	"github.com/spf13/cobra"
)

var (
	briefInjectPath     string
	briefInjectMaxLines int
)

var briefCmd = &cobra.Command{
	Use:     "brief",
	Short:   "Manage agent brief files",
	GroupID: groupPlumbing,
}

var briefInjectCmd = &cobra.Command{
	Use:   "inject",
	Short: "Inject brief into session context",
	Long: `Read a brief file and output framed content for session injection.

Used by Claude Code hooks to inject agent context on session start
and after context compaction.`,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := brief.Inject(briefInjectPath, briefInjectMaxLines)
		if err != nil {
			return err
		}
		if content != "" {
			fmt.Println(content)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(briefCmd)
	briefCmd.AddCommand(briefInjectCmd)
	briefInjectCmd.Flags().StringVar(&briefInjectPath, "path", "", "path to brief file")
	briefInjectCmd.MarkFlagRequired("path")
	briefInjectCmd.Flags().IntVar(&briefInjectMaxLines, "max-lines", 200, "maximum lines before truncation")
}
