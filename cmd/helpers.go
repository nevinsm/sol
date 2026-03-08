package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

// restartSession stops a running session and then delegates to startCmd for
// the start phase. If stopFn is non-nil it is called to stop the component;
// otherwise mgr.Stop(sessName) is used directly. label is used in the
// default error message wrapping mgr.Stop. stoppedMsg is printed after a
// successful stop.
func restartSession(mgr *session.Manager, sessName, label, stoppedMsg string, stopFn func() error, startCmd *cobra.Command, args []string) error {
	if mgr.Exists(sessName) {
		if stopFn != nil {
			if err := stopFn(); err != nil {
				return err
			}
		} else {
			if err := mgr.Stop(sessName, false); err != nil {
				return fmt.Errorf("failed to stop %s: %w", label, err)
			}
		}
		fmt.Println(stoppedMsg)
	}
	return startCmd.RunE(startCmd, args)
}

// archiveBrief archives a brief file by moving it to an archive subdirectory
// with an RFC3339-based timestamp. briefDir is the base brief directory (the
// archive subdirectory is created under it). briefPath is the path to the
// current brief file. Returns the archive filename on success.
func archiveBrief(briefDir, briefPath string) (string, error) {
	archiveDir := filepath.Join(briefDir, "archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create archive directory: %w", err)
	}

	// Generate archive filename with RFC3339 timestamp, colons replaced by dashes.
	ts := time.Now().UTC().Format(time.RFC3339)
	safeTS := strings.ReplaceAll(ts, ":", "-")
	archiveFile := safeTS + ".md"
	archivePath := filepath.Join(archiveDir, archiveFile)

	if err := os.Rename(briefPath, archivePath); err != nil {
		return "", fmt.Errorf("failed to archive brief: %w", err)
	}

	return archiveFile, nil
}

// parseVarFlags parses key=value flag entries. Returns an error if any
// entry does not contain "=".
func parseVarFlags(vars []string) (map[string]string, error) {
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --var %q: must be key=value", v)
		}
		m[parts[0]] = parts[1]
	}
	return m, nil
}

// gatedWorldOpener opens a world store after verifying the world exists.
func gatedWorldOpener(world string) (*store.Store, error) {
	if err := config.RequireWorld(world); err != nil {
		return nil, err
	}
	return store.OpenWorld(world)
}
