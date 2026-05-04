package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorldDeletePositional verifies the positional form
//
//	sol world delete <name> --confirm
//
// works without the deprecated --world flag.
func TestWorldDeletePositional(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	if _, err := runGT(t, gtHome, "world", "init", "myworld"); err != nil {
		t.Fatalf("setup: world init failed: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "delete", "myworld", "--confirm")
	if err != nil {
		t.Fatalf("positional world delete failed: %v: %s", err, out)
	}

	if _, err := os.Stat(filepath.Join(gtHome, "myworld", "world.toml")); !os.IsNotExist(err) {
		t.Fatal("world.toml still exists after positional delete")
	}
}

// TestWorldDeleteDeprecatedFlagBackwardCompat verifies the legacy --world
// flag still works (one-release backward compatibility) and prints a
// deprecation notice on stderr.
func TestWorldDeleteDeprecatedFlagBackwardCompat(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	if _, err := runGT(t, gtHome, "world", "init", "myworld"); err != nil {
		t.Fatalf("setup: world init failed: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "delete", "--world=myworld", "--confirm")
	if err != nil {
		t.Fatalf("legacy --world delete failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "deprecated") {
		t.Errorf("expected deprecation notice on stderr, got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(gtHome, "myworld", "world.toml")); !os.IsNotExist(err) {
		t.Fatal("world.toml still exists after legacy delete")
	}
}

// TestWorldDeletePositionalAndFlagConflict verifies that providing both
// the positional name and --world is rejected to avoid silent ambiguity.
func TestWorldDeletePositionalAndFlagConflict(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	if _, err := runGT(t, gtHome, "world", "init", "myworld"); err != nil {
		t.Fatalf("setup: world init failed: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "delete", "myworld", "--world=myworld", "--confirm")
	if err == nil {
		t.Fatalf("expected error when both positional and --world used, got success: %s", out)
	}
	if !strings.Contains(out, "cannot use both") {
		t.Errorf("expected 'cannot use both' error, got: %s", out)
	}
}

// TestWorldSyncPositional verifies that 'sol world sync <name>' works
// using the positional argument form. The command exits successfully
// when there is no managed repo and no source_repo configured? No — it
// errors. So we set up a source repo and verify the sync clones it.
func TestWorldSyncPositional(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Create a real git repo as source.
	sourceRepo := t.TempDir()
	gitRun(t, sourceRepo, "init")
	gitRun(t, sourceRepo, "config", "user.email", "test@test.com")
	gitRun(t, sourceRepo, "config", "user.name", "Test")
	gitRun(t, sourceRepo, "commit", "--allow-empty", "-m", "init")

	if _, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo="+sourceRepo); err != nil {
		t.Fatalf("setup: world init failed: %v", err)
	}

	// Remove the managed repo so sync needs to (re)clone.
	if err := os.RemoveAll(filepath.Join(gtHome, "myworld", "repo")); err != nil {
		t.Fatalf("setup: remove repo failed: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "sync", "myworld")
	if err != nil {
		t.Fatalf("positional world sync failed: %v: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(gtHome, "myworld", "repo")); os.IsNotExist(err) {
		t.Fatalf("managed repo not created by positional sync: %s", out)
	}
}

// TestWorldSyncDeprecatedFlagBackwardCompat verifies that 'sol world sync --world=<name>'
// still works and prints a deprecation notice.
func TestWorldSyncDeprecatedFlagBackwardCompat(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	sourceRepo := t.TempDir()
	gitRun(t, sourceRepo, "init")
	gitRun(t, sourceRepo, "config", "user.email", "test@test.com")
	gitRun(t, sourceRepo, "config", "user.name", "Test")
	gitRun(t, sourceRepo, "commit", "--allow-empty", "-m", "init")

	if _, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo="+sourceRepo); err != nil {
		t.Fatalf("setup: world init failed: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(gtHome, "myworld", "repo")); err != nil {
		t.Fatalf("setup: remove repo failed: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "sync", "--world=myworld")
	if err != nil {
		t.Fatalf("legacy --world sync failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "deprecated") {
		t.Errorf("expected deprecation notice, got: %s", out)
	}
}

// TestWorldListColumns verifies the new operational columns appear in
// the default tabular output.
func TestWorldListColumns(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	if _, err := runGT(t, gtHome, "world", "init", "myworld"); err != nil {
		t.Fatalf("setup: world init failed: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "list")
	if err != nil {
		t.Fatalf("world list failed: %v: %s", err, out)
	}
	for _, col := range []string{"NAME", "STATE", "HEALTH", "AGENTS", "QUEUE", "SOURCE REPO", "CREATED"} {
		if !strings.Contains(out, col) {
			t.Errorf("world list missing column %q in header: %s", col, out)
		}
	}
	// myworld is fresh so it should report active.
	if !strings.Contains(out, "active") {
		t.Errorf("expected fresh world to report 'active': %s", out)
	}
}

// TestWorldListJSONNewFields verifies the JSON output includes the new
// state, health, agents, queue fields.
func TestWorldListJSONNewFields(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	if _, err := runGT(t, gtHome, "world", "init", "myworld"); err != nil {
		t.Fatalf("setup: world init failed: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "list", "--json")
	if err != nil {
		t.Fatalf("world list --json failed: %v: %s", err, out)
	}

	var items []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("failed to parse JSON: %v: %s", err, out)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 world, got %d", len(items))
	}
	for _, key := range []string{"name", "state", "health", "agents", "queue", "source_repo", "created_at"} {
		if _, ok := items[0][key]; !ok {
			t.Errorf("world list JSON missing field %q: %v", key, items[0])
		}
	}
	if items[0]["state"] != "active" {
		t.Errorf("expected state=active, got %v", items[0]["state"])
	}
}
