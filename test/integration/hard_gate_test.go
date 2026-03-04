package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

func TestHardGateAllCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	cases := []struct {
		name string
		args []string
	}{
		// store commands
		{"store create", []string{"store", "create", "--world=noworld", "--title=test"}},
		{"store get", []string{"store", "get", "sol-00000000", "--world=noworld"}},
		{"store list", []string{"store", "list", "--world=noworld"}},
		{"store update", []string{"store", "update", "sol-00000000", "--world=noworld", "--status=closed"}},
		{"store close", []string{"store", "close", "sol-00000000", "--world=noworld"}},
		{"store query", []string{"store", "query", "--world=noworld", "--sql=SELECT 1"}},
		// store dep commands
		{"store dep add", []string{"store", "dep", "add", "sol-00000001", "sol-00000002", "--world=noworld"}},
		{"store dep remove", []string{"store", "dep", "remove", "sol-00000001", "sol-00000002", "--world=noworld"}},
		{"store dep list", []string{"store", "dep", "list", "sol-00000001", "--world=noworld"}},
		// core commands
		{"cast", []string{"cast", "sol-00000000", "--world=noworld"}},
		{"status", []string{"status", "--world=noworld"}},
		{"prime", []string{"prime", "--world=noworld", "--agent=test"}},
		{"resolve", []string{"resolve", "--world=noworld", "--agent=test"}},
		// agent commands
		{"agent create", []string{"agent", "create", "test", "--world=noworld"}},
		{"agent list", []string{"agent", "list", "--world=noworld"}},
		// forge commands
		{"forge queue", []string{"forge", "queue", "--world=noworld"}},
		{"forge ready", []string{"forge", "ready", "--world=noworld"}},
		{"forge blocked", []string{"forge", "blocked", "--world=noworld"}},
		// sentinel commands
		{"sentinel run", []string{"sentinel", "run", "--world=noworld"}},
		// workflow commands
		{"workflow current", []string{"workflow", "current", "--world=noworld", "--agent=test"}},
		{"workflow status", []string{"workflow", "status", "--world=noworld", "--agent=test"}},
		// world commands (that require existing world)
		{"world status", []string{"world", "status", "--world=noworld"}},
		{"world delete", []string{"world", "delete", "--world=noworld", "--confirm"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runGT(t, gtHome, tc.args...)
			if err == nil {
				t.Fatalf("expected error, got success: %s", out)
			}
			if !strings.Contains(out, "does not exist") {
				t.Fatalf("expected 'does not exist' error, got: %s", out)
			}
		})
	}
}

func TestHardGatePreArc1World(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Create DB manually (world exists in store but no world.toml).
	t.Setenv("SOL_HOME", gtHome)
	s, err := store.OpenWorld("legacy")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	s.Close()

	out, err := runGT(t, gtHome, "store", "create", "--world=legacy", "--title=test")
	if err == nil {
		t.Fatalf("expected error, got success: %s", out)
	}
	if !strings.Contains(out, "before world lifecycle") {
		t.Fatalf("expected 'before world lifecycle' error, got: %s", out)
	}
}

func TestHardGatePassesAfterInit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Init the world.
	out, err := runGT(t, gtHome, "world", "init", "myworld")
	if err != nil {
		t.Fatalf("world init failed: %v: %s", err, out)
	}

	// Store create should now succeed.
	out, err = runGT(t, gtHome, "store", "create", "--world=myworld", "--title=test")
	if err != nil {
		t.Fatalf("store create failed after init: %v: %s", err, out)
	}
	if !strings.HasPrefix(out, "sol-") {
		t.Errorf("store create output not an ID: %q", out)
	}
}
