package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCastUsesConfigSourceRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	isolateTmux(t)
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Init world with source repo pointing to a real git repo.
	sourceRepo := setupGitRepo(t)
	out, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo="+sourceRepo)
	if err != nil {
		t.Fatalf("world init failed: %v: %s", err, out)
	}

	// Create a writ.
	itemID, err := runGT(t, gtHome, "writ", "create", "--world=myworld", "--title=test cast config")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, itemID)
	}
	itemID = strings.TrimSpace(itemID)

	// Run cast from /tmp (NOT the git repo) — should still work via config.
	cmd := runGTWithDir(t, gtHome, "/tmp", "cast", itemID, "--world=myworld")
	if cmd.err != nil {
		t.Fatalf("cast failed (should use config source_repo): %v: %s", cmd.err, cmd.out)
	}
	if !strings.Contains(cmd.out, "Cast "+itemID) {
		t.Errorf("cast output missing expected text: %s", cmd.out)
	}
}

func TestDispatchCapacityEnforced(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	isolateTmux(t)
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	sourceRepo := setupGitRepo(t)
	out, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo="+sourceRepo)
	if err != nil {
		t.Fatalf("world init failed: %v: %s", err, out)
	}

	// Set capacity = 1.
	writeWorldTOML(t, gtHome, "myworld", sourceRepo, map[string]string{
		"agents": "capacity = 1",
	})

	// Create 2 writs.
	item1, err := runGT(t, gtHome, "writ", "create", "--world=myworld", "--title=item 1")
	if err != nil {
		t.Fatalf("writ create 1 failed: %v: %s", err, item1)
	}
	item1 = strings.TrimSpace(item1)

	item2, err := runGT(t, gtHome, "writ", "create", "--world=myworld", "--title=item 2")
	if err != nil {
		t.Fatalf("writ create 2 failed: %v: %s", err, item2)
	}
	item2 = strings.TrimSpace(item2)

	// Cast first item → should succeed.
	cmd := runGTWithDir(t, gtHome, sourceRepo, "cast", item1, "--world=myworld")
	if cmd.err != nil {
		t.Fatalf("first cast failed: %v: %s", cmd.err, cmd.out)
	}

	// Cast second item → should fail with capacity error.
	cmd = runGTWithDir(t, gtHome, sourceRepo, "cast", item2, "--world=myworld")
	if cmd.err == nil {
		t.Fatalf("second cast should have failed with capacity error, got: %s", cmd.out)
	}
	if !strings.Contains(cmd.out, "reached agent capacity") {
		t.Fatalf("expected 'reached agent capacity' error, got: %s", cmd.out)
	}
}

func TestAgentCreateCapacityEnforced(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	sourceRepo := setupGitRepo(t)
	out, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo="+sourceRepo)
	if err != nil {
		t.Fatalf("world init failed: %v: %s", err, out)
	}

	// Set capacity = 1.
	writeWorldTOML(t, gtHome, "myworld", sourceRepo, map[string]string{
		"agents": "capacity = 1",
	})

	// First agent create → should succeed.
	out, err = runGT(t, gtHome, "agent", "create", "Alpha", "--world=myworld")
	if err != nil {
		t.Fatalf("first agent create failed: %v: %s", err, out)
	}

	// Second agent create → should fail with capacity error.
	out, err = runGT(t, gtHome, "agent", "create", "Bravo", "--world=myworld")
	if err == nil {
		t.Fatalf("second agent create should have failed with capacity error, got: %s", out)
	}
	if !strings.Contains(out, "reached agent capacity") {
		t.Fatalf("expected 'reached agent capacity' error, got: %s", out)
	}
}

func TestAgentCreateCapacitySkipsNonAgentRoles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	sourceRepo := setupGitRepo(t)
	out, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo="+sourceRepo)
	if err != nil {
		t.Fatalf("world init failed: %v: %s", err, out)
	}

	// Set capacity = 1.
	writeWorldTOML(t, gtHome, "myworld", sourceRepo, map[string]string{
		"agents": "capacity = 1",
	})

	// Create one outpost agent to fill capacity.
	out, err = runGT(t, gtHome, "agent", "create", "Alpha", "--world=myworld")
	if err != nil {
		t.Fatalf("agent create failed: %v: %s", err, out)
	}

	// Creating an envoy should succeed despite capacity being full.
	out, err = runGT(t, gtHome, "agent", "create", "Consul", "--world=myworld", "--role=envoy")
	if err != nil {
		t.Fatalf("envoy create should bypass capacity check, got: %v: %s", err, out)
	}
}

