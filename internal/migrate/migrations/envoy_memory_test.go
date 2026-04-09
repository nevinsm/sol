package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/migrate"
	"github.com/nevinsm/sol/internal/store"
)

// setupEnv isolates a test by pointing SOL_HOME at a private temp dir and
// returning an open sphere store bound to it. Tests that need a world or
// envoy register them explicitly via the returned store.
func setupEnv(t *testing.T) (*store.SphereStore, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SOL_HOME", home)
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere: %v", err)
	}
	t.Cleanup(func() { ss.Close() })
	return ss, home
}

// seedEnvoy creates the on-disk layout for an envoy: the worktree dir and
// an optional populated .brief/ dir. files is a map of relative path →
// content written inside .brief/ (empty map means brief exists but is empty;
// nil means no brief dir is created at all).
func seedEnvoy(t *testing.T, solHome, world, agent string, files map[string]string) {
	t.Helper()
	envoyDir := filepath.Join(solHome, world, "envoys", agent)
	worktree := filepath.Join(envoyDir, "worktree")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	if files == nil {
		return
	}
	briefDir := filepath.Join(worktree, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatalf("mkdir brief: %v", err)
	}
	for name, body := range files {
		p := filepath.Join(briefDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir parent: %v", err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write brief file %q: %v", name, err)
		}
	}
}

// writeWorldConfig writes a minimal world.toml that forces the envoy role
// onto the given runtime. Used for codex-envoy coverage.
func writeWorldConfig(t *testing.T, solHome, world, runtime string) {
	t.Helper()
	dir := filepath.Join(solHome, world)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir world dir: %v", err)
	}
	content := "[agents.runtimes]\nenvoy = \"" + runtime + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "world.toml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write world.toml: %v", err)
	}
}

func registerWorldAndEnvoy(t *testing.T, ss *store.SphereStore, world, agent string) {
	t.Helper()
	if err := ss.RegisterWorld(world, ""); err != nil {
		t.Fatalf("register world: %v", err)
	}
	if _, err := ss.CreateAgent(agent, world, "envoy"); err != nil {
		t.Fatalf("create agent: %v", err)
	}
}

// lookupMigration returns the envoy-memory Migration from the global
// registry. The init() in envoy_memory.go registers it on package load.
func lookupMigration(t *testing.T) migrate.Migration {
	t.Helper()
	m, ok := migrate.Get("envoy-memory")
	if !ok {
		t.Fatal("envoy-memory migration not registered")
	}
	return m
}

// ----- Detect -----

func TestDetectCountsEnvoysWithBriefs(t *testing.T) {
	ss, home := setupEnv(t)
	m := lookupMigration(t)

	// Envoy A: has brief content.
	registerWorldAndEnvoy(t, ss, "alpha", "Polaris")
	seedEnvoy(t, home, "alpha", "Polaris", map[string]string{
		"memory.md":             "# memory\n- a\n- b\n",
		"broker-abstraction.md": "# broker\nnotes\n",
	})

	// Envoy B: fresh, no brief.
	registerWorldAndEnvoy(t, ss, "beta", "Comet")
	seedEnvoy(t, home, "beta", "Comet", nil)

	res, err := m.Detect(migrate.Context{SphereStore: ss})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !res.Needed {
		t.Fatalf("expected Needed=true, got false (reason=%q)", res.Reason)
	}
	if !strings.Contains(res.Reason, "1") {
		t.Errorf("expected count of 1 in reason, got %q", res.Reason)
	}
}

func TestDetectNotNeededForFreshSphere(t *testing.T) {
	ss, home := setupEnv(t)
	m := lookupMigration(t)

	registerWorldAndEnvoy(t, ss, "alpha", "Polaris")
	seedEnvoy(t, home, "alpha", "Polaris", nil)

	res, err := m.Detect(migrate.Context{SphereStore: ss})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if res.Needed {
		t.Errorf("expected Needed=false, got true (reason=%q)", res.Reason)
	}
}

