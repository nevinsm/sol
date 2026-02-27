package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

func TestHardGateStoreCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "store", "create", "--world=noworld", "--title=test")
	if err == nil {
		t.Fatalf("expected error, got success: %s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %s", out)
	}
}

func TestHardGateStoreGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "store", "get", "sol-00000000", "--world=noworld")
	if err == nil {
		t.Fatalf("expected error, got success: %s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %s", out)
	}
}

func TestHardGateCast(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "cast", "sol-00000000", "noworld")
	if err == nil {
		t.Fatalf("expected error, got success: %s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %s", out)
	}
}

func TestHardGateForgeQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "forge", "queue", "noworld")
	if err == nil {
		t.Fatalf("expected error, got success: %s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %s", out)
	}
}

func TestHardGateStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "status", "noworld")
	if err == nil {
		t.Fatalf("expected error, got success: %s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %s", out)
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
	out, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo=/tmp/fakerepo")
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
