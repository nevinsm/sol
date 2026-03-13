package session

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// InjectEnv sets each key-value pair from env into the tmux session environment
// using `tmux set-environment`. This updates the session's environment so that
// future panes and processes inherit the variables.
//
// Returns nil immediately if env is empty (no-op).
// Key names are logged at debug level; values are never logged.
func InjectEnv(sessionName string, env map[string]string) error {
	if len(env) == 0 {
		return nil
	}

	// Collect and sort keys for deterministic log output.
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	slog.Debug("session: injecting env vars", "session", sessionName, "keys", strings.Join(keys, ","))

	for _, k := range keys {
		v := env[k]
		setEnv, setEnvCancel := tmuxCmd("set-environment", "-t", tmuxExactTarget(sessionName), k, v)
		out, err := setEnv.CombinedOutput()
		setEnvCancel()
		if err != nil {
			return fmt.Errorf("session: failed to set env %q in session %q: %s: %w",
				k, sessionName, strings.TrimSpace(string(out)), err)
		}
	}

	return nil
}