func TestDetectCountsLegacyPlaceholders(t *testing.T) {
	ss, home := setupEnv(t)
	m := lookupMigration(t)

	registerWorldAndEnvoy(t, ss, "alpha", "Polaris")
	seedEnvoy(t, home, "alpha", "Polaris", nil)
	// Empty legacy placeholder at <envoyDir>/.brief (not inside worktree).
	legacy := filepath.Join(home, "alpha", "envoys", "Polaris", ".brief")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := m.Detect(migrate.Context{SphereStore: ss})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !res.Needed {
		t.Errorf("expected Needed=true for legacy placeholder, got false")
	}
	if !strings.Contains(res.Reason, "placeholder") {
		t.Errorf("expected reason to mention placeholder, got %q", res.Reason)
	}
}

// ----- Run -----

func TestRunCopiesMemoryAndTopicFiles(t *testing.T) {
	ss, home := setupEnv(t)
	m := lookupMigration(t)

	registerWorldAndEnvoy(t, ss, "alpha", "Polaris")
	seedEnvoy(t, home, "alpha", "Polaris", map[string]string{
		"memory.md":             "# Polaris memory\n- decision: use X\n",
		"broker-abstraction.md": "# broker notes\ndetails\n",
		".session_start":        "marker",
	})

	res, err := m.Run(migrate.Context{SphereStore: ss}, migrate.RunOpts{Confirm: true})
	if err != nil {
		t.Fatalf("Run: %v (summary=%q)", err, res.Summary)
	}

	memDir := filepath.Join(home, "alpha", "envoys", "Polaris", "memory")
	// MEMORY.md created from memory.md.
	got, err := os.ReadFile(filepath.Join(memDir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if !strings.Contains(string(got), "Polaris memory") {
		t.Errorf("MEMORY.md missing original content: %q", got)
	}
	// Topic file preserved at sibling level.
	if _, err := os.Stat(filepath.Join(memDir, "broker-abstraction.md")); err != nil {
		t.Errorf("broker-abstraction.md missing: %v", err)
	}
	// Non-.md sidecar preserved verbatim for audit.
	if _, err := os.Stat(filepath.Join(memDir, ".session_start")); err != nil {
		t.Errorf(".session_start not preserved: %v", err)
	}
	// Old memory.md should NOT exist at the destination (it was renamed).
	if _, err := os.Stat(filepath.Join(memDir, "memory.md")); !os.IsNotExist(err) {
		t.Errorf("memory.md should have been renamed to MEMORY.md, stat err = %v", err)
	}
}

func TestRunAppendsMigratedTopicNotesSection(t *testing.T) {
	ss, home := setupEnv(t)
	m := lookupMigration(t)

	registerWorldAndEnvoy(t, ss, "alpha", "Polaris")
	seedEnvoy(t, home, "alpha", "Polaris", map[string]string{
		"memory.md":             "# existing index\n",
		"broker-abstraction.md": "# broker\n",
		"runtime-notes.md":      "# runtime\n",
	})

	if _, err := m.Run(migrate.Context{SphereStore: ss}, migrate.RunOpts{Confirm: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	memFile := filepath.Join(home, "alpha", "envoys", "Polaris", "memory", "MEMORY.md")
	body, err := os.ReadFile(memFile)
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "## Migrated Topic Notes") {
		t.Errorf("MEMORY.md missing 'Migrated Topic Notes' section:\n%s", s)
	}
	if !strings.Contains(s, "broker-abstraction.md") || !strings.Contains(s, "runtime-notes.md") {
		t.Errorf("MEMORY.md missing topic links:\n%s", s)
	}
	// Original content preserved above the new section.
	if !strings.Contains(s, "existing index") {
		t.Errorf("original MEMORY.md content lost:\n%s", s)
	}
}

func TestRunDeletesSourceOnSuccess(t *testing.T) {
	ss, home := setupEnv(t)
	m := lookupMigration(t)

	registerWorldAndEnvoy(t, ss, "alpha", "Polaris")
	seedEnvoy(t, home, "alpha", "Polaris", map[string]string{
		"memory.md": "# memory\n",
	})

	if _, err := m.Run(migrate.Context{SphereStore: ss}, migrate.RunOpts{Confirm: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	briefDir := filepath.Join(home, "alpha", "envoys", "Polaris", "worktree", ".brief")
	if _, err := os.Stat(briefDir); !os.IsNotExist(err) {
		t.Errorf("source .brief/ should have been removed after copy, stat err = %v", err)
	}
}

func TestRunSkipsAlreadyMigrated(t *testing.T) {
	ss, home := setupEnv(t)
	m := lookupMigration(t)

	registerWorldAndEnvoy(t, ss, "alpha", "Polaris")
	seedEnvoy(t, home, "alpha", "Polaris", map[string]string{
		"memory.md": "# new brief\n",
	})
	// Pre-populate the destination to simulate an already-migrated envoy.
	memDir := filepath.Join(home, "alpha", "envoys", "Polaris", "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("# existing MEMORY\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := m.Run(migrate.Context{SphereStore: ss}, migrate.RunOpts{Confirm: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Summary, "skipped") {
		t.Errorf("expected summary to mention skipped, got %q", res.Summary)
	}
	// Destination MEMORY.md unchanged.
	got, _ := os.ReadFile(filepath.Join(memDir, "MEMORY.md"))
	if string(got) != "# existing MEMORY\n" {
		t.Errorf("MEMORY.md was overwritten: %q", got)
	}
	// Source brief preserved (skipped means no mutation at all).
	briefDir := filepath.Join(home, "alpha", "envoys", "Polaris", "worktree", ".brief")
	if _, err := os.Stat(briefDir); err != nil {
		t.Errorf("expected source .brief/ to be preserved on skip, got %v", err)
	}
}

func TestRunSkipsCodexEnvoys(t *testing.T) {
	ss, home := setupEnv(t)
	m := lookupMigration(t)

	writeWorldConfig(t, home, "alpha", "codex")
	registerWorldAndEnvoy(t, ss, "alpha", "Polaris")
	seedEnvoy(t, home, "alpha", "Polaris", map[string]string{
		"memory.md": "# memory\n",
	})

	res, err := m.Run(migrate.Context{SphereStore: ss}, migrate.RunOpts{Confirm: true})
	if err != nil {
		t.Fatalf("Run: %v (summary=%q)", err, res.Summary)
	}
	if !strings.Contains(res.Summary, "skipped") {
		t.Errorf("expected codex envoy to be counted as skipped, got %q", res.Summary)
	}
	// No memory dir created for codex envoy.
	memDir := filepath.Join(home, "alpha", "envoys", "Polaris", "memory")
	if _, err := os.Stat(memDir); !os.IsNotExist(err) {
		t.Errorf("codex envoy should not have memory dir, stat err = %v", err)
	}
	// Source brief preserved — migration refused to touch it.
	briefDir := filepath.Join(home, "alpha", "envoys", "Polaris", "worktree", ".brief")
	if _, err := os.Stat(briefDir); err != nil {
		t.Errorf("codex envoy .brief/ should be preserved: %v", err)
	}
}

func TestRunIdempotent(t *testing.T) {
	ss, home := setupEnv(t)
	m := lookupMigration(t)

	registerWorldAndEnvoy(t, ss, "alpha", "Polaris")
	seedEnvoy(t, home, "alpha", "Polaris", map[string]string{
		"memory.md":     "# memory\n",
		"some-topic.md": "# topic\n",
	})

	if _, err := m.Run(migrate.Context{SphereStore: ss}, migrate.RunOpts{Confirm: true}); err != nil {
		t.Fatalf("Run #1: %v", err)
	}
	// Second run must be a clean no-op (everything is detected as already
	// migrated).
	res, err := m.Run(migrate.Context{SphereStore: ss}, migrate.RunOpts{Confirm: true})
	if err != nil {
		t.Fatalf("Run #2: %v", err)
	}
	if strings.Contains(res.Summary, "migrated 1") {
		t.Errorf("second run should not re-migrate, got summary %q", res.Summary)
	}

	// MEMORY.md should still be intact.
	memFile := filepath.Join(home, "alpha", "envoys", "Polaris", "memory", "MEMORY.md")
	if _, err := os.Stat(memFile); err != nil {
		t.Errorf("MEMORY.md should exist after second run: %v", err)
	}
}

func TestRunRemovesEmptyLegacyPlaceholder(t *testing.T) {
	ss, home := setupEnv(t)
	m := lookupMigration(t)

	registerWorldAndEnvoy(t, ss, "alpha", "Polaris")
	seedEnvoy(t, home, "alpha", "Polaris", nil)
	legacy := filepath.Join(home, "alpha", "envoys", "Polaris", ".brief")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Run(migrate.Context{SphereStore: ss}, migrate.RunOpts{Confirm: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy placeholder should be removed, stat err = %v", err)
	}
}
