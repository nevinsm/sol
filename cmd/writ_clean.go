package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/cliapi/writs"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	cleanOlderThan string
	cleanConfirm   bool
	cleanJSON      bool
)

var writCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean writ output directories",
	Long: `Delete output directories for closed writs past the retention threshold.

Requires --confirm to proceed; without it, lists candidates and exits.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		// Determine retention threshold: default → config → CLI flag.
		retentionDays := 15
		cfg, cfgErr := config.LoadWorldConfig(world)
		if cfgErr == nil && cfg.WritClean.RetentionDays > 0 {
			retentionDays = cfg.WritClean.RetentionDays
		}
		if cleanOlderThan != "" {
			days, err := parseDaysDuration(cleanOlderThan)
			if err != nil {
				return fmt.Errorf("invalid --older-than value: %w", err)
			}
			retentionDays = days
		}

		s, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer s.Close()

		cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)

		// Find closed writs with closed_at before cutoff.
		closedWrits, err := s.ListWrits(store.ListFilters{Status: "closed"})
		if err != nil {
			return fmt.Errorf("failed to list closed writs: %w", err)
		}

		type cleanCandidate struct {
			writ store.Writ
			dir  string
			size int64
		}
		var candidates []cleanCandidate

		for _, w := range closedWrits {
			// Must have closed_at before cutoff.
			if w.ClosedAt == nil || w.ClosedAt.After(cutoff) {
				continue
			}

			// Skip if already cleaned.
			if w.Metadata != nil {
				if _, ok := w.Metadata["cleaned_at"]; ok {
					continue
				}
			}

			// Check for output directory existence.
			outputDir := config.WritOutputDir(world, w.ID)
			info, err := os.Stat(outputDir)
			if os.IsNotExist(err) {
				continue // idempotent — already gone
			}
			if err != nil {
				return fmt.Errorf("failed to stat output directory for %q: %w", w.ID, err)
			}
			if !info.IsDir() {
				continue
			}

			// Dependency guard: skip if any open transitive dependent exists.
			hasOpen, err := s.HasOpenTransitiveDependents(w.ID)
			if err != nil {
				return fmt.Errorf("failed to check dependents for %q: %w", w.ID, err)
			}
			if hasOpen {
				continue
			}

			// Calculate directory size.
			size := dirSize(outputDir)

			candidates = append(candidates, cleanCandidate{
				writ: w,
				dir:  outputDir,
				size: size,
			})
		}

		if len(candidates) == 0 {
			if cleanJSON {
				return printJSON(writs.WritCleanResult{
					RetentionDays: retentionDays,
				})
			}
			fmt.Println("No eligible writ output directories to clean.")
			return nil
		}

		if !cleanConfirm {
			if !cleanJSON {
				tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				fmt.Fprintf(tw, "ID\tCLOSED AT\tSIZE\n")
				var totalSize int64
				for _, c := range candidates {
					closedAt := ""
					if c.writ.ClosedAt != nil {
						closedAt = c.writ.ClosedAt.Format(time.RFC3339)
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\n", c.writ.ID, closedAt, formatSize(c.size))
					totalSize += c.size
				}
				tw.Flush()
				fmt.Printf("\nWould clean %d directories, reclaiming %s.\n", len(candidates), formatSize(totalSize))
				fmt.Println("Run with --confirm to proceed.")
			}
			return &exitError{code: 1}
		}

		// Execute cleanup.
		var totalSize int64
		var cleaned int
		now := time.Now().UTC().Format(time.RFC3339)

		for _, c := range candidates {
			if err := os.RemoveAll(c.dir); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to remove %s: %v\n", c.dir, err)
				continue
			}

			// Update metadata with cleaned_at.
			if err := s.SetWritMetadata(c.writ.ID, map[string]any{"cleaned_at": now}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to update metadata for %s: %v\n", c.writ.ID, err)
			}

			totalSize += c.size
			cleaned++
		}

		if cleanJSON {
			return printJSON(writs.WritCleanResult{
				WritsCleaned:  cleaned,
				DirsRemoved:   cleaned,
				BytesFreed:    totalSize,
				RetentionDays: retentionDays,
			})
		}

		fmt.Printf("Cleaned %d output directories, reclaimed %s\n", cleaned, formatSize(totalSize))
		return nil
	},
}

func init() {
	writCleanCmd.Flags().String("world", "", "world name")
	writCleanCmd.Flags().StringVar(&cleanOlderThan, "older-than", "", "retention threshold (e.g., 7d, 15d, 30d)")
	writCleanCmd.Flags().BoolVar(&cleanConfirm, "confirm", false, "confirm the destructive operation")
	writCleanCmd.Flags().BoolVar(&cleanJSON, "json", false, "output as JSON")
}

// parseDaysDuration parses a string like "15d", "7d", "30d" into a number of days.
func parseDaysDuration(s string) (int, error) {
	re := regexp.MustCompile(`^(\d+)d$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("expected format like 7d, 15d, 30d; got %q", s)
	}
	days, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid number in %q: %w", s, err)
	}
	if days <= 0 {
		return 0, fmt.Errorf("retention days must be positive, got %d", days)
	}
	return days, nil
}

// dirSize calculates the total size of files in a directory tree.
func dirSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// formatSize is defined in world_export.go.
