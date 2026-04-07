package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nevinsm/sol/internal/workflow"
)

// TestWorkflowEjectFreshSucceedsWithoutConfirm verifies that a first-time
// eject (target doesn't exist yet) is not considered destructive and does
// not require --confirm.
func TestWorkflowEjectFreshSucceedsWithoutConfirm(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	rootCmd.SetArgs([]string{"workflow", "eject", "rule-of-five"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("fresh eject failed: %v", err)
	}

	// The target directory should now exist.
	target := workflow.Dir("rule-of-five")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected target %s to exist: %v", target, err)
	}
	if _, err := os.Stat(filepath.Join(target, "manifest.toml")); err != nil {
		t.Fatalf("expected manifest.toml in target: %v", err)
	}
}

// TestWorkflowEjectExistingRequiresConfirm verifies that re-ejecting over
// an already-ejected workflow is destructive and requires --confirm.
// Without --confirm, exit 1 and nothing is overwritten.
func TestWorkflowEjectExistingRequiresConfirm(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Do a first ejection so the target exists.
	rootCmd.SetArgs([]string{"workflow", "eject", "rule-of-five"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("initial eject failed: %v", err)
	}

	target := workflow.Dir("rule-of-five")

	// Write a marker file so we can detect whether the dir got overwritten.
	marker := filepath.Join(target, "HAND-EDIT.marker")
	if err := os.WriteFile(marker, []byte("do not overwrite"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	// Second eject without --confirm: should exit 1 and preserve the marker.
	rootCmd.SetArgs([]string{"workflow", "eject", "rule-of-five"})
	err := rootCmd.Execute()
	if code := ExitCode(err); code != 1 {
		t.Fatalf("expected exit code 1 without --confirm, got %d (err=%v)", code, err)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker file should still exist (dry-run should not touch the dir): %v", err)
	}
}

// TestWorkflowEjectExistingWithConfirmOverwrites verifies that --confirm
// actually performs the backup-and-overwrite.
func TestWorkflowEjectExistingWithConfirmOverwrites(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	rootCmd.SetArgs([]string{"workflow", "eject", "rule-of-five"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("initial eject failed: %v", err)
	}

	target := workflow.Dir("rule-of-five")
	marker := filepath.Join(target, "HAND-EDIT.marker")
	if err := os.WriteFile(marker, []byte("hand edit"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	rootCmd.SetArgs([]string{"workflow", "eject", "rule-of-five", "--confirm"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("re-eject with --confirm failed: %v", err)
	}

	// Marker should no longer exist in the target (it got rotated out with the backup).
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("expected marker to be gone after overwrite, stat err=%v", err)
	}

	// A backup directory should exist alongside the target.
	parent := filepath.Dir(target)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	foundBackup := false
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) > len("rule-of-five.bak-") && e.Name()[:len("rule-of-five.bak-")] == "rule-of-five.bak-" {
			foundBackup = true
			break
		}
	}
	if !foundBackup {
		t.Fatalf("expected backup directory rule-of-five.bak-* alongside target, got entries: %v", entries)
	}
}
