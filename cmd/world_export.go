package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nevinsm/sol/internal/cliapi/worlds"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/worldexport"
	"github.com/spf13/cobra"
)

var (
	worldExportOutput string
	worldExportJSON   bool
)

var worldExportCmd = &cobra.Command{
	Use:   "export <name>",
	Short: "Export a world to a tar.gz archive",
	Long: `Export a world's state to a compressed archive for backup or migration.

The archive includes the world database (WAL-checkpointed), world.toml,
sphere-scoped data (agents, messages, escalations, caravans), and a manifest.
Ephemeral state (tmux sessions, PID files, worktrees) is excluded.

The managed repo (repo/) is excluded — it can be re-cloned from source_repo.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if err := config.RequireWorld(name); err != nil {
			return err
		}

		result, err := worldexport.Export(worldexport.ExportOptions{
			World:      name,
			OutputPath: worldExportOutput,
			SolVersion: version,
		})
		if err != nil {
			return err
		}

		if worldExportJSON {
			resp := worlds.WorldExportResult{
				World:       name,
				ArchivePath: result.OutputPath,
				SizeBytes:   result.Size,
				ExportedAt:  time.Now().UTC().Truncate(time.Second),
			}
			data, err := json.Marshal(resp)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		if result.Size > 0 {
			fmt.Printf("World %q exported to %s (%s)\n", name, result.OutputPath, formatSize(result.Size))
		} else {
			fmt.Printf("World %q exported to %s\n", name, result.OutputPath)
		}
		return nil
	},
}

// formatSize returns a human-readable file size.
func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func init() {
	worldCmd.AddCommand(worldExportCmd)
	worldExportCmd.Flags().StringVarP(&worldExportOutput, "output", "o", "",
		"output file path (default: <name>-export.tar.gz)")
	worldExportCmd.Flags().BoolVar(&worldExportJSON, "json", false, "output as JSON")
}
