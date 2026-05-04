package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestWorldCloneBasic(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	t.Setenv("SOL_HOME", gtHome)

	// Create a source repo and init a world.
	sourceRepo := t.TempDir()
	gitRun(t, sourceRepo, "init")
	gitRun(t, sourceRepo, "config", "user.email", "test@test.com")
	gitRun(t, sourceRepo, "config", "user.name", "Test")
	gitRun(t, sourceRepo, "commit", "--allow-empty", "-m", "init")

	if _, err := runGT(t, gtHome, "world", "init", "source", "--source-repo="+sourceRepo); err != nil {
		t.Fatalf("world init failed: %v", err)
	}

	// Create writs in source.
	itemID, err := runGT(t, gtHome, "writ", "create", "--world=source", "--title=Task One")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, itemID)
	}
	itemID = strings.TrimSpace(itemID)

	item2ID, err := runGT(t, gtHome, "writ", "create", "--world=source", "--title=Task Two")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, item2ID)
	}
	item2ID = strings.TrimSpace(item2ID)

	// Add a dependency.
	if _, err := runGT(t, gtHome, "writ", "dep", "add", itemID, item2ID, "--world=source"); err != nil {
		t.Fatalf("dep add failed: %v", err)
	}

	// Clone source → target.
	out, err := runGT(t, gtHome, "world", "clone", "source", "target")
	if err != nil {
		t.Fatalf("world clone failed: %v: %s", err, out)
	}

	if !strings.Contains(out, `cloned from "source"`) {
		t.Errorf("expected 'cloned from' in output, got: %s", out)
	}

	// Verify target world.toml exists.
	tomlPath := filepath.Join(gtHome, "target", "world.toml")
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		t.Fatal("target world.toml not created")
	}

	// Verify target DB exists.
	dbPath := filepath.Join(gtHome, ".store", "target.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("target.db not created")
	}

	// Verify writs were copied.
	targetStore, err := store.OpenWorld("target")
	if err != nil {
		t.Fatalf("open target store: %v", err)
	}
	defer targetStore.Close()

	item, err := targetStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("get writ in target: %v", err)
	}
	if item.Title != "Task One" {
		t.Errorf("expected title 'Task One', got %q", item.Title)
	}
	// Assignee should be cleared.
	if item.Assignee != "" {
		t.Errorf("expected empty assignee in clone, got %q", item.Assignee)
	}

	// Verify dependency was copied.
	deps, err := targetStore.GetDependencies(itemID)
	if err != nil {
		t.Fatalf("list dependencies: %v", err)
	}
	if len(deps) != 1 || deps[0] != item2ID {
		t.Errorf("expected dependency on %s, got %v", item2ID, deps)
	}

	// Verify target is in world list.
	listOut, err := runGT(t, gtHome, "world", "list")
	if err != nil {
		t.Fatalf("world list failed: %v: %s", err, listOut)
	}
	if !strings.Contains(listOut, "target") {
		t.Errorf("target not in world list: %s", listOut)
	}
}

func TestWorldCloneIncludeHistory(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	t.Setenv("SOL_HOME", gtHome)

	if _, err := runGT(t, gtHome, "world", "init", "source"); err != nil {
		t.Fatalf("world init: %v", err)
	}

	// Insert agent_history entries in the source world so we can verify
	// --include-history actually copies them (without this, the flag is a no-op
	// and the test passes vacuously).
	sourceStore, err := store.OpenWorld("source")
	if err != nil {
		t.Fatalf("open source store: %v", err)
	}
	now := time.Now().UTC()
	historyID, err := sourceStore.WriteHistory("TestAgent", "", "test-action", "test summary", now, nil)
	if err != nil {
		t.Fatalf("WriteHistory: %v", err)
	}
	if historyID == "" {
		t.Fatal("WriteHistory returned empty ID")
	}
	sourceStore.Close()

	// Clone without --include-history.
	out, err := runGT(t, gtHome, "world", "clone", "source", "no-history")
	if err != nil {
		t.Fatalf("clone without history: %v: %s", err, out)
	}

	// Verify: no-history clone should NOT have agent_history entries.
	noHistStore, err := store.OpenWorld("no-history")
	if err != nil {
		t.Fatalf("open no-history store: %v", err)
	}
	var noHistCount int
	if err := noHistStore.DB().QueryRow("SELECT COUNT(*) FROM agent_history").Scan(&noHistCount); err != nil {
		t.Fatalf("count agent_history in no-history: %v", err)
	}
	noHistStore.Close()
	if noHistCount != 0 {
		t.Errorf("no-history clone has %d agent_history entries, want 0", noHistCount)
	}

	// Clone with --include-history.
	out, err = runGT(t, gtHome, "world", "clone", "source", "with-history", "--include-history")
	if err != nil {
		t.Fatalf("clone with history: %v: %s", err, out)
	}

	// Verify: with-history clone SHOULD have agent_history entries.
	withHistStore, err := store.OpenWorld("with-history")
	if err != nil {
		t.Fatalf("open with-history store: %v", err)
	}
	var withHistCount int
	if err := withHistStore.DB().QueryRow("SELECT COUNT(*) FROM agent_history").Scan(&withHistCount); err != nil {
		t.Fatalf("count agent_history in with-history: %v", err)
	}
	withHistStore.Close()
	if withHistCount != 1 {
		t.Errorf("with-history clone has %d agent_history entries, want 1", withHistCount)
	}
}

func TestWorldCloneTargetExists(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	if _, err := runGT(t, gtHome, "world", "init", "source"); err != nil {
		t.Fatalf("world init source: %v", err)
	}
	if _, err := runGT(t, gtHome, "world", "init", "target"); err != nil {
		t.Fatalf("world init target: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "clone", "source", "target")
	if err == nil {
		t.Fatalf("expected error cloning to existing world, got success: %s", out)
	}
	if !strings.Contains(out, "already exists") {
		t.Errorf("expected 'already exists' error, got: %s", out)
	}
}

func TestWorldCloneSourceNotExists(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "world", "clone", "nonexistent", "target")
	if err == nil {
		t.Fatalf("expected error cloning from nonexistent, got success: %s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Errorf("expected 'does not exist' error, got: %s", out)
	}
}

func TestWorldCloneConfigCopied(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	t.Setenv("SOL_HOME", gtHome)

	if _, err := runGT(t, gtHome, "world", "init", "source"); err != nil {
		t.Fatalf("world init: %v", err)
	}

	// Mark source as sleeping to verify it gets cleared in clone.
	if _, err := runGT(t, gtHome, "world", "sleep", "source"); err != nil {
		t.Fatalf("world sleep: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "clone", "source", "target")
	if err != nil {
		t.Fatalf("world clone: %v: %s", err, out)
	}

	// Verify target config does NOT have sleeping=true.
	tomlData, err := os.ReadFile(filepath.Join(gtHome, "target", "world.toml"))
	if err != nil {
		t.Fatalf("read target world.toml: %v", err)
	}
	if strings.Contains(string(tomlData), "sleeping = true") {
		t.Error("target world.toml should not have sleeping = true")
	}
}
