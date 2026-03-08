package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritCreateBlockedInSleepingWorld(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "sleeptest"

	// Create world directory with sleeping=true config.
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\nsleeping = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reset package-level flag vars.
	createTitle = "test writ"
	createDescription = "test description"
	createPriority = 2
	createLabels = nil
	createKind = ""
	createMetadata = ""

	rootCmd.SetArgs([]string{"writ", "create", "--world", world, "--title", "test writ"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when creating writ in sleeping world")
	}
	errStr := err.Error()
	if !(strings.Contains(errStr, "sleeping") && strings.Contains(errStr, "writ creation blocked")) {
		t.Errorf("expected sleeping/blocked error, got: %v", err)
	}
	if !strings.Contains(errStr, "sol world wake") {
		t.Errorf("expected wake hint in error, got: %v", err)
	}
}
