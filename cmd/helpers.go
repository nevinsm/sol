package cmd

import (
	"encoding/json"
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

// briefSubcommand builds a brief subcommand with common read-and-print logic.
// pathFn is called at runtime with command arguments and returns the brief path
// and a not-found message (which may include dynamic values like world or agent
// name). argsValidator may be nil to accept any arguments.
func briefSubcommand(use, short string, argsValidator cobra.PositionalArgs,
	pathFn func(args []string) (path, notFoundMsg string, err error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:          use,
		Short:        short,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, notFoundMsg, err := pathFn(args)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println(notFoundMsg)
					return nil
				}
				return fmt.Errorf("failed to read brief: %w", err)
			}
			fmt.Print(string(data))
			return nil
		},
	}
	if argsValidator != nil {
		cmd.Args = argsValidator
	}
	return cmd
}

// debriefSubcommand builds a debrief subcommand with common archive logic.
// pathFn is called at runtime with command arguments and returns the brief path,
// brief directory, not-found message, and post-archive ready message.
// argsValidator may be nil to accept any arguments.
func debriefSubcommand(use, short string, argsValidator cobra.PositionalArgs,
	pathFn func(args []string) (briefPath, briefDir, notFoundMsg, readyMsg string, err error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:          use,
		Short:        short,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			briefPath, briefDir, notFoundMsg, readyMsg, err := pathFn(args)
			if err != nil {
				return err
			}
			if _, err := os.Stat(briefPath); err != nil {
				if os.IsNotExist(err) {
					fmt.Println(notFoundMsg)
					return nil
				}
				return fmt.Errorf("failed to check brief: %w", err)
			}
			archiveFile, err := archiveBrief(briefDir, briefPath)
			if err != nil {
				return err
			}
			fmt.Printf("Archived brief to .brief/archive/%s\n", archiveFile)
			fmt.Println(readyMsg)
			return nil
		},
	}
	if argsValidator != nil {
		cmd.Args = argsValidator
	}
	return cmd
}

// restartSession stops a running session and then delegates to startCmd for
// the start phase. If stopFn is non-nil it is called to stop the component;
// otherwise mgr.Stop(sessName) is used directly. label is used in the
// default error message wrapping mgr.Stop. stoppedMsg is printed after a
// successful stop. If unlockFn is non-nil it is called after stop completes
// but before start begins, ensuring any held lock covers the entire stop
// operation.
func restartSession(mgr *session.Manager, sessName, label, stoppedMsg string, stopFn func() error, unlockFn func() error, startCmd *cobra.Command, args []string) error {
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
	if unlockFn != nil {
		_ = unlockFn()
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

// printJSON encodes v as indented JSON to stdout.
func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// gatedWorldOpener opens a world store after verifying the world exists.
func gatedWorldOpener(world string) (*store.WorldStore, error) {
	if err := config.RequireWorld(world); err != nil {
		return nil, err
	}
	return store.OpenWorld(world)
}