func TestDispatchCapacityZeroUnlimited(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	isolateTmux(t)
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	sourceRepo := setupGitRepo(t)
	out, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo="+sourceRepo)
	if err != nil {
		t.Fatalf("world init failed: %v: %s", err, out)
	}

	// Default capacity = 0 (unlimited). Cast multiple items.
	for i := 0; i < 3; i++ {
		itemID, err := runGT(t, gtHome, "writ", "create", "--world=myworld", "--title=item")
		if err != nil {
			t.Fatalf("writ create %d failed: %v: %s", i, err, itemID)
		}
		itemID = strings.TrimSpace(itemID)

		cmd := runGTWithDir(t, gtHome, sourceRepo, "cast", itemID, "--world=myworld")
		if cmd.err != nil {
			t.Fatalf("cast %d failed: %v: %s", i, cmd.err, cmd.out)
		}
	}
}

func TestDispatchNamePoolFromConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	isolateTmux(t)
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	sourceRepo := setupGitRepo(t)
	out, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo="+sourceRepo)
	if err != nil {
		t.Fatalf("world init failed: %v: %s", err, out)
	}

	// Create custom name pool file.
	customPoolPath := filepath.Join(gtHome, "custom-names.txt")
	os.WriteFile(customPoolPath, []byte("Mercury\nVenus\nEarth\n"), 0o644)

	// Write name_pool_path to world.toml.
	writeWorldTOML(t, gtHome, "myworld", sourceRepo, map[string]string{
		"agents": "name_pool_path = \"" + customPoolPath + "\"",
	})

	// Create and cast item.
	itemID, err := runGT(t, gtHome, "writ", "create", "--world=myworld", "--title=test name pool")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, itemID)
	}
	itemID = strings.TrimSpace(itemID)

	cmd := runGTWithDir(t, gtHome, sourceRepo, "cast", itemID, "--world=myworld")
	if cmd.err != nil {
		t.Fatalf("cast failed: %v: %s", cmd.err, cmd.out)
	}

	// Agent name should come from custom pool.
	if !strings.Contains(cmd.out, "Mercury") {
		t.Errorf("expected agent name from custom pool (Mercury), got: %s", cmd.out)
	}
}

// --- helpers ---

type cmdResult struct {
	out string
	err error
}

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "-C", dir, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %s: %v", out, err)
	}
	// Configure git user — required in environments without global git config.
	for _, args := range [][]string{
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	} {
		cmd = exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %s: %v", args[0], out, err)
		}
	}
	cmd = exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", "initial")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %s: %v", out, err)
	}
	return dir
}

func runGTWithDir(t *testing.T, gtHome, dir string, args ...string) cmdResult {
	t.Helper()
	bin := gtBin(t)
	c := exec.Command(bin, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "SOL_HOME="+gtHome)
	out, err := c.CombinedOutput()
	return cmdResult{out: strings.TrimSpace(string(out)), err: err}
}

// writeWorldTOML writes a world.toml with the source_repo and any additional
// section overrides. The overrides map keys are section names (e.g., "agents",
// "forge") and values are the TOML content within that section.
func writeWorldTOML(t *testing.T, gtHome, world, sourceRepo string, overrides map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("[world]\nsource_repo = \"" + sourceRepo + "\"\n\n")

	// Write [agents] section.
	b.WriteString("[agents]\n")
	if agents, ok := overrides["agents"]; ok {
		b.WriteString(agents + "\n")
	}
	b.WriteString("\n")

	// Write [forge] section.
	b.WriteString("[forge]\n")
	if forge, ok := overrides["forge"]; ok {
		b.WriteString(forge + "\n")
	}
	b.WriteString("\n")

	tomlPath := filepath.Join(gtHome, world, "world.toml")
	if err := os.WriteFile(tomlPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("failed to write world.toml: %v", err)
	}
}
