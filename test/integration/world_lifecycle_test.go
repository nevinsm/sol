package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

func TestWorldInitBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "world", "init", "myworld")
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

	// Create a real git repo as source.
	sourceRepo := t.TempDir()
	gitRun(t, sourceRepo, "init")
	gitRun(t, sourceRepo, "config", "user.email", "test@test.com")
	gitRun(t, sourceRepo, "config", "user.name", "Test")
	gitRun(t, sourceRepo, "commit", "--allow-empty", "-m", "init")

	out, err := runGT(t, gtHome, "world", "init", "myworld", "--source-repo="+sourceRepo)
	if err != nil {
		t.Fatalf("world init failed: %v: %s", err, out)
	}

	// Verify world.toml contains source_repo.
	tomlPath := filepath.Join(gtHome, "myworld", "world.toml")
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), sourceRepo) {
		t.Fatalf("world.toml does not contain source_repo: %s", data)
	}

	// Verify managed clone exists.
	repoDir := filepath.Join(gtHome, "myworld", "repo")
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		t.Fatal("managed clone not created")
	}
}

func TestWorldInitAlreadyExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Init once — success.
	out, err := runGT(t, gtHome, "world", "init", "myworld")
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

	// Create a world DB directly (simulate pre-Arc1 — DB exists, no world.toml).
	t.Setenv("SOL_HOME", gtHome)
	s, err := store.OpenWorld("legacy")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	_, err = s.CreateWorkItem("Old item", "", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	s.Close()

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
	out, err := runGT(t, gtHome, "world", "init", "legacy")
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
	if _, err := runGT(t, gtHome, "world", "init", "alpha"); err != nil {
		t.Fatalf("setup: world init alpha failed: %v", err)
	}
	if _, err := runGT(t, gtHome, "world", "init", "beta"); err != nil {
		t.Fatalf("setup: world init beta failed: %v", err)
	}

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

	if _, err := runGT(t, gtHome, "world", "init", "myworld"); err != nil {
		t.Fatalf("setup: world init failed: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "status", "--world=myworld")
	if err != nil {
		t.Fatalf("world status failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Config") {
		t.Errorf("output missing 'Config' section: %s", out)
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

	out, err := runGT(t, gtHome, "world", "status", "--world=nonexistent")
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

	if _, err := runGT(t, gtHome, "world", "init", "myworld"); err != nil {
		t.Fatalf("setup: world init failed: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "delete", "--world=myworld", "--confirm")
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

	if _, err := runGT(t, gtHome, "world", "init", "myworld"); err != nil {
		t.Fatalf("setup: world init failed: %v", err)
	}

	out, err := runGT(t, gtHome, "world", "delete", "--world=myworld")
	if err == nil {
		t.Fatal("world delete (no --confirm) should exit non-zero")
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

	out, err := runGT(t, gtHome, "world", "delete", "--world=nonexistent", "--confirm")
	if err == nil {
		t.Fatalf("expected error, got success: %s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %s", out)
	}
}

func TestWorldStatusJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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

	out, err := runGT(t, gtHome, "world", "status", "--world=myworld", "--json")
	if err != nil {
		t.Fatalf("world status --json failed: %v: %s", err, out)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v: %s", err, out)
	}

	// Verify config section is present with source_repo (snake_case JSON keys).
	cfg, ok := result["config"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'config' object in JSON output, got: %s", out)
	}
	world, ok := cfg["world"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'world' object in config, got: %v", cfg)
	}
	if world["source_repo"] != sourceRepo {
		t.Fatalf("expected source_repo %q, got: %v", sourceRepo, world["source_repo"])
	}
}

func TestWorldInitWithoutSourceRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Run world init without --source-repo from a non-git directory.
	cmd := runGTWithDir(t, gtHome, "/tmp", "world", "init", "myworld")
	if cmd.err != nil {
		t.Fatalf("world init without --source-repo failed: %v: %s", cmd.err, cmd.out)
	}

	// Verify world.toml exists.
	tomlPath := filepath.Join(gtHome, "myworld", "world.toml")
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		t.Fatal("world.toml not created")
	}

	// Verify source_repo is empty in the TOML.
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "source_repo = \"/") {
		t.Fatalf("expected empty source_repo, but found a path in world.toml: %s", data)
	}
}

func TestWorldDeleteCleansUpAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	// Register an agent.
	out, err := runGT(t, gtHome, "agent", "create", "Toast", "--world=myworld")
	if err != nil {
		t.Fatalf("agent create failed: %v: %s", err, out)
	}

	// Delete the world.
	out, err = runGT(t, gtHome, "world", "delete", "--world=myworld", "--confirm")
	if err != nil {
		t.Fatalf("world delete failed: %v: %s", err, out)
	}

	// Verify agent is gone from sphere.db.
	t.Setenv("SOL_HOME", gtHome)
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	agents, err := sphereStore.ListAgents("myworld", "")
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected 0 agents after delete, got %d", len(agents))
	}
}

func TestWorldDeleteCleansUpCaravanItems(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	// Create a work item.
	itemID, err := runGT(t, gtHome, "store", "create", "--world=myworld", "--title=test item")
	if err != nil {
		t.Fatalf("store create failed: %v: %s", err, itemID)
	}
	itemID = strings.TrimSpace(itemID)

	// Create a caravan with the work item.
	caravanOut, err := runGT(t, gtHome, "caravan", "create", "test-caravan", itemID, "--world=myworld")
	if err != nil {
		t.Fatalf("caravan create failed: %v: %s", err, caravanOut)
	}
	// Extract caravan ID from output like: Created caravan car-a365ed87: "test-caravan" (1 items)
	var caravanID string
	for _, word := range strings.Fields(caravanOut) {
		if strings.HasPrefix(word, "car-") {
			caravanID = strings.TrimSuffix(word, ":")
			break
		}
	}
	if caravanID == "" {
		t.Fatalf("could not extract caravan ID from output: %s", caravanOut)
	}

	// Delete the world.
	out, err := runGT(t, gtHome, "world", "delete", "--world=myworld", "--confirm")
	if err != nil {
		t.Fatalf("world delete failed: %v: %s", err, out)
	}

	// Verify caravan still exists but items are gone.
	t.Setenv("SOL_HOME", gtHome)
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	caravan, err := sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatalf("get caravan: %v", err)
	}
	if caravan == nil {
		t.Fatal("caravan should still exist after world delete")
	}

	items, err := sphereStore.ListCaravanItems(caravanID)
	if err != nil {
		t.Fatalf("list caravan items: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 caravan items after world delete, got %d", len(items))
	}
}

func TestWorldListJSONEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "world", "list", "--json")
	if err != nil {
		t.Fatalf("world list --json failed: %v: %s", err, out)
	}

	// Output should be valid JSON: []
	var items []interface{}
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("failed to parse JSON: %v: %s", err, out)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty array, got %d items", len(items))
	}
}

func TestWorldInitInvalidName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	cases := []struct {
		name  string
		match string
	}{
		{".hidden", "invalid world name"},
		{"has spaces", "invalid world name"},
		{"", "accepts 1 arg(s)"}, // cobra rejects missing arg before our validation
		{"store", "reserved"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"world", "init", tc.name}
			if tc.name == "" {
				args = []string{"world", "init"} // cobra will reject missing arg
			}
			out, err := runGT(t, gtHome, args...)
			if err == nil {
				t.Fatalf("expected error for name %q, got success: %s", tc.name, out)
			}
			if !strings.Contains(out, tc.match) {
				t.Fatalf("expected %q in error for name %q, got: %s", tc.match, tc.name, out)
			}
		})
	}
}

func TestWorldDeleteRefusesWithActiveSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Check that tmux is available.
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available, skipping")
	}

	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Create a world.
	if _, err := runGT(t, gtHome, "world", "init", "deltest"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Start a tmux session with the world's naming convention.
	// Session name format: sol-{world}-{agent}
	sessionName := "sol-deltest-TestAgent"
	if err := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "sleep", "60").Run(); err != nil {
		t.Fatalf("setup: failed to create tmux session: %v", err)
	}
	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	// Write session metadata so mgr.List() discovers it.
	sessDir := filepath.Join(gtHome, ".runtime", "sessions")
	os.MkdirAll(sessDir, 0o755)
	meta := `{"name":"` + sessionName + `","role":"agent","world":"deltest","workdir":"/tmp","started_at":"2026-01-01T00:00:00Z"}`
	os.WriteFile(filepath.Join(sessDir, sessionName+".json"), []byte(meta), 0o644)

	// Attempt to delete — should be refused.
	out, err := runGT(t, gtHome, "world", "delete", "--world=deltest", "--confirm")
	if err == nil {
		t.Fatalf("expected error with active session, got success: %s", out)
	}
	if !strings.Contains(out, "active session") {
		t.Fatalf("expected 'active session' error, got: %s", out)
	}
}
