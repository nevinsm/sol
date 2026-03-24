package session

import (
	"strings"
	"testing"
)

// TestInjectEnv_Empty verifies that InjectEnv is a no-op for an empty map.
// It must not fail even when no tmux session is running, since the empty check
// returns before any tmux call.
func TestInjectEnv_Empty(t *testing.T) {
	t.Parallel()
	if err := InjectEnv("nonexistent-session", map[string]string{}); err != nil {
		t.Fatalf("InjectEnv with empty map should be a no-op, got: %v", err)
	}
}

// TestInjectEnv_SetsVars verifies that InjectEnv calls tmux set-environment
// for each key and that the injected vars are readable via tmux show-environment.
func TestInjectEnv_SetsVars(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	const sessName = "injectenv-test"
	if err := mgr.Start(sessName, t.TempDir(), "sleep 300", nil, "outpost", "testworld"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(sessName, true) })

	env := map[string]string{
		"TEST_SECRET_KEY": "secret123",
		"TEST_API_URL":    "https://example.com",
	}

	if err := InjectEnv(sessName, env); err != nil {
		t.Fatalf("InjectEnv failed: %v", err)
	}

	// Verify via tmux show-environment that the vars are present.
	for key, want := range env {
		cmd, cancel := tmuxCmd("show-environment", "-t", tmuxExactTarget(sessName), key)
		out, err := cmd.Output()
		cancel()
		if err != nil {
			t.Errorf("show-environment %q failed: %v", key, err)
			continue
		}
		// tmux show-environment output is "KEY=VALUE\n"
		line := strings.TrimSpace(string(out))
		expected := key + "=" + want
		if line != expected {
			t.Errorf("env var %q: got %q, want %q", key, line, expected)
		}
	}
}
