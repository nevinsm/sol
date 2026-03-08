package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	worldExportOutput string
)

// exportManifest is the metadata written to manifest.json in the archive.
type exportManifest struct {
	World      string   `json:"world"`
	SourceRepo string   `json:"source_repo,omitempty"`
	ExportedAt string   `json:"exported_at"`
	Contents   []string `json:"contents"`
}

var worldExportCmd = &cobra.Command{
	Use:   "export <name>",
	Short: "Export a world to a tar.gz archive",
	Long: `Export a world's state to a compressed archive for backup or migration.

The archive includes the world database (WAL-checkpointed), world.toml,
agent configuration directories, and a metadata manifest. Ephemeral state
(tmux sessions, PID files, worktrees) is excluded.

The managed repo (repo/) is excluded — it can be re-cloned from source_repo.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if err := config.RequireWorld(name); err != nil {
			return err
		}

		// Determine output path.
		output := worldExportOutput
		if output == "" {
			output = name + "-export.tar.gz"
		}

		// Checkpoint world database WAL.
		worldStore, err := store.OpenWorld(name)
		if err != nil {
			return err
		}
		defer worldStore.Close()
		if err := worldStore.Checkpoint(); err != nil {
			return fmt.Errorf("failed to checkpoint world database: %w", err)
		}

		// Open output file.
		f, err := os.Create(output)
		if err != nil {
			return fmt.Errorf("failed to create output file %q: %w", output, err)
		}
		defer f.Close()

		gw := gzip.NewWriter(f)
		defer gw.Close()
		tw := tar.NewWriter(gw)
		defer tw.Close()

		var contents []string
		prefix := name + "/"

		// 1. Add world database.
		dbPath := filepath.Join(config.StoreDir(), name+".db")
		if err := addFileToTar(tw, dbPath, prefix+"database/"+name+".db"); err != nil {
			return fmt.Errorf("failed to add world database: %w", err)
		}
		contents = append(contents, "database/"+name+".db")

		// 2. Walk world directory, excluding ephemeral state.
		worldDir := config.WorldDir(name)
		err = filepath.Walk(worldDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			rel, err := filepath.Rel(worldDir, path)
			if err != nil {
				return err
			}

			// Skip the world directory root itself.
			if rel == "." {
				return nil
			}

			// Skip ephemeral/excludable paths.
			if shouldExclude(rel, info) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			archivePath := prefix + "tree/" + rel
			if info.IsDir() {
				return addDirToTar(tw, archivePath, info)
			}

			// Skip symlinks — they point to local credentials paths.
			if info.Mode()&os.ModeSymlink != 0 {
				return nil
			}

			if err := addFileToTar(tw, path, archivePath); err != nil {
				return fmt.Errorf("failed to add %q: %w", rel, err)
			}
			contents = append(contents, "tree/"+rel)
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to walk world directory: %w", err)
		}

		// 3. Write manifest.
		cfg, _ := config.LoadWorldConfig(name)
		manifest := exportManifest{
			World:      name,
			ExportedAt: time.Now().UTC().Format(time.RFC3339),
			Contents:   contents,
		}
		if cfg.World.SourceRepo != "" {
			manifest.SourceRepo = cfg.World.SourceRepo
		}

		manifestData, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal manifest: %w", err)
		}
		if err := addBytesToTar(tw, manifestData, prefix+"manifest.json"); err != nil {
			return fmt.Errorf("failed to add manifest: %w", err)
		}

		// Flush writers.
		if err := tw.Close(); err != nil {
			return fmt.Errorf("failed to finalize tar: %w", err)
		}
		if err := gw.Close(); err != nil {
			return fmt.Errorf("failed to finalize gzip: %w", err)
		}

		// Report size.
		stat, err := os.Stat(output)
		if err != nil {
			fmt.Printf("World %q exported to %s\n", name, output)
			return nil
		}
		fmt.Printf("World %q exported to %s (%s)\n", name, output, formatSize(stat.Size()))
		return nil
	},
}

// shouldExclude returns true for paths that should be excluded from export.
func shouldExclude(rel string, info os.FileInfo) bool {
	parts := strings.Split(rel, string(filepath.Separator))
	for _, p := range parts {
		switch p {
		case "repo":
			// Managed git clone — re-cloneable from source_repo.
			return true
		case "worktree":
			// Ephemeral worktrees created by cast.
			return true
		}
	}

	// Skip tether files (ephemeral active-work markers).
	if filepath.Base(rel) == ".tether" {
		return true
	}

	return false
}

// addFileToTar adds a regular file to the tar archive.
func addFileToTar(tw *tar.Writer, srcPath, archiveName string) error {
	// Use Lstat to handle symlinks — we skip symlinks in the walker,
	// but this function may be called directly for the DB file.
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    archiveName,
		Size:    info.Size(),
		Mode:    int64(info.Mode()),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(tw, f)
	return err
}

// addDirToTar adds a directory entry to the tar archive.
func addDirToTar(tw *tar.Writer, archiveName string, info os.FileInfo) error {
	header := &tar.Header{
		Typeflag: tar.TypeDir,
		Name:     archiveName + "/",
		Mode:     int64(info.Mode()),
		ModTime:  info.ModTime(),
	}
	return tw.WriteHeader(header)
}

// addBytesToTar adds in-memory content as a file to the tar archive.
func addBytesToTar(tw *tar.Writer, data []byte, archiveName string) error {
	header := &tar.Header{
		Name:    archiveName,
		Size:    int64(len(data)),
		Mode:    0o644,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
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
}
