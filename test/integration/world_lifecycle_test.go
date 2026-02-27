package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorldInitBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo=/tmp/fakerepo")
	if err != nil {
		t.Fatalf("world init failed: %v: %s", err, out)
	}

	// Verify world.toml exists.
	tomlPath := filepath.Join(gtHome, "myworld", "world.toml")
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		t.Fatal("world.toml not created")
	}

	// Verify myworld.db exists.
	dbPath := filepath.Join(gtHome, ".store", "myworld.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("myworld.db not created")
	}

	// Verify myworld/ directory exists.
	worldDir := filepath.Join(gtHome, "myworld")
	if _, err := os.Stat(worldDir); os.IsNotExist(err) {
		t.Fatal("myworld/ directory not created")
	}

	// Verify myworld/outposts/ directory exists.
	outpostsDir := filepath.Join(gtHome, "myworld", "outposts")
	if _, err := os.Stat(outpostsDir); os.IsNotExist(err) {
		t.Fatal("myworld/outposts/ directory not created")
	}
}

func TestWorldInitWithSourceRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo=/tmp/fakerepo")
	if err != nil {
		t.Fatalf("world init failed: %v: %s", err, out)
	}

	// Verify world.toml contains source_repo.
	tomlPath := filepath.Join(gtHome, "myworld", "world.toml")
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/tmp/fakerepo") {
		t.Fatalf("world.toml does not contain source_repo: %s", data)
	}
}

func TestWorldInitAlreadyExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Init once — success.
	out, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo=/tmp/fakerepo")
	if err != nil {
		t.Fatalf("first init failed: %v: %s", err, out)
	}

	// Init again — error.
	out, err = runGT(t, gtHome, "world", "init", "myworld")
	if err == nil {
		t.Fatalf("expected error on second init, got success: %s", out)
	}
	if !strings.Contains(out, "already initialized") {
		t.Fatalf("expected 'already initialized' error, got: %s", out)
	}
}

func TestWorldInitPreArc1World(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Create a world DB manually (simulate pre-Arc1 by creating a work item).
	out, err := runGT(t, gtHome, "store", "create", "--world=legacy", "--title=Old item")
	if err != nil {
		t.Fatalf("store create failed: %v: %s", err, out)
	}

	// Verify DB exists but world.toml does not.
	dbPath := filepath.Join(gtHome, ".store", "legacy.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("legacy.db not created by store create")
	}
	tomlPath := filepath.Join(gtHome, "legacy", "world.toml")
	if _, err := os.Stat(tomlPath); !os.IsNotExist(err) {
		t.Fatal("world.toml should not exist yet")
	}

	// Init the pre-Arc1 world — should succeed (adoption).
	out, err = runGT(t, gtHome, "world", "init", "legacy")
	if err != nil {
		t.Fatalf("world init legacy failed: %v: %s", err, out)
	}

	// Verify world.toml created.
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		t.Fatal("world.toml not created after adoption")
	}
}

func TestWorldList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Init two worlds.
	runGT(t, gtHome, "world", "init", "alpha", "--source-repo=/tmp/a")
	runGT(t, gtHome, "world", "init", "beta", "--source-repo=/tmp/b")

	out, err := runGT(t, gtHome, "world", "list")
	if err != nil {
		t.Fatalf("world list failed: %v: %s", err, out)
	}

	if !strings.Contains(out, "alpha") {
		t.Errorf("output missing 'alpha': %s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("output missing 'beta': %s", out)
	}
	if !strings.Contains(out, "2 world(s)") {
		t.Errorf("output missing '2 world(s)': %s", out)
	}
}

func TestWorldListEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "world", "list")
	if err != nil {
		t.Fatalf("world list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "No worlds initialized") {
		t.Errorf("expected 'No worlds initialized', got: %s", out)
	}
}

func TestWorldListJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	runGT(t, gtHome, "world", "init", "myworld", "--source-repo=/tmp/fakerepo")

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
	if items[0]["name"] != "myworld" {
		t.Fatalf("expected name 'myworld', got %v", items[0]["name"])
	}
}

func TestWorldStatusBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	runGT(t, gtHome, "world", "init", "myworld", "--source-repo=/tmp/fakerepo")

	out, err := runGT(t, gtHome, "world", "status", "myworld")
	if err != nil {
		t.Fatalf("world status failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Config:") {
		t.Errorf("output missing 'Config:' section: %s", out)
	}
	if !strings.Contains(out, "Source repo:") {
		t.Errorf("output missing 'Source repo:': %s", out)
	}
}

func TestWorldStatusNotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "world", "status", "nonexistent")
	if err == nil {
		t.Fatalf("expected error, got success: %s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %s", out)
	}
}

func TestWorldDeleteBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	runGT(t, gtHome, "world", "init", "myworld", "--source-repo=/tmp/fakerepo")

	out, err := runGT(t, gtHome, "world", "delete", "myworld", "--confirm")
	if err != nil {
		t.Fatalf("world delete failed: %v: %s", err, out)
	}

	// Verify world.toml gone.
	tomlPath := filepath.Join(gtHome, "myworld", "world.toml")
	if _, err := os.Stat(tomlPath); !os.IsNotExist(err) {
		t.Fatal("world.toml still exists after delete")
	}

	// Verify myworld.db gone.
	dbPath := filepath.Join(gtHome, ".store", "myworld.db")
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("myworld.db still exists after delete")
	}

	// Verify myworld/ directory gone.
	worldDir := filepath.Join(gtHome, "myworld")
	if _, err := os.Stat(worldDir); !os.IsNotExist(err) {
		t.Fatal("myworld/ directory still exists after delete")
	}
}

func TestWorldDeleteNoConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	runGT(t, gtHome, "world", "init", "myworld", "--source-repo=/tmp/fakerepo")

	out, err := runGT(t, gtHome, "world", "delete", "myworld")
	if err != nil {
		t.Fatalf("world delete (no --confirm) failed: %v: %s", err, out)
	}

	// Output should show deletion plan.
	if !strings.Contains(out, "permanently delete") {
		t.Errorf("expected deletion plan in output: %s", out)
	}
	if !strings.Contains(out, "--confirm") {
		t.Errorf("expected '--confirm' hint in output: %s", out)
	}

	// Verify world.toml still exists.
	tomlPath := filepath.Join(gtHome, "myworld", "world.toml")
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		t.Fatal("world.toml should still exist without --confirm")
	}
}

func TestWorldDeleteNotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "world", "delete", "nonexistent", "--confirm")
	if err == nil {
		t.Fatalf("expected error, got success: %s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %s", out)
	}
}
