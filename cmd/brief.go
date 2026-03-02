package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/brief"
	"github.com/spf13/cobra"
)

var (
	briefInjectPath     string
	briefInjectMaxLines int
)

var briefCmd = &cobra.Command{
	Use:   "brief",
	Short: "Manage agent brief files",
}

var briefInjectCmd = &cobra.Command{
	Use:   "inject",
	Short: "Inject brief into session context",
	Long: `Read a brief file and output framed content for session injection.

Used by Claude Code hooks to inject agent context on session start
and after context compaction. Also records session start timestamp
for the stop hook save check.`,
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
		return brief.WriteSessionStart(filepath.Dir(briefInjectPath))
	},
}

var briefCheckSaveCmd = &cobra.Command{
	Use:   "check-save <path>",
	Short: "Check if brief was updated since session start",
	Long: `Stop hook command. Checks whether the brief file was modified
since the session started. If not, outputs a nudge message and exits
with code 1 to block the stop.

Set SOL_STOP_HOOK_ACTIVE=true on second invocation to allow stop
without brief update (prevents infinite loops).`,
	SilenceErrors: true,
	SilenceUsage:  true,
	Args:          cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getenv("SOL_STOP_HOOK_ACTIVE") == "true" {
			return nil
		}

		updated, err := brief.CheckSave(args[0])
		if err != nil {
			return err
		}
		if updated {
			return nil
		}

		fmt.Println(`Your brief has not been updated since this session started.
Please update .brief/memory.md with key context before exiting:
- Decisions made and rationale
- Current state of work
- What to do next

Then try exiting again.`)
		return &exitError{code: 1}
	},
}

func init() {
	rootCmd.AddCommand(briefCmd)
	briefCmd.AddCommand(briefInjectCmd)
	briefCmd.AddCommand(briefCheckSaveCmd)
	briefInjectCmd.Flags().StringVar(&briefInjectPath, "path", "", "path to brief file")
	briefInjectCmd.MarkFlagRequired("path")
	briefInjectCmd.Flags().IntVar(&briefInjectMaxLines, "max-lines", 200, "maximum lines before truncation")
}
