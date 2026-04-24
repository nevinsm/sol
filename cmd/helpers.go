package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

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
			if err := mgr.Stop(sessName, false); err != nil && !errors.Is(err, session.ErrNotFound) {
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
