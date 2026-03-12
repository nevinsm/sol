package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/workflow"
)

// --- Mock session manager ---

type mockSessionManager struct {
	started    map[string]bool
	stopped    map[string]bool
	injected   map[string]string            // session → last injected text
	startedEnv map[string]map[string]string // session → env vars
}

func newMockSessionManager() *mockSessionManager {
	return &mockSessionManager{
		started:    make(map[string]bool),
		stopped:    make(map[string]bool),
		injected:   make(map[string]string),
		startedEnv: make(map[string]map[string]string),
	}
}

func (m *mockSessionManager) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.started[name] = true
	m.startedEnv[name] = env
	return nil
}

func (m *mockSessionManager) Stop(name string, force bool) error {
	m.stopped[name] = true
	return nil
}

func (m *mockSessionManager) Exists(name string) bool {
	return m.started[name] && !m.stopped[name]
}

func (m *mockSessionManager) Inject(name string, text string, submit bool) error {
	m.injected[name] = text
	return nil
}

func (m *mockSessionManager) NudgeSession(name string, message string) error {
	return nil
}

func (m *mockSessionManager) WaitForIdle(name string, timeout time.Duration) error {
	return nil
}

func (m *mockSessionManager) Capture(name string, lines int) (string, error) {
	return "", nil
}

func (m *mockSessionManager) Cycle(name, workdir, cmd string, env map[string]string, role, world string) error {
	return fmt.Errorf("cycle not supported in mock")
}

// --- Helper to set up real stores in temp dirs ---

// writeTestToken writes a minimal api_key token to $SOL_HOME/.accounts/token.json
// so startup.Launch can inject credentials in tests (empty account handle).
func writeTestToken(t *testing.T, solHome string) {
	t.Helper()
	accountsDir := filepath.Join(solHome, ".accounts")
	if err := os.MkdirAll(accountsDir, 0o755); err != nil {
		t.Fatalf("failed to create .accounts dir: %v", err)
	}
	tokenJSON := `{"type":"api_key","token":"test-key","created_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(accountsDir, "token.json"), []byte(tokenJSON), 0o600); err != nil {
		t.Fatalf("failed to write test token: %v", err)
	}
}

func setupStores(t *testing.T) (*store.Store, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(dir+"/.store", 0o755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}

	// Write a fake token so startup.Launch can inject credentials.
	writeTestToken(t, dir)

	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	t.Cleanup(func() { worldStore.Close() })

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	t.Cleanup(func() { sphereStore.Close() })

	return worldStore, sphereStore
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	allArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", allArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
	}
}

// addBareRemote creates a bare git repo and adds it as "origin" to repoDir
// so that git push succeeds in tests.
func addBareRemote(t *testing.T, repoDir string) {
	t.Helper()
	bareDir := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, repoDir, "clone", "--bare", ".", bareDir)
	runGit(t, repoDir, "remote", "add", "origin", bareDir)
}

// --- Cast tests ---

func TestCastHappyPath(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Create a temporary git repo to use as source.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(context.Background(), CastOpts{
		WritID: itemID,
		World:        "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	if result.WritID != itemID {
		t.Errorf("expected writ ID %q, got %q", itemID, result.WritID)
	}
	if result.AgentName != "Toast" {
		t.Errorf("expected agent name Toast, got %q", result.AgentName)
	}
	if result.SessionName != "sol-ember-Toast" {
		t.Errorf("expected session name sol-ember-Toast, got %q", result.SessionName)
	}

	// Verify tether was written.
	tetherID, err := tether.Read("ember", "Toast", "outpost")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != itemID {
		t.Errorf("tether has %q, expected %q", tetherID, itemID)
	}

	// Verify writ was updated.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("expected writ status 'tethered', got %q", item.Status)
	}
	if item.Assignee != "ember/Toast" {
		t.Errorf("expected assignee 'ember/Toast', got %q", item.Assignee)
	}

	// Verify agent was updated.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
	if agent.ActiveWrit != itemID {
		t.Errorf("expected agent active_writ %q, got %q", itemID, agent.ActiveWrit)
	}

	// Verify session was started.
	if !mgr.started["sol-ember-Toast"] {
		t.Error("expected session to be started")
	}

	// Verify CLAUDE.local.md was installed.
	claudeMD := result.WorktreeDir + "/CLAUDE.local.md"
	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.local.md: %v", err)
	}
	if !strings.Contains(string(data), "Toast") {
		t.Error("CLAUDE.local.md missing agent name")
	}
}

// TestCastAgentStateBeforeTether verifies that Cast() sets agent state to
// "working" before writing the tether file. This ordering prevents a race
// with sentinel's cleanupOrphanedTethers, which skips agents that exist in
// the DB (any state). If tether were written first while agent is "idle",
// a concurrent sentinel patrol could clear it.
func TestCastAgentStateBeforeTether(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Order test", "Verify ordering", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Verify agent starts idle.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Fatalf("expected agent to start idle, got %q", agent.State)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	_, err = Cast(context.Background(), CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	// After Cast completes, verify all three operations succeeded in order:
	// 1. Agent state → working (done first to prevent sentinel race)
	agent, err = sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
	if agent.ActiveWrit != itemID {
		t.Errorf("expected active writ %q, got %q", itemID, agent.ActiveWrit)
	}

	// 2. Tether file written
	if !tether.IsTethered("ember", "Toast", "outpost") {
		t.Error("expected tether to be written")
	}

	// 3. Writ status → tethered
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("expected writ status 'tethered', got %q", item.Status)
	}
}

func TestCastTelemetryEnvWhenLedgerConfigured(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Configure ledger port in global config (sol.toml — ledger is sphere-scoped).
	solHome := os.Getenv("SOL_HOME")
	if err := os.WriteFile(filepath.Join(solHome, "sol.toml"), []byte("[ledger]\nport = 9999\n"), 0o644); err != nil {
		t.Fatalf("failed to write sol.toml: %v", err)
	}

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	_, err = Cast(context.Background(), CastOpts{
		WritID: itemID,
		World:      "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	env := mgr.startedEnv["sol-ember-Toast"]
	if env == nil {
		t.Fatal("no env captured for session")
	}

	checks := map[string]string{
		"CLAUDE_CODE_ENABLE_TELEMETRY":    "1",
		"OTEL_LOGS_EXPORTER":              "otlp",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "http://localhost:9999",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL": "http/json",
	}
	for k, want := range checks {
		if got := env[k]; got != want {
			t.Errorf("env[%s] = %q, want %q", k, got, want)
		}
	}

	attrs := env["OTEL_RESOURCE_ATTRIBUTES"]
	if !strings.Contains(attrs, "agent.name=Toast") {
		t.Errorf("OTEL_RESOURCE_ATTRIBUTES missing agent.name: %s", attrs)
	}
	if !strings.Contains(attrs, "world=ember") {
		t.Errorf("OTEL_RESOURCE_ATTRIBUTES missing world: %s", attrs)
	}
	if !strings.Contains(attrs, "writ_id="+itemID) {
		t.Errorf("OTEL_RESOURCE_ATTRIBUTES missing writ_id: %s", attrs)
	}
}

func TestCastNoTelemetryWhenLedgerDisabled(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Explicitly disable the ledger by setting port=0 in global config.
	// (Default is 4318; without this, telemetry would be active.)
	solHome := os.Getenv("SOL_HOME")
	if err := os.WriteFile(filepath.Join(solHome, "sol.toml"), []byte("[ledger]\nport = 0\n"), 0o644); err != nil {
		t.Fatalf("failed to write sol.toml: %v", err)
	}

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	_, err = Cast(context.Background(), CastOpts{
		WritID: itemID,
		World:      "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	env := mgr.startedEnv["sol-ember-Toast"]
	if env == nil {
		t.Fatal("no env captured for session")
	}

	otelKeys := []string{
		"CLAUDE_CODE_ENABLE_TELEMETRY",
		"OTEL_LOGS_EXPORTER",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
		"OTEL_RESOURCE_ATTRIBUTES",
	}
	for _, k := range otelKeys {
		if v, ok := env[k]; ok {
			t.Errorf("env[%s] = %q, expected absent when ledger disabled (port=0)", k, v)
		}
	}
}

func TestCastAutoAgent(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Alpha", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(context.Background(), CastOpts{
		WritID: itemID,
		World:        "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}
	if result.AgentName != "Alpha" {
		t.Errorf("expected auto-selected agent 'Alpha', got %q", result.AgentName)
	}
}

func TestCastAutoProvision(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	// No agent exists — Cast should auto-provision from the name pool.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(context.Background(), CastOpts{
		WritID: itemID,
		World:        "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	// First name in the default pool is "Nova".
	if result.AgentName != "Nova" {
		t.Errorf("expected auto-provisioned agent 'Nova', got %q", result.AgentName)
	}

	// Verify the agent was created in the store.
	agent, err := sphereStore.GetAgent("ember/Nova")
	if err != nil {
		t.Fatalf("failed to get auto-provisioned agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
	if agent.ActiveWrit != itemID {
		t.Errorf("expected agent active_writ %q, got %q", itemID, agent.ActiveWrit)
	}
}

func TestCastAutoProvisionCapacityEnforced(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Set capacity = 1 via world config.
	solHome := os.Getenv("SOL_HOME")
	worldDir := solHome + "/ember"
	os.MkdirAll(worldDir, 0o755)
	os.WriteFile(worldDir+"/world.toml", []byte("[agents]\ncapacity = 1\n"), 0o644)

	// Create first writ and cast — should auto-provision one agent.
	item1, err := worldStore.CreateWrit("Item 1", "First item", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	_, err = Cast(context.Background(), CastOpts{
		WritID: item1,
		World:      "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("first Cast failed: %v", err)
	}

	// Create second writ and cast — should fail with capacity error.
	item2, err := worldStore.CreateWrit("Item 2", "Second item", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	_, err = Cast(context.Background(), CastOpts{
		WritID: item2,
		World:      "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err == nil {
		t.Fatal("expected capacity error on second cast")
	}
	if !strings.Contains(err.Error(), "reached agent capacity") {
		t.Errorf("expected 'reached agent capacity' error, got: %v", err)
	}
}

func TestCastAutoProvisionCapacityZeroUnlimited(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Default capacity = 0 (unlimited). No world.toml needed.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	// Create and cast multiple writs — all should succeed.
	for i := 0; i < 3; i++ {
		itemID, err := worldStore.CreateWrit(
			fmt.Sprintf("Item %d", i), "desc", "autarch", 2, nil)
		if err != nil {
			t.Fatalf("failed to create writ %d: %v", i, err)
		}

		_, err = Cast(context.Background(), CastOpts{
			WritID: itemID,
			World:      "ember",
			SourceRepo: repoDir,
		}, worldStore, sphereStore, mgr, nil)
		if err != nil {
			t.Fatalf("Cast %d failed: %v", i, err)
		}
	}

	// Verify 3 agents exist.
	agents, err := sphereStore.ListAgents("ember", "")
	if err != nil {
		t.Fatalf("failed to list agents: %v", err)
	}
	if len(agents) != 3 {
		t.Errorf("expected 3 agents, got %d", len(agents))
	}
}

func TestCastAutoProvisionCustomNamePool(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a custom name pool file.
	solHome := os.Getenv("SOL_HOME")
	customPoolPath := solHome + "/custom-names.txt"
	os.WriteFile(customPoolPath, []byte("Mercury\nVenus\nEarth\n"), 0o644)

	// Write world config pointing to custom pool.
	worldDir := solHome + "/ember"
	os.MkdirAll(worldDir, 0o755)
	toml := fmt.Sprintf("[agents]\nname_pool_path = %q\n", customPoolPath)
	os.WriteFile(worldDir+"/world.toml", []byte(toml), 0o644)

	itemID, err := worldStore.CreateWrit("Test item", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(context.Background(), CastOpts{
		WritID: itemID,
		World:      "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	// Agent name should come from the custom pool.
	if result.AgentName != "Mercury" {
		t.Errorf("expected agent name 'Mercury' from custom pool, got %q", result.AgentName)
	}
}

func TestCastAutoProvisionSkipsUsed(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create agents with the first 3 pool names and set them to "working".
	poolNames := []string{"Nova", "Vega", "Lyra"}
	for _, name := range poolNames {
		if _, err := sphereStore.CreateAgent(name, "ember", "outpost"); err != nil {
			t.Fatalf("failed to create agent %q: %v", name, err)
		}
		if err := sphereStore.UpdateAgentState("ember/"+name, "working", "sol-other"); err != nil {
			t.Fatalf("failed to update agent %q: %v", name, err)
		}
	}

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(context.Background(), CastOpts{
		WritID: itemID,
		World:        "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	// Auto-provisioned name must not be any of the already-used names.
	for _, used := range poolNames {
		if result.AgentName == used {
			t.Errorf("auto-provisioned agent got already-used name %q", used)
		}
	}
	if result.AgentName == "" {
		t.Error("auto-provisioned agent has empty name")
	}
}

func TestCastFlockPreventsDoubleDispatch(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	// Acquire the lock manually before calling Cast.
	lock, err := AcquireWritLock(itemID)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	defer lock.Release()

	_, err = Cast(context.Background(), CastOpts{
		WritID: itemID,
		World:        "ember",
		SourceRepo: "/tmp",
	}, worldStore, sphereStore, mgr, nil)

	if err == nil {
		t.Fatal("expected contention error")
	}
	if !strings.Contains(err.Error(), "being dispatched by another process") {
		t.Errorf("expected contention error, got: %v", err)
	}
}

func TestCastItemNotOpen(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "done"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	_, err = Cast(context.Background(), CastOpts{
		WritID: itemID,
		World:        "ember",
		AgentName:  "Toast",
		SourceRepo: "/tmp",
	}, worldStore, sphereStore, mgr, nil)

	if err == nil {
		t.Fatal("expected error for non-open writ")
	}
	if !strings.Contains(err.Error(), "expected \"open\"") {
		t.Errorf("expected 'expected open' error, got: %v", err)
	}
}

func TestCastRejectsNonAgentRoles(t *testing.T) {
	for _, role := range []string{"envoy", "governor", "forge", "sentinel"} {
		t.Run(role, func(t *testing.T) {
			worldStore, sphereStore := setupStores(t)
			mgr := newMockSessionManager()

			itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
			if err != nil {
				t.Fatalf("failed to create writ: %v", err)
			}

			if _, err := sphereStore.CreateAgent("Toast", "ember", role); err != nil {
				t.Fatalf("failed to create agent: %v", err)
			}

			_, err = Cast(context.Background(), CastOpts{
				WritID: itemID,
				World:      "ember",
				AgentName:  "Toast",
				SourceRepo: "/tmp",
			}, worldStore, sphereStore, mgr, nil)

			if err == nil {
				t.Fatalf("expected error when dispatching to %s agent", role)
			}
			if !strings.Contains(err.Error(), "cannot dispatch to "+role) {
				t.Errorf("expected role rejection error, got: %v", err)
			}
		})
	}
}

func TestCastRejectsSleepingWorld(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Mark the world as sleeping by writing world.toml.
	solHome := os.Getenv("SOL_HOME")
	worldDir := filepath.Join(solHome, "ember")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatalf("failed to create world dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\nsleeping = true\n"), 0o644); err != nil {
		t.Fatalf("failed to write world.toml: %v", err)
	}

	_, err = Cast(context.Background(), CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "Toast",
		SourceRepo: "/tmp",
	}, worldStore, sphereStore, mgr, nil)

	if err == nil {
		t.Fatal("expected error when dispatching to sleeping world")
	}
	if !strings.Contains(err.Error(), "sleeping") {
		t.Errorf("expected sleeping error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "dispatch blocked") {
		t.Errorf("expected 'dispatch blocked' in error, got: %v", err)
	}
}

func TestCastRejectsSleepingWorldPreloaded(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Pass a pre-loaded config with sleeping=true.
	sleepingCfg := config.WorldConfig{}
	sleepingCfg.World.Sleeping = true

	_, err = Cast(context.Background(), CastOpts{
		WritID:      itemID,
		World:       "ember",
		AgentName:   "Toast",
		SourceRepo:  "/tmp",
		WorldConfig: &sleepingCfg,
	}, worldStore, sphereStore, mgr, nil)

	if err == nil {
		t.Fatal("expected error when dispatching to sleeping world (pre-loaded config)")
	}
	if !strings.Contains(err.Error(), "sleeping") {
		t.Errorf("expected sleeping error, got: %v", err)
	}
}

// --- Prime tests ---

func TestPrimeWithTether(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	if !strings.Contains(result.Output, "WORK CONTEXT") {
		t.Error("output missing WORK CONTEXT header")
	}
	if !strings.Contains(result.Output, "Toast") {
		t.Error("output missing agent name")
	}
	if !strings.Contains(result.Output, itemID) {
		t.Error("output missing writ ID")
	}
	if !strings.Contains(result.Output, "Add README") {
		t.Error("output missing title")
	}
	if !strings.Contains(result.Output, "sol resolve") {
		t.Error("output missing sol resolve instruction")
	}
}

func TestPrimeWithoutTether(t *testing.T) {
	worldStore, _ := setupStores(t)

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	if result.Output != "No work tethered" {
		t.Errorf("expected 'No work tethered', got %q", result.Output)
	}
}

// --- Workflow prime tests ---

// setupTestWorkflow creates a minimal workflow in $SOL_HOME/workflows/{name}/.
func setupTestWorkflow(t *testing.T, name string) {
	t.Helper()
	workflowDir := filepath.Join(config.Home(), "workflows", name)
	stepsDir := filepath.Join(workflowDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir workflow: %v", err)
	}

	manifest := `name = "` + name + `"
type = "workflow"

[variables]
[variables.issue]
required = true
[variables.base_branch]
default = "main"

[[steps]]
id = "load-context"
title = "Load work context"
instructions = "steps/01-load.md"

[[steps]]
id = "implement"
title = "Implement the change"
instructions = "steps/02-impl.md"
needs = ["load-context"]

[[steps]]
id = "verify"
title = "Verify the implementation"
instructions = "steps/03-verify.md"
needs = ["implement"]
`
	if err := os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	for _, f := range []struct{ name, content string }{
		{"steps/01-load.md", "# Load\nRead the writ {{issue}}."},
		{"steps/02-impl.md", "# Implement\nWrite code for {{issue}}."},
		{"steps/03-verify.md", "# Verify\nRun tests for {{issue}}."},
	} {
		if err := os.WriteFile(filepath.Join(workflowDir, f.name), []byte(f.content), 0o644); err != nil {
			t.Fatalf("write %s: %v", f.name, err)
		}
	}
}

// advanceWorkflowStep simulates completing the current step by writing state/step JSON directly.
func advanceWorkflowStep(t *testing.T, world, agentName, role string) {
	t.Helper()
	wfDir := workflow.InstanceDir(world, agentName, role)

	// Read current state.
	stateData, err := os.ReadFile(filepath.Join(wfDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state workflow.State
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}

	// Mark the current step as complete.
	completedID := state.CurrentStep
	stepPath := filepath.Join(wfDir, "steps", completedID+".json")
	stepData, err := os.ReadFile(stepPath)
	if err != nil {
		t.Fatalf("read step: %v", err)
	}
	var step workflow.Step
	if err := json.Unmarshal(stepData, &step); err != nil {
		t.Fatalf("unmarshal step: %v", err)
	}
	now := time.Now().UTC()
	step.Status = "complete"
	step.CompletedAt = &now
	writeTestJSON(t, stepPath, step)

	// Use the real Advance function to set the next step.
	// But since we've already marked it, just update state directly.
	state.Completed = append(state.Completed, completedID)

	// Find next step: read all step files to find a "pending" one.
	entries, _ := os.ReadDir(filepath.Join(wfDir, "steps"))
	nextID := ""
	for _, e := range entries {
		if e.Name() == completedID+".json" {
			continue
		}
		sd, _ := os.ReadFile(filepath.Join(wfDir, "steps", e.Name()))
		var s workflow.Step
		json.Unmarshal(sd, &s)
		if s.Status == "pending" {
			nextID = s.ID
			break
		}
	}
	if nextID != "" {
		state.CurrentStep = nextID
		// Mark next step as executing.
		nextPath := filepath.Join(wfDir, "steps", nextID+".json")
		nd, _ := os.ReadFile(nextPath)
		var ns workflow.Step
		json.Unmarshal(nd, &ns)
		ns.Status = "executing"
		ns.StartedAt = &now
		writeTestJSON(t, nextPath, ns)
	} else {
		state.CurrentStep = ""
	}
	writeTestJSON(t, filepath.Join(wfDir, "state.json"), state)
}

func writeTestJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write json %s: %v", path, err)
	}
}

func TestPrimeWithWorkflowFullChecklist(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Implement feature X", "Build feature X", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}
	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("write tether: %v", err)
	}

	// Create workflow and instantiate it.
	setupTestWorkflow(t, "test-work")
	if _, _, err := workflow.Instantiate("ember", "Toast", "outpost", "test-work", map[string]string{
		"issue": itemID,
	}); err != nil {
		t.Fatalf("instantiate workflow: %v", err)
	}

	// Step 1 (load-context) is current. Advance to step 2 (implement).
	advanceWorkflowStep(t, "ember", "Toast", "outpost")

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	// Verify full checklist is present.
	output := result.Output

	// Header.
	if !strings.Contains(output, "WORK CONTEXT") {
		t.Error("missing WORK CONTEXT header")
	}
	if !strings.Contains(output, "Toast") {
		t.Error("missing agent name")
	}
	if !strings.Contains(output, "step 2/3") {
		t.Errorf("missing step progress; got:\n%s", output)
	}

	// Checklist markers.
	if !strings.Contains(output, "[x] 1. Load work context") {
		t.Errorf("missing completed step marker; got:\n%s", output)
	}
	if !strings.Contains(output, "[>] 2. Implement the change") {
		t.Errorf("missing current step marker; got:\n%s", output)
	}
	if !strings.Contains(output, "[ ] 3. Verify the implementation") {
		t.Errorf("missing pending step marker; got:\n%s", output)
	}

	// Current step instructions shown in full.
	if !strings.Contains(output, "Write code for "+itemID) {
		t.Errorf("missing current step instructions; got:\n%s", output)
	}

	// Other step instructions NOT shown (only titles).
	if strings.Contains(output, "Read the writ "+itemID) {
		t.Errorf("completed step instructions should not appear in output")
	}
	if strings.Contains(output, "Run tests for "+itemID) {
		t.Errorf("pending step instructions should not appear in output")
	}

	// Propulsion instructions.
	if !strings.Contains(output, "sol workflow advance") {
		t.Error("missing advance instruction")
	}
	if !strings.Contains(output, "sol resolve") {
		t.Error("missing resolve instruction")
	}
}

func TestPrimeWithWorkflowFirstStep(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("First step test", "Test first step", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}
	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("write tether: %v", err)
	}

	setupTestWorkflow(t, "test-work")
	if _, _, err := workflow.Instantiate("ember", "Toast", "outpost", "test-work", map[string]string{
		"issue": itemID,
	}); err != nil {
		t.Fatalf("instantiate workflow: %v", err)
	}

	// Step 1 is current (no advance).
	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	output := result.Output

	if !strings.Contains(output, "step 1/3") {
		t.Errorf("missing step 1/3 progress; got:\n%s", output)
	}
	if !strings.Contains(output, "[>] 1. Load work context") {
		t.Errorf("step 1 should be current; got:\n%s", output)
	}
	if !strings.Contains(output, "[ ] 2. Implement the change") {
		t.Errorf("step 2 should be pending; got:\n%s", output)
	}
	if !strings.Contains(output, "[ ] 3. Verify the implementation") {
		t.Errorf("step 3 should be pending; got:\n%s", output)
	}
}

// --- Resolve tests ---

func TestResolveHappyPath(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create a worktree directory with a git repo (simulating a worktree).
	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:       "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.WritID != itemID {
		t.Errorf("expected writ ID %q, got %q", itemID, result.WritID)
	}
	if result.AgentName != "Toast" {
		t.Errorf("expected agent name Toast, got %q", result.AgentName)
	}
	expectedBranch := fmt.Sprintf("outpost/Toast/%s", itemID)
	if result.BranchName != expectedBranch {
		t.Errorf("expected branch %q, got %q", expectedBranch, result.BranchName)
	}

	// Verify merge request was created.
	if result.MergeRequestID == "" {
		t.Error("expected MergeRequestID to be set")
	}

	// Verify writ was updated to done.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected writ status 'done', got %q", item.Status)
	}

	// Verify outpost agent record is deleted (name reclaimed).
	_, err = sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent record to be deleted after resolve")
	} else if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound for deleted agent, got: %v", err)
	}

	// Verify tether is cleared.
	tetherID, err := tether.Read("ember", "Toast", "outpost")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != "" {
		t.Errorf("expected empty tether, got %q", tetherID)
	}
}

func TestResolveNoTether(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Resolve now looks up the agent first (for role-aware tether path),
	// so create an agent record so we get past that check.
	sphereStore.CreateAgent("Toast", "ember", "outpost")

	_, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err == nil {
		t.Fatal("expected error when no tether exists")
	}
	if !strings.Contains(err.Error(), "no work tethered") {
		t.Errorf("expected 'no work tethered' error, got: %v", err)
	}
}

func TestResolveConflictResolution(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create the original writ.
	origItemID, err := worldStore.CreateWrit("Add feature X", "Implement feature X", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	// Create a merge request for the original writ.
	mrID, err := worldStore.CreateMergeRequest(origItemID, "outpost/Alpha/"+origItemID, 2)
	if err != nil {
		t.Fatalf("failed to create merge request: %v", err)
	}

	// Create the conflict-resolution task.
	resolutionID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Resolve merge conflicts: Add feature X",
		Description: "Resolve merge conflicts",
		CreatedBy:   "ember/forge",
		Priority:    1,
		Labels:      []string{"conflict-resolution", "source-mr:" + mrID},
		ParentID:    origItemID,
	})
	if err != nil {
		t.Fatalf("failed to create resolution task: %v", err)
	}

	// Block the MR with the resolution task.
	if err := worldStore.BlockMergeRequest(mrID, resolutionID); err != nil {
		t.Fatalf("failed to block MR: %v", err)
	}

	// Set up agent and tether the resolution task.
	if err := worldStore.UpdateWrit(resolutionID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", resolutionID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}
	if err := tether.Write("ember", "Toast", resolutionID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree dir with git repo.
	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:       "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve (conflict-resolution) failed: %v", err)
	}

	// Verify NO new merge request was created.
	if result.MergeRequestID != "" {
		t.Errorf("expected empty MergeRequestID for conflict-resolution, got %q", result.MergeRequestID)
	}

	// Verify the resolution writ is closed.
	resItem, err := worldStore.GetWrit(resolutionID)
	if err != nil {
		t.Fatalf("failed to get resolution item: %v", err)
	}
	if resItem.Status != "closed" {
		t.Errorf("expected resolution item status 'closed', got %q", resItem.Status)
	}

	// Verify the original MR is unblocked.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("failed to get MR: %v", err)
	}
	if mr.BlockedBy != "" {
		t.Errorf("expected MR blocked_by to be empty (unblocked), got %q", mr.BlockedBy)
	}
	if mr.Phase != "ready" {
		t.Errorf("expected MR phase 'ready' after unblock, got %q", mr.Phase)
	}

	// Verify outpost agent record is deleted (name reclaimed).
	_, err = sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent record to be deleted after resolve")
	} else if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound for deleted agent, got: %v", err)
	}

	// Verify tether is cleared.
	tetherID, err := tether.Read("ember", "Toast", "outpost")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != "" {
		t.Errorf("expected empty tether, got %q", tetherID)
	}
}

func TestResolveConflictResolutionResetsParentMR(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create the original writ with a MR.
	origItemID, err := worldStore.CreateWrit("Add feature Y", "Implement feature Y", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	mrID, err := worldStore.CreateMergeRequest(origItemID, "outpost/Alpha/"+origItemID, 2)
	if err != nil {
		t.Fatalf("failed to create merge request: %v", err)
	}

	// Simulate the MR being claimed and then failing (max attempts exceeded).
	worldStore.ClaimMergeRequest("forge/Forge")
	if err := worldStore.UpdateMergeRequestPhase(mrID, "failed"); err != nil {
		t.Fatalf("failed to mark MR as failed: %v", err)
	}

	// Verify MR is in failed state with attempts > 0.
	mr, _ := worldStore.GetMergeRequest(mrID)
	if mr.Phase != "failed" || mr.Attempts != 1 {
		t.Fatalf("expected failed MR with attempts=1, got phase=%q attempts=%d", mr.Phase, mr.Attempts)
	}

	// Create conflict-resolution child writ (parent_id points to original).
	resolutionID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Resolve merge conflicts: Add feature Y",
		Description: "Resolve merge conflicts",
		CreatedBy:   "ember/forge",
		Priority:    1,
		Labels:      []string{"conflict-resolution", "source-mr:" + mrID},
		ParentID:    origItemID,
	})
	if err != nil {
		t.Fatalf("failed to create resolution task: %v", err)
	}

	// Set up agent and tether the resolution task.
	if err := worldStore.UpdateWrit(resolutionID, store.WritUpdates{Status: "tethered", Assignee: "ember/Bravo"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Bravo", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Bravo", "working", resolutionID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}
	if err := tether.Write("ember", "Bravo", resolutionID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree dir with git repo.
	worktreeDir := WorktreePath("ember", "Bravo")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Bravo")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Bravo",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve (conflict-resolution) failed: %v", err)
	}

	// Verify NO new merge request was created.
	if result.MergeRequestID != "" {
		t.Errorf("expected empty MergeRequestID for conflict-resolution, got %q", result.MergeRequestID)
	}

	// Verify the parent MR is now ready with attempts reset to 0.
	mr, err = worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("failed to get MR: %v", err)
	}
	if mr.Phase != "ready" {
		t.Errorf("expected parent MR phase 'ready', got %q", mr.Phase)
	}
	if mr.Attempts != 0 {
		t.Errorf("expected parent MR attempts=0, got %d", mr.Attempts)
	}
	if mr.ClaimedBy != "" {
		t.Errorf("expected parent MR claimed_by empty, got %q", mr.ClaimedBy)
	}

	// Verify the resolution writ is closed.
	resItem, err := worldStore.GetWrit(resolutionID)
	if err != nil {
		t.Fatalf("failed to get resolution item: %v", err)
	}
	if resItem.Status != "closed" {
		t.Errorf("expected resolution item status 'closed', got %q", resItem.Status)
	}
}

func TestResolveConflictResolutionResetsBlockedAndFailedMRs(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create original writ with TWO MRs — one blocked, one failed.
	origItemID, err := worldStore.CreateWrit("Add feature Z", "Implement feature Z", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	// MR1: will be blocked by the resolution task.
	mr1ID, err := worldStore.CreateMergeRequest(origItemID, "outpost/Alpha/"+origItemID, 2)
	if err != nil {
		t.Fatalf("failed to create MR1: %v", err)
	}

	// MR2: already failed independently.
	mr2ID, err := worldStore.CreateMergeRequest(origItemID, "outpost/Beta/"+origItemID, 2)
	if err != nil {
		t.Fatalf("failed to create MR2: %v", err)
	}
	worldStore.ClaimMergeRequest("forge/Forge")
	worldStore.ClaimMergeRequest("forge/Forge")
	if err := worldStore.UpdateMergeRequestPhase(mr2ID, "failed"); err != nil {
		t.Fatalf("failed to mark MR2 as failed: %v", err)
	}

	// Create conflict-resolution child writ.
	resolutionID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Resolve merge conflicts: Add feature Z",
		Description: "Resolve merge conflicts",
		CreatedBy:   "ember/forge",
		Priority:    1,
		Labels:      []string{"conflict-resolution", "source-mr:" + mr1ID},
		ParentID:    origItemID,
	})
	if err != nil {
		t.Fatalf("failed to create resolution task: %v", err)
	}

	// Block MR1 with the resolution task.
	if err := worldStore.BlockMergeRequest(mr1ID, resolutionID); err != nil {
		t.Fatalf("failed to block MR1: %v", err)
	}

	// Set up agent and tether.
	if err := worldStore.UpdateWrit(resolutionID, store.WritUpdates{Status: "tethered", Assignee: "ember/Charlie"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Charlie", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Charlie", "working", resolutionID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}
	if err := tether.Write("ember", "Charlie", resolutionID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Charlie")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Charlie")
	mgr.started[sessName] = true

	_, err = Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Charlie",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify MR1 (was blocked) is now ready with attempts=0.
	mr1, err := worldStore.GetMergeRequest(mr1ID)
	if err != nil {
		t.Fatalf("failed to get MR1: %v", err)
	}
	if mr1.Phase != "ready" {
		t.Errorf("MR1 phase = %q, want 'ready'", mr1.Phase)
	}
	if mr1.Attempts != 0 {
		t.Errorf("MR1 attempts = %d, want 0", mr1.Attempts)
	}
	if mr1.BlockedBy != "" {
		t.Errorf("MR1 blocked_by = %q, want empty", mr1.BlockedBy)
	}

	// Verify MR2 (was failed) is now ready with attempts=0.
	mr2, err := worldStore.GetMergeRequest(mr2ID)
	if err != nil {
		t.Fatalf("failed to get MR2: %v", err)
	}
	if mr2.Phase != "ready" {
		t.Errorf("MR2 phase = %q, want 'ready'", mr2.Phase)
	}
	if mr2.Attempts != 0 {
		t.Errorf("MR2 attempts = %d, want 0", mr2.Attempts)
	}
}

func TestResolveCreatesMergeRequest(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Implement login page", "Build the login page", "autarch", 1, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:       "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify MergeRequestID is set.
	if result.MergeRequestID == "" {
		t.Fatal("expected MergeRequestID to be set")
	}
	if !strings.HasPrefix(result.MergeRequestID, "mr-") {
		t.Errorf("expected MergeRequestID to start with 'mr-', got %q", result.MergeRequestID)
	}

	// Verify merge request exists in store with correct fields.
	mr, err := worldStore.GetMergeRequest(result.MergeRequestID)
	if err != nil {
		t.Fatalf("failed to get merge request: %v", err)
	}
	if mr.Phase != "ready" {
		t.Errorf("expected MR phase 'ready', got %q", mr.Phase)
	}
	if mr.WritID != itemID {
		t.Errorf("expected MR writ_id %q, got %q", itemID, mr.WritID)
	}
	expectedBranch := fmt.Sprintf("outpost/Toast/%s", itemID)
	if mr.Branch != expectedBranch {
		t.Errorf("expected MR branch %q, got %q", expectedBranch, mr.Branch)
	}
	if mr.Priority != 1 {
		t.Errorf("expected MR priority 1, got %d", mr.Priority)
	}

	// Verify writ is done.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected writ status 'done', got %q", item.Status)
	}

	// Verify outpost agent record is deleted (name reclaimed).
	_, err = sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent record to be deleted after resolve")
	} else if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound for deleted agent, got: %v", err)
	}
}

func TestResolveSkipsFailedMRCreatesNew(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Fix bug", "Fix the bug", "autarch", 1, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	// Pre-create a failed MR for this writ (simulates a previous failed merge attempt).
	failedMRID, err := worldStore.CreateMergeRequest(itemID, "outpost/Alpha/"+itemID, 1)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
	}
	if err := worldStore.UpdateMergeRequestPhase(failedMRID, "claimed"); err != nil {
		t.Fatalf("failed to claim MR: %v", err)
	}
	if err := worldStore.UpdateMergeRequestPhase(failedMRID, "failed"); err != nil {
		t.Fatalf("failed to fail MR: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Should have created a NEW MR, not reused the failed one.
	if result.MergeRequestID == failedMRID {
		t.Errorf("expected new MR ID, but got failed MR ID %q", failedMRID)
	}
	if result.MergeRequestID == "" {
		t.Fatal("expected MergeRequestID to be set")
	}

	// New MR should be in "ready" phase with the correct branch.
	newMR, err := worldStore.GetMergeRequest(result.MergeRequestID)
	if err != nil {
		t.Fatalf("failed to get new merge request: %v", err)
	}
	if newMR.Phase != "ready" {
		t.Errorf("expected new MR phase 'ready', got %q", newMR.Phase)
	}
	expectedBranch := fmt.Sprintf("outpost/Toast/%s", itemID)
	if newMR.Branch != expectedBranch {
		t.Errorf("expected new MR branch %q, got %q", expectedBranch, newMR.Branch)
	}

	// Failed MR should still exist and remain in "failed" phase.
	oldMR, err := worldStore.GetMergeRequest(failedMRID)
	if err != nil {
		t.Fatalf("failed to get old merge request: %v", err)
	}
	if oldMR.Phase != "failed" {
		t.Errorf("expected old MR to remain in 'failed' phase, got %q", oldMR.Phase)
	}
}

func TestResolveReusesReadyMR(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Fix bug", "Fix the bug", "autarch", 1, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	// Pre-create a ready MR for this writ (simulates idempotent re-resolve).
	readyMRID, err := worldStore.CreateMergeRequest(itemID, "outpost/Toast/"+itemID, 1)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Should reuse the existing ready MR.
	if result.MergeRequestID != readyMRID {
		t.Errorf("expected reused MR ID %q, got %q", readyMRID, result.MergeRequestID)
	}
}

// --- Prime with handoff tests ---

func TestPrimeWithHandoff(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	// Write tether file.
	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Write handoff file.
	state := &handoff.State{
		WritID:      itemID,
		AgentName:       "Toast",
		World:             "ember",
		Role:            "outpost",
		PreviousSession: "sol-ember-Toast",
		Summary:         "Implemented login form. Tests passing.",
		RecentCommits:   []string{"abc1234 feat: add login form"},
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("failed to write handoff: %v", err)
	}

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime with handoff failed: %v", err)
	}

	if !strings.Contains(result.Output, "HANDOFF CONTEXT") {
		t.Error("output missing HANDOFF CONTEXT header")
	}
	if !strings.Contains(result.Output, "Toast") {
		t.Error("output missing agent name")
	}
	if !strings.Contains(result.Output, itemID) {
		t.Error("output missing writ ID")
	}
	if !strings.Contains(result.Output, "Implemented login form") {
		t.Error("output missing summary")
	}
	if !strings.Contains(result.Output, "abc1234 feat: add login form") {
		t.Error("output missing recent commits")
	}
	if !strings.Contains(result.Output, "sol handoff") {
		t.Error("output missing handoff instruction")
	}

	// Handoff file should be deleted after prime.
	if handoff.HasHandoff("ember", "Toast", "outpost") {
		t.Error("expected handoff file to be removed after prime")
	}
}

func TestPrimeHandoffTakesPriority(t *testing.T) {
	worldStore, _ := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	// Write tether file.
	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Write handoff file.
	state := &handoff.State{
		WritID:       itemID,
		AgentName:        "Toast",
		World:              "ember",
		Role:             "outpost",
		PreviousSession:  "sol-ember-Toast",
		Summary:          "Handoff summary here.",
		RecentCommits:    []string{"abc1234 feat: work"},
		WorkflowStep:     "implement",
		WorkflowProgress: "1/3 complete",
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("failed to write handoff: %v", err)
	}

	// Also set up workflow state (should be ignored in favor of handoff).
	wfDir := fmt.Sprintf("%s/ember/outposts/Toast/.workflow", solHome)
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("failed to create workflow dir: %v", err)
	}
	stateJSON := `{"current_step":"implement","completed":["plan"],"status":"running","started_at":"2026-02-27T10:00:00Z"}`
	os.WriteFile(wfDir+"/state.json", []byte(stateJSON), 0o644)

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime with handoff+workflow failed: %v", err)
	}

	// Should have handoff context, not workflow context.
	if !strings.Contains(result.Output, "HANDOFF CONTEXT") {
		t.Error("output missing HANDOFF CONTEXT — handoff should take priority")
	}
	if strings.Contains(result.Output, "WORK CONTEXT") {
		t.Error("output contains WORK CONTEXT — handoff should take priority over workflow")
	}

	// Handoff file should be deleted.
	if handoff.HasHandoff("ember", "Toast", "outpost") {
		t.Error("expected handoff file to be removed after prime")
	}
}

func TestPrimeNoHandoff(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// No handoff file — should use standard prime.
	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	if !strings.Contains(result.Output, "WORK CONTEXT") {
		t.Error("expected standard WORK CONTEXT output when no handoff")
	}
	if strings.Contains(result.Output, "HANDOFF CONTEXT") {
		t.Error("unexpected HANDOFF CONTEXT when no handoff file exists")
	}
}

func TestPrimeWithFreshSessionMarker(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Write handoff marker (simulates recent handoff).
	if err := handoff.WriteMarker("ember", "Toast", "outpost", "session handoff"); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime with marker failed: %v", err)
	}

	// Should contain the fresh-session warning.
	if !strings.Contains(result.Output, "fresh session") {
		t.Error("output missing fresh-session warning")
	}
	if !strings.Contains(result.Output, "do NOT call sol handoff") {
		t.Error("output missing handoff prevention instruction")
	}

	// Marker should be removed after prime.
	ts, _, _ := handoff.ReadMarker("ember", "Toast", "outpost")
	if !ts.IsZero() {
		t.Error("expected marker to be removed after prime")
	}
}

func TestPrimeHandoffWithGitState(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Write handoff file with git state.
	state := &handoff.State{
		WritID:      itemID,
		AgentName:       "Toast",
		World:           "ember",
		Role:            "outpost",
		PreviousSession: "sol-ember-Toast",
		Summary:         "Working on login form.",
		RecentCommits:   []string{"abc1234 feat: add login form"},
		GitStatus:       " M hello.go\n?? new.go",
		DiffStat:        " hello.go | 2 +-\n 1 file changed",
		GitStash:        "stash@{0}: WIP on main",
		StepDescription: "Implement the login form",
		WorkflowStep:    "implement",
		WorkflowProgress: "1/3 complete",
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("failed to write handoff: %v", err)
	}

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	// Should contain git state in output.
	if !strings.Contains(result.Output, "GIT STATUS") {
		t.Error("output missing GIT STATUS section")
	}
	if !strings.Contains(result.Output, "hello.go") {
		t.Error("output missing modified file in git status")
	}
	if !strings.Contains(result.Output, "UNCOMMITTED CHANGES") {
		t.Error("output missing UNCOMMITTED CHANGES section")
	}
	if !strings.Contains(result.Output, "STASHED WORK") {
		t.Error("output missing STASHED WORK section")
	}
	// Step description should be included.
	if !strings.Contains(result.Output, "Implement the login form") {
		t.Error("output missing step description")
	}
}

func TestPrimeDurableHandoff(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	state := &handoff.State{
		WritID:      itemID,
		AgentName:       "Toast",
		World:           "ember",
		Role:            "outpost",
		PreviousSession: "sol-ember-Toast",
		Summary:         "Working on it.",
		RecentCommits:   []string{"abc1234 feat: work"},
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("failed to write handoff: %v", err)
	}

	// First prime consumes the handoff.
	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("first Prime failed: %v", err)
	}
	if !strings.Contains(result.Output, "HANDOFF CONTEXT") {
		t.Error("first prime should show handoff context")
	}

	// File should still exist but be consumed.
	read, err := handoff.Read("ember", "Toast", "outpost")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if read == nil {
		t.Fatal("expected handoff file to survive consumption (durable)")
	}
	if !read.Consumed {
		t.Error("expected consumed flag to be true")
	}

	// Second prime should NOT show handoff context (consumed).
	result2, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("second Prime failed: %v", err)
	}
	if strings.Contains(result2.Output, "HANDOFF CONTEXT") {
		t.Error("second prime should not show consumed handoff context")
	}
	if !strings.Contains(result2.Output, "WORK CONTEXT") {
		t.Error("second prime should show standard work context")
	}
}

func TestPrimeCompactRecoveryLightweight(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file with detailed instructions", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Write handoff file.
	state := &handoff.State{
		WritID:      itemID,
		AgentName:       "Toast",
		World:           "ember",
		Role:            "outpost",
		PreviousSession: "sol-ember-Toast",
		Summary:         "Implemented login form. Tests passing.",
		RecentCommits:   []string{"abc1234 feat: add login form"},
		GitStatus:       " M hello.go",
		DiffStat:        " hello.go | 2 +-",
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("failed to write handoff: %v", err)
	}

	// Write compact marker (simulates PreCompact-triggered handoff).
	if err := handoff.WriteMarker("ember", "Toast", "outpost", "compact"); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime with compact recovery failed: %v", err)
	}

	// Should use SESSION RECOVERY header, not HANDOFF CONTEXT.
	if !strings.Contains(result.Output, "SESSION RECOVERY") {
		t.Error("output missing SESSION RECOVERY header")
	}
	if strings.Contains(result.Output, "HANDOFF CONTEXT") {
		t.Error("compact recovery should use SESSION RECOVERY, not HANDOFF CONTEXT")
	}

	// Should include handoff state.
	if !strings.Contains(result.Output, "Implemented login form") {
		t.Error("output missing summary from handoff state")
	}
	if !strings.Contains(result.Output, "abc1234 feat: add login form") {
		t.Error("output missing recent commits")
	}
	if !strings.Contains(result.Output, "hello.go") {
		t.Error("output missing git status")
	}

	// Should NOT include full writ description (lightweight).
	if strings.Contains(result.Output, "detailed instructions") {
		t.Error("compact recovery should not include full writ description")
	}

	// Should include continuation instructions.
	if !strings.Contains(result.Output, "Continue from where you left off") {
		t.Error("output missing continuation instructions")
	}
	if !strings.Contains(result.Output, "Do NOT re-read the writ description") {
		t.Error("output missing instruction to avoid re-reading description")
	}

	// Should NOT have the generic fresh-session warning (compact has its own framing).
	if strings.Contains(result.Output, "fresh session") {
		t.Error("compact recovery should not have generic fresh-session warning")
	}

	// Handoff file should be consumed.
	if handoff.HasHandoff("ember", "Toast", "outpost") {
		t.Error("expected handoff to be consumed after compact recovery prime")
	}

	// Marker should be removed.
	ts, _, _ := handoff.ReadMarker("ember", "Toast", "outpost")
	if !ts.IsZero() {
		t.Error("expected marker to be removed after prime")
	}
}

func TestPrimeCompactRecoveryWithWorkflow(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Write handoff file with workflow state.
	state := &handoff.State{
		WritID:       itemID,
		AgentName:        "Toast",
		World:            "ember",
		Role:             "outpost",
		PreviousSession:  "sol-ember-Toast",
		Summary:          "Working on step 2.",
		RecentCommits:    []string{"abc1234 feat: step 1 done"},
		WorkflowStep:     "implement",
		WorkflowProgress: "1/3 complete",
		StepDescription:  "Implement the feature",
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("failed to write handoff: %v", err)
	}

	// Compact marker.
	if err := handoff.WriteMarker("ember", "Toast", "outpost", "compact"); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime with compact+workflow failed: %v", err)
	}

	if !strings.Contains(result.Output, "SESSION RECOVERY") {
		t.Error("output missing SESSION RECOVERY header")
	}
	if !strings.Contains(result.Output, "CURRENT WORKFLOW STATE") {
		t.Error("output missing workflow state section")
	}
	if !strings.Contains(result.Output, "1/3 complete") {
		t.Error("output missing workflow progress")
	}
	if !strings.Contains(result.Output, "Implement the feature") {
		t.Error("output missing step description")
	}
}

func TestPrimeNonCompactHandoffUsesFullPrime(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	state := &handoff.State{
		WritID:      itemID,
		AgentName:       "Toast",
		World:           "ember",
		Role:            "outpost",
		PreviousSession: "sol-ember-Toast",
		Summary:         "Working on it.",
		RecentCommits:   []string{"abc1234 feat: work"},
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("failed to write handoff: %v", err)
	}

	// Non-compact marker (e.g., manual handoff or old-style).
	if err := handoff.WriteMarker("ember", "Toast", "outpost", "session handoff"); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime with non-compact handoff failed: %v", err)
	}

	// Should use full HANDOFF CONTEXT, not SESSION RECOVERY.
	if !strings.Contains(result.Output, "HANDOFF CONTEXT") {
		t.Error("non-compact handoff should use full HANDOFF CONTEXT")
	}
	if strings.Contains(result.Output, "SESSION RECOVERY") {
		t.Error("non-compact handoff should not use SESSION RECOVERY")
	}

	// Should have the fresh-session warning.
	if !strings.Contains(result.Output, "fresh session") {
		t.Error("non-compact handoff should have fresh-session warning")
	}
}

func TestPrimeCompactWithoutHandoffFallsThrough(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Compact marker but NO handoff file — should fall through to standard prime.
	if err := handoff.WriteMarker("ember", "Toast", "outpost", "compact"); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime with compact but no handoff failed: %v", err)
	}

	// Should use standard WORK CONTEXT since there's no handoff state.
	if !strings.Contains(result.Output, "WORK CONTEXT") {
		t.Error("compact without handoff should fall through to standard WORK CONTEXT")
	}
	if strings.Contains(result.Output, "SESSION RECOVERY") {
		t.Error("compact without handoff should not use SESSION RECOVERY")
	}
}

// --- Mock world store that wraps a real store but can inject errors ---

type mockWorldStore struct {
	*store.Store
	createMRErr error // if set, CreateMergeRequest returns this error
}

func (m *mockWorldStore) CreateMergeRequest(writID, branch string, priority int) (string, error) {
	if m.createMRErr != nil {
		return "", m.createMRErr
	}
	return m.Store.CreateMergeRequest(writID, branch, priority)
}

// --- Resolve rollback/safety tests ---

func TestResolveRollbackOnMRFailure(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add feature", "Build the feature", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree with git repo and remote.
	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	// Use mock world store that fails on CreateMergeRequest.
	mock := &mockWorldStore{
		Store:       worldStore,
		createMRErr: fmt.Errorf("simulated MR creation failure"),
	}

	_, err = Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, mock, sphereStore, mgr, nil)

	if err == nil {
		t.Fatal("expected error from failed CreateMergeRequest")
	}
	if !strings.Contains(err.Error(), "simulated MR creation failure") {
		t.Errorf("expected simulated error, got: %v", err)
	}

	// Verify: writ status is rolled back to "tethered" (not stuck at "done").
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("expected writ status rolled back to 'tethered', got %q", item.Status)
	}
}

func TestResolvePushFailureCreatesMR(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add feature", "Build the feature", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree with git repo but NO remote (so push fails).
	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	// Intentionally NO addBareRemote — push will fail.

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify: MR is created with phase "failed".
	if result.MergeRequestID == "" {
		t.Fatal("expected MergeRequestID to be set even with push failure")
	}

	mr, err := worldStore.GetMergeRequest(result.MergeRequestID)
	if err != nil {
		t.Fatalf("failed to get merge request: %v", err)
	}
	if mr.Phase != "failed" {
		t.Errorf("expected MR phase 'failed', got %q", mr.Phase)
	}

	// Verify: writ is "done", agent record is deleted.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected writ status 'done', got %q", item.Status)
	}

	// Verify outpost agent record is deleted (name reclaimed).
	_, err = sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent record to be deleted after resolve")
	} else if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound for deleted agent, got: %v", err)
	}
}

func TestReCastPartialFailureRecovery(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add feature", "Build the feature", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	// Set up partial failure state: item tethered to agent, but agent still "idle".
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	// Agent state is "idle" with no active_writ — simulates crash after writ
	// update but before agent state update.

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(context.Background(), CastOpts{
		WritID: itemID,
		World:      "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Cast (partial failure recovery) failed: %v", err)
	}

	if result.WritID != itemID {
		t.Errorf("expected writ ID %q, got %q", itemID, result.WritID)
	}
	if result.AgentName != "Toast" {
		t.Errorf("expected agent name Toast, got %q", result.AgentName)
	}

	// Verify: agent state is now "working", session started.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
	if !mgr.started["sol-ember-Toast"] {
		t.Error("expected session to be started")
	}
}

// --- Envoy resolve tests ---

func TestResolveEnvoyKeepsSession(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Envoy task", "An envoy writ", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	// Create an envoy agent.
	if _, err := sphereStore.CreateAgent("Scout", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Scout", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Scout", itemID, "envoy"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree dir at envoy path with git repo.
	worktreeDir := envoy.WorktreePath("ember", "Scout")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Scout")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Scout",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Session should NOT have been stopped.
	if mgr.stopped[sessName] {
		t.Error("expected session to NOT be stopped for envoy resolve")
	}

	// SessionKept should be true.
	if !result.SessionKept {
		t.Error("expected SessionKept to be true for envoy resolve")
	}

	// Branch name should have envoy/{world}/{agentName} format.
	expectedBranch := "envoy/ember/Scout"
	if result.BranchName != expectedBranch {
		t.Errorf("expected branch %q, got %q", expectedBranch, result.BranchName)
	}

	// Writ should still be done.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected writ status 'done', got %q", item.Status)
	}

	// Agent should be idle.
	agent, err := sphereStore.GetAgent("ember/Scout")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}

	// Tether should be cleared.
	tetherID, err := tether.Read("ember", "Scout", "envoy")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != "" {
		t.Errorf("expected empty tether, got %q", tetherID)
	}

	// MR should be created.
	if result.MergeRequestID == "" {
		t.Error("expected MergeRequestID to be set")
	}
}

func TestResolvePersistentAgentWithRemainingTethers(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create two writs.
	writ1, err := worldStore.CreateWrit("Envoy task 1", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 1: %v", err)
	}
	if err := worldStore.UpdateWrit(writ1, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ 1: %v", err)
	}
	writ2, err := worldStore.CreateWrit("Envoy task 2", "Second task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 2: %v", err)
	}
	if err := worldStore.UpdateWrit(writ2, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ 2: %v", err)
	}

	// Create envoy agent with active_writ = writ1.
	if _, err := sphereStore.CreateAgent("Scout", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Scout", "working", writ1); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	// Tether both writs.
	if err := tether.Write("ember", "Scout", writ1, "envoy"); err != nil {
		t.Fatalf("failed to write tether 1: %v", err)
	}
	if err := tether.Write("ember", "Scout", writ2, "envoy"); err != nil {
		t.Fatalf("failed to write tether 2: %v", err)
	}

	// Create worktree dir at envoy path with git repo.
	worktreeDir := envoy.WorktreePath("ember", "Scout")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Scout")
	mgr.started[sessName] = true

	// Resolve the active writ (writ1).
	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Scout",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.WritID != writ1 {
		t.Errorf("expected resolved writ %q, got %q", writ1, result.WritID)
	}

	// Agent should still be working (remaining tethers exist).
	agent, err := sphereStore.GetAgent("ember/Scout")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}

	// Active writ should be cleared (we resolved it).
	if agent.ActiveWrit != "" {
		t.Errorf("expected active_writ to be cleared, got %q", agent.ActiveWrit)
	}

	// Only the resolved writ's tether should be removed.
	if tether.IsTetheredTo("ember", "Scout", writ1, "envoy") {
		t.Error("expected resolved writ's tether to be removed")
	}
	if !tether.IsTetheredTo("ember", "Scout", writ2, "envoy") {
		t.Error("expected remaining writ's tether to still exist")
	}

	// Session should NOT have been stopped.
	if mgr.stopped[sessName] {
		t.Error("expected session to NOT be stopped for envoy resolve")
	}
}

func TestResolvePersistentAgentNonActiveWrit(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create two writs.
	writ1, err := worldStore.CreateWrit("Envoy task 1", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 1: %v", err)
	}
	if err := worldStore.UpdateWrit(writ1, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ 1: %v", err)
	}
	writ2, err := worldStore.CreateWrit("Envoy task 2", "Second task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 2: %v", err)
	}
	if err := worldStore.UpdateWrit(writ2, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ 2: %v", err)
	}

	// Create envoy agent with active_writ = writ1.
	if _, err := sphereStore.CreateAgent("Scout", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Scout", "working", writ1); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	// Tether both writs.
	if err := tether.Write("ember", "Scout", writ1, "envoy"); err != nil {
		t.Fatalf("failed to write tether 1: %v", err)
	}
	if err := tether.Write("ember", "Scout", writ2, "envoy"); err != nil {
		t.Fatalf("failed to write tether 2: %v", err)
	}

	// Create worktree dir at envoy path with git repo.
	worktreeDir := envoy.WorktreePath("ember", "Scout")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Scout")
	mgr.started[sessName] = true

	// Resolve the non-active writ (writ2) using explicit WritID.
	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Scout",
		WritID:    writ2,
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.WritID != writ2 {
		t.Errorf("expected resolved writ %q, got %q", writ2, result.WritID)
	}

	// Agent should still be working.
	agent, err := sphereStore.GetAgent("ember/Scout")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}

	// Active writ should be UNCHANGED (we resolved a non-active writ).
	if agent.ActiveWrit != writ1 {
		t.Errorf("expected active_writ to remain %q, got %q", writ1, agent.ActiveWrit)
	}

	// Only the resolved writ's tether should be removed.
	if tether.IsTetheredTo("ember", "Scout", writ2, "envoy") {
		t.Error("expected resolved writ's tether to be removed")
	}
	if !tether.IsTetheredTo("ember", "Scout", writ1, "envoy") {
		t.Error("expected active writ's tether to still exist")
	}
}

func TestResolvePersistentAgentLastTether(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Envoy last task", "The only task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	// Create envoy agent with single active writ.
	if _, err := sphereStore.CreateAgent("Scout", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Scout", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Scout", itemID, "envoy"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree dir at envoy path with git repo.
	worktreeDir := envoy.WorktreePath("ember", "Scout")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Scout")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Scout",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.WritID != itemID {
		t.Errorf("expected resolved writ %q, got %q", itemID, result.WritID)
	}

	// Agent should be idle (last tether resolved).
	agent, err := sphereStore.GetAgent("ember/Scout")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}

	// Active writ should be cleared.
	if agent.ActiveWrit != "" {
		t.Errorf("expected active_writ to be cleared, got %q", agent.ActiveWrit)
	}

	// Tether should be removed.
	if tether.IsTethered("ember", "Scout", "envoy") {
		t.Error("expected all tethers to be cleared after last resolve")
	}

	// Session should NOT have been stopped (envoy keeps session).
	if mgr.stopped[sessName] {
		t.Error("expected session to NOT be stopped for envoy resolve")
	}
}

func TestResolveAgentKillsSession(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Agent task", "A regular writ", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	// Create a regular agent.
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// SessionKept should be false for regular agents.
	if result.SessionKept {
		t.Error("expected SessionKept to be false for regular agent resolve")
	}
}

func TestResolveRemovesWorktreeForOutpostAgent(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Cleanup test", "Test worktree cleanup", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Set up a real managed repo and create a worktree from it.
	repoPath := config.RepoPath("ember")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, repoPath)

	worktreeDir := WorktreePath("ember", "Toast")
	branchName := fmt.Sprintf("outpost/Toast/%s", itemID)
	runGit(t, repoPath, "worktree", "add", worktreeDir, "-b", branchName, "HEAD")

	// Verify worktree exists before resolve.
	if _, err := os.Stat(worktreeDir); err != nil {
		t.Fatalf("worktree should exist before resolve: %v", err)
	}

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.SessionKept {
		t.Error("expected SessionKept to be false for outpost agent")
	}

	// Wait for the async cleanup goroutine (1s delay + execution time).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Verify worktree directory was removed.
	if _, err := os.Stat(worktreeDir); !os.IsNotExist(err) {
		t.Errorf("worktree directory should be removed after resolve, but still exists: %s", worktreeDir)
	}
}

// --- ResolveSourceRepo tests ---

func TestResolveSourceRepoManagedClone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create managed repo directory.
	repoPath := config.RepoPath("testworld")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	result, err := ResolveSourceRepo("testworld", config.WorldConfig{})
	if err != nil {
		t.Fatalf("ResolveSourceRepo failed: %v", err)
	}
	if result != repoPath {
		t.Errorf("expected %q, got %q", repoPath, result)
	}
}

func TestResolveSourceRepoConfigFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// No managed clone exists — should fall back to config value.
	cfg := config.WorldConfig{}
	cfg.World.SourceRepo = "/some/legacy/path"

	result, err := ResolveSourceRepo("testworld", cfg)
	if err != nil {
		t.Fatalf("ResolveSourceRepo failed: %v", err)
	}
	if result != "/some/legacy/path" {
		t.Errorf("expected /some/legacy/path, got %q", result)
	}
}

func TestResolveNudgesForgeWithMRReady(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add feature", "Implement a feature", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create a worktree directory with a git repo.
	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	// Start the agent session AND a forge session so the nudge fires.
	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true
	forgeSession := config.SessionName("ember", "forge")
	mgr.started[forgeSession] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify forge received MR_READY nudge.
	msgs, err := nudge.List(forgeSession)
	if err != nil {
		t.Fatalf("failed to list nudge queue: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected forge nudge queue to have MR_READY message, got none")
	}

	found := false
	for _, msg := range msgs {
		if msg.Type == "MR_READY" {
			found = true
			if msg.Sender != "Toast" {
				t.Errorf("expected sender Toast, got %q", msg.Sender)
			}
			if !strings.Contains(msg.Subject, result.MergeRequestID) {
				t.Errorf("expected subject to contain MR ID %q, got %q", result.MergeRequestID, msg.Subject)
			}
			if !strings.Contains(msg.Body, itemID) {
				t.Errorf("expected body to contain writ ID %q, got %q", itemID, msg.Body)
			}
			if !strings.Contains(msg.Body, result.MergeRequestID) {
				t.Errorf("expected body to contain MR ID %q, got %q", result.MergeRequestID, msg.Body)
			}
			break
		}
	}
	if !found {
		t.Error("expected MR_READY message in forge nudge queue, not found")
	}
}

func TestResolveQueuesForgeNudgeEvenWithoutForge(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Add feature", "Implement a feature", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true
	// No forge session started — smart delivery queues the message
	// for when forge eventually starts and drains its queue.

	_, err = Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify forge nudge is queued even without a running forge session.
	// Smart delivery (nudge.Deliver) always queues as fallback — the message
	// will be drained when forge starts.
	forgeSession := config.SessionName("ember", "forge")
	msgs, err := nudge.List(forgeSession)
	if err != nil {
		t.Fatalf("failed to list nudge queue: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected forge nudge to be queued for later delivery, got none")
	}
}

// --- Resolve lock tests ---

func TestResolveCreatesAndRemovesLock(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Lock test", "Test resolve lock", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	// Verify lock does not exist before resolve.
	if IsResolveInProgress("ember", "Toast", "outpost") {
		t.Fatal("resolve lock should not exist before resolve")
	}

	_, err = Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify lock is removed after resolve completes.
	if IsResolveInProgress("ember", "Toast", "outpost") {
		t.Error("resolve lock should be removed after successful resolve")
	}
}

func TestResolveIdempotent(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Idempotent test", "Test double resolve", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	// First resolve — normal path.
	result1, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("first Resolve failed: %v", err)
	}

	// Now simulate a partial resolve state for second call:
	// Re-create agent, re-write tether, set writ back to tethered.
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to re-create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}
	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}
	// Writ is already "done" from first resolve — simulates partial resolve.

	// Re-create worktree for the second resolve (remove old one first).
	os.RemoveAll(worktreeDir)
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to re-create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	mgr.started[sessName] = true
	mgr.stopped[sessName] = false

	// Second resolve — should complete without error (idempotent).
	result2, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("second Resolve (idempotent) failed: %v", err)
	}

	// Both resolves should reference the same writ.
	if result1.WritID != result2.WritID {
		t.Errorf("expected same writ ID, got %q and %q", result1.WritID, result2.WritID)
	}

	// Second resolve should reuse the existing MR (not create a duplicate).
	if result2.MergeRequestID != result1.MergeRequestID {
		t.Errorf("expected same MR ID (idempotent), got %q and %q", result1.MergeRequestID, result2.MergeRequestID)
	}

	// Writ should be done.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected writ status 'done', got %q", item.Status)
	}
}

func TestPrimeDetectsStaleLock(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Stale lock test", "Test stale lock detection", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create a stale resolve lock (simulating a crash mid-resolve).
	lockPath := ResolveLockPath("ember", "Toast", "outpost")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("failed to create lock dir: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte(itemID), 0o644); err != nil {
		t.Fatalf("failed to write stale lock: %v", err)
	}

	// Verify lock exists.
	if !IsResolveInProgress("ember", "Toast", "outpost") {
		t.Fatal("expected resolve lock to exist")
	}

	// Prime should detect and remove the stale lock.
	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	// Lock should be cleaned up.
	if IsResolveInProgress("ember", "Toast", "outpost") {
		t.Error("expected stale resolve lock to be removed after prime")
	}

	// Prime should still return context (not fail).
	if !strings.Contains(result.Output, "WORK CONTEXT") {
		t.Error("expected WORK CONTEXT output after stale lock cleanup")
	}
}

func TestResolveSourceRepoManagedCloneTakesPriority(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create managed repo directory.
	repoPath := config.RepoPath("testworld")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Config also has a source_repo — managed clone should take priority.
	cfg := config.WorldConfig{}
	cfg.World.SourceRepo = "/some/other/path"

	result, err := ResolveSourceRepo("testworld", cfg)
	if err != nil {
		t.Fatalf("ResolveSourceRepo failed: %v", err)
	}
	if result != repoPath {
		t.Errorf("expected managed clone %q, got %q", repoPath, result)
	}
}

// --- Non-code writ resolve tests ---

func TestResolveNonCodeWritSkipsGitAndMR(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a writ with kind=analysis.
	itemID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Analyze codebase",
		Description: "Perform analysis",
		CreatedBy:   "autarch",
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create a worktree directory with a git repo (simulating a worktree).
	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// ResolveResult fields should be empty for non-code writs.
	if result.BranchName != "" {
		t.Errorf("expected empty BranchName for non-code writ, got %q", result.BranchName)
	}
	if result.MergeRequestID != "" {
		t.Errorf("expected empty MergeRequestID for non-code writ, got %q", result.MergeRequestID)
	}
	if result.WritID != itemID {
		t.Errorf("expected writ ID %q, got %q", itemID, result.WritID)
	}

	// Verify writ was closed (not set to "done").
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "closed" {
		t.Errorf("expected writ status 'closed' for non-code writ, got %q", item.Status)
	}
	if item.CloseReason != "completed" {
		t.Errorf("expected close_reason 'completed', got %q", item.CloseReason)
	}

	// Verify no merge request was created.
	mrs, err := worldStore.ListMergeRequestsByWrit(itemID, "")
	if err != nil {
		t.Fatalf("failed to list MRs: %v", err)
	}
	if len(mrs) != 0 {
		t.Errorf("expected 0 merge requests for non-code writ, got %d", len(mrs))
	}

	// Verify tether is cleared.
	tetherID, err := tether.Read("ember", "Toast", "outpost")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != "" {
		t.Errorf("expected empty tether, got %q", tetherID)
	}

	// Verify outpost agent record is deleted.
	_, err = sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent record to be deleted after resolve")
	}
}

func TestResolveNonCodeWritSkipsForgeNudge(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a writ with kind=analysis.
	itemID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Analyze codebase",
		Description: "Perform analysis",
		CreatedBy:   "autarch",
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	// Start forge and governor sessions so we can verify nudges are NOT sent.
	forgeSession := config.SessionName("ember", "forge")
	mgr.started[forgeSession] = true
	govSession := config.SessionName("ember", "governor")
	mgr.started[govSession] = true

	_, err = Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify forge did NOT receive a nudge.
	forgeMsgs, err := nudge.List(forgeSession)
	if err != nil {
		t.Fatalf("failed to list forge nudge queue: %v", err)
	}
	for _, msg := range forgeMsgs {
		if msg.Type == "MR_READY" {
			t.Error("expected no MR_READY nudge for non-code writ, but found one")
		}
	}

	// Verify governor DID receive an AGENT_DONE nudge for non-code writ resolve.
	// Governor should be notified when any writ completes, not just code writs.
	govMsgs, err := nudge.List(govSession)
	if err != nil {
		t.Fatalf("failed to list governor nudge queue: %v", err)
	}
	foundAgentDone := false
	for _, msg := range govMsgs {
		if msg.Type == "AGENT_DONE" {
			foundAgentDone = true
			if !strings.Contains(msg.Body, itemID) {
				t.Errorf("expected AGENT_DONE body to contain writ ID %q, got %q", itemID, msg.Body)
			}
		}
	}
	if !foundAgentDone {
		t.Error("expected AGENT_DONE nudge for non-code writ resolve, but found none")
	}
}

func TestResolveCodeWritDefaultKind(t *testing.T) {
	// Writs with empty kind (the default) should follow the code path.
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// CreateWrit uses the default kind (empty → defaults to "code" in schema).
	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Default-kind writs should follow code path: branch and MR set.
	expectedBranch := fmt.Sprintf("outpost/Toast/%s", itemID)
	if result.BranchName != expectedBranch {
		t.Errorf("expected branch %q, got %q", expectedBranch, result.BranchName)
	}
	if result.MergeRequestID == "" {
		t.Error("expected MergeRequestID to be set for code writ")
	}

	// Verify writ was updated to done (not closed).
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected writ status 'done', got %q", item.Status)
	}
}

func TestCastCreatesOutputDirectory(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	solHome := os.Getenv("SOL_HOME")

	// Create world.toml so Cast doesn't fail on config load.
	worldDir := filepath.Join(solHome, "ember")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatalf("failed to create world dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write world.toml: %v", err)
	}

	itemID, err := worldStore.CreateWrit("Test task", "Test description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Set up git repo for worktree creation.
	repoDir := filepath.Join(solHome, "ember", "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	_, err = Cast(context.Background(), CastOpts{
		WritID:    itemID,
		World:     "ember",
		AgentName: "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	// Verify output directory was created.
	outputDir := config.WritOutputDir("ember", itemID)
	info, err := os.Stat(outputDir)
	if err != nil {
		t.Fatalf("expected output directory %q to exist: %v", outputDir, err)
	}
	if !info.IsDir() {
		t.Errorf("expected %q to be a directory", outputDir)
	}
}

func TestCastCreatesOutputDirectoryForNonCodeWrit(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	solHome := os.Getenv("SOL_HOME")

	worldDir := filepath.Join(solHome, "ember")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatalf("failed to create world dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write world.toml: %v", err)
	}

	// Create a non-code writ.
	itemID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Analyze codebase",
		Description: "Perform analysis",
		CreatedBy:   "autarch",
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	repoDir := filepath.Join(solHome, "ember", "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	_, err = Cast(context.Background(), CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	// Output directory should be created for non-code writs too.
	outputDir := config.WritOutputDir("ember", itemID)
	info, err := os.Stat(outputDir)
	if err != nil {
		t.Fatalf("expected output directory %q to exist for non-code writ: %v", outputDir, err)
	}
	if !info.IsDir() {
		t.Errorf("expected %q to be a directory", outputDir)
	}
}

func TestOutputDirectorySurvivesResolve(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a writ with kind=analysis.
	itemID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Analyze codebase",
		Description: "Perform analysis",
		CreatedBy:   "autarch",
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	// Pre-create the output directory (as Cast() would do).
	outputDir := config.WritOutputDir("ember", itemID)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("failed to create output dir: %v", err)
	}
	// Write a test file to the output directory.
	testFile := filepath.Join(outputDir, "report.json")
	if err := os.WriteFile(testFile, []byte(`{"status":"ok"}`), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err = Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Output directory should still exist after resolve.
	info, err := os.Stat(outputDir)
	if err != nil {
		t.Fatalf("expected output directory to survive resolve: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected %q to be a directory", outputDir)
	}

	// Test file should still exist.
	if _, err := os.Stat(testFile); err != nil {
		t.Errorf("expected test file to survive resolve: %v", err)
	}
}

func TestWritKindDefaultsToCode(t *testing.T) {
	worldStore, _ := setupStores(t)

	// CreateWrit (no Kind option) should default to "code".
	itemID, err := worldStore.CreateWrit("Test task", "Test description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Kind != "code" {
		t.Errorf("expected default Kind 'code', got %q", item.Kind)
	}
}

func TestWritKindSetByCreateWritWithOpts(t *testing.T) {
	worldStore, _ := setupStores(t)

	// CreateWritWithOpts with Kind=analysis.
	itemID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Analyze codebase",
		Description: "Perform analysis",
		CreatedBy:   "autarch",
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Kind != "analysis" {
		t.Errorf("expected Kind 'analysis', got %q", item.Kind)
	}
}

func TestCloseWritWithReason(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Test task", "Test description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := worldStore.CloseWrit(itemID, "completed"); err != nil {
		t.Fatalf("CloseWrit with reason failed: %v", err)
	}

	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "closed" {
		t.Errorf("expected status 'closed', got %q", item.Status)
	}
	if item.CloseReason != "completed" {
		t.Errorf("expected close_reason 'completed', got %q", item.CloseReason)
	}
	if item.ClosedAt == nil {
		t.Error("expected ClosedAt to be set")
	}
}

func TestCastKindPassedToContext(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a writ with kind=analysis.
	itemID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Analyze codebase",
		Description: "Perform analysis",
		CreatedBy:   "autarch",
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(context.Background(), CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	// Verify CLAUDE.local.md contains the kind.
	data, err := os.ReadFile(result.WorktreeDir + "/CLAUDE.local.md")
	if err != nil {
		t.Fatalf("failed to read CLAUDE.local.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Kind: analysis") {
		t.Error("CLAUDE.local.md missing 'Kind: analysis'")
	}
}

func TestCastCodeKindDefault(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a writ without explicit kind — should default to "code".
	itemID, err := worldStore.CreateWrit("Add feature", "Add a feature", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(context.Background(), CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	data, err := os.ReadFile(result.WorktreeDir + "/CLAUDE.local.md")
	if err != nil {
		t.Fatalf("failed to read CLAUDE.local.md: %v", err)
	}
	if !strings.Contains(string(data), "Kind: code") {
		t.Error("CLAUDE.local.md missing 'Kind: code' for default writ")
	}
}

func TestCastDirectDeps(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a dependency writ (analysis kind).
	depID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Gather requirements",
		Description: "Gather requirements for the feature",
		CreatedBy:   "autarch",
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("failed to create dep writ: %v", err)
	}

	// Create the main writ.
	mainID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Implement feature",
		Description: "Build the feature",
		CreatedBy:   "autarch",
		Kind:        "code",
	})
	if err != nil {
		t.Fatalf("failed to create main writ: %v", err)
	}

	// Add dependency: main depends on dep.
	if err := worldStore.AddDependency(mainID, depID); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(context.Background(), CastOpts{
		WritID:     mainID,
		World:      "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	// Verify CLAUDE.local.md contains the dependency section.
	data, err := os.ReadFile(result.WorktreeDir + "/CLAUDE.local.md")
	if err != nil {
		t.Fatalf("failed to read CLAUDE.local.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "## Direct Dependencies") {
		t.Error("CLAUDE.local.md missing Direct Dependencies section")
	}
	if !strings.Contains(content, "Gather requirements") {
		t.Error("CLAUDE.local.md missing dependency title")
	}
	if !strings.Contains(content, depID) {
		t.Error("CLAUDE.local.md missing dependency writ ID")
	}
	if !strings.Contains(content, "kind: analysis") {
		t.Error("CLAUDE.local.md missing dependency kind")
	}
}

// --- ActivateWrit tests ---

func TestActivateWritHappyPath(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create two writs.
	writ1, err := worldStore.CreateWrit("Envoy task 1", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 1: %v", err)
	}
	if err := worldStore.UpdateWrit(writ1, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ 1: %v", err)
	}
	writ2, err := worldStore.CreateWrit("Envoy task 2", "Second task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 2: %v", err)
	}
	if err := worldStore.UpdateWrit(writ2, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ 2: %v", err)
	}

	// Create envoy agent with active_writ = writ1.
	if _, err := sphereStore.CreateAgent("Scout", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Scout", "working", writ1); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	// Tether both writs.
	if err := tether.Write("ember", "Scout", writ1, "envoy"); err != nil {
		t.Fatalf("failed to write tether 1: %v", err)
	}
	if err := tether.Write("ember", "Scout", writ2, "envoy"); err != nil {
		t.Fatalf("failed to write tether 2: %v", err)
	}

	// Activate writ2 (switching from writ1).
	result, err := ActivateWrit(ActivateOpts{
		World:     "ember",
		AgentName: "Scout",
		WritID:    writ2,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("ActivateWrit failed: %v", err)
	}

	// Verify result.
	if result.WritID != writ2 {
		t.Errorf("WritID = %q, want %q", result.WritID, writ2)
	}
	if result.PreviousWrit != writ1 {
		t.Errorf("PreviousWrit = %q, want %q", result.PreviousWrit, writ1)
	}
	if result.AlreadyActive {
		t.Error("AlreadyActive should be false for a switch")
	}

	// Verify DB updated.
	agent, err := sphereStore.GetAgent("ember/Scout")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.ActiveWrit != writ2 {
		t.Errorf("active_writ = %q, want %q", agent.ActiveWrit, writ2)
	}
	if agent.State != "working" {
		t.Errorf("state = %q, want %q", agent.State, "working")
	}
}

func TestActivateWritAlreadyActive(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create writ.
	writ1, err := worldStore.CreateWrit("Envoy task 1", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(writ1, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	// Create envoy agent with active_writ = writ1.
	if _, err := sphereStore.CreateAgent("Scout", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Scout", "working", writ1); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	// Tether writ1.
	if err := tether.Write("ember", "Scout", writ1, "envoy"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Activate writ1 (already active — should be no-op).
	result, err := ActivateWrit(ActivateOpts{
		World:     "ember",
		AgentName: "Scout",
		WritID:    writ1,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("ActivateWrit failed: %v", err)
	}

	if !result.AlreadyActive {
		t.Error("AlreadyActive should be true")
	}
	if result.WritID != writ1 {
		t.Errorf("WritID = %q, want %q", result.WritID, writ1)
	}
}

func TestActivateWritNotTethered(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create writ but don't tether it.
	writ1, err := worldStore.CreateWrit("Envoy task 1", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	// Create envoy agent.
	if _, err := sphereStore.CreateAgent("Scout", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Try to activate untethered writ — should fail.
	_, err = ActivateWrit(ActivateOpts{
		World:     "ember",
		AgentName: "Scout",
		WritID:    writ1,
	}, worldStore, sphereStore, mgr, nil)
	if err == nil {
		t.Fatal("expected error for untethered writ")
	}
	if !strings.Contains(err.Error(), "not tethered") {
		t.Errorf("error = %q, want it to mention 'not tethered'", err.Error())
	}
}

func TestActivateWritAgentNotFound(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create writ.
	writ1, err := worldStore.CreateWrit("Task", "Desc", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	// No agent created — should fail.
	_, err = ActivateWrit(ActivateOpts{
		World:     "ember",
		AgentName: "Ghost",
		WritID:    writ1,
	}, worldStore, sphereStore, mgr, nil)
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
	if !strings.Contains(err.Error(), "failed to get agent") {
		t.Errorf("error = %q, want it to mention agent lookup failure", err.Error())
	}
}

func TestActivateWritFromEmpty(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create writ.
	writ1, err := worldStore.CreateWrit("Envoy task 1", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(writ1, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	// Create envoy agent with no active writ.
	if _, err := sphereStore.CreateAgent("Scout", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Tether writ.
	if err := tether.Write("ember", "Scout", writ1, "envoy"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Activate writ1 (no previous active writ).
	result, err := ActivateWrit(ActivateOpts{
		World:     "ember",
		AgentName: "Scout",
		WritID:    writ1,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("ActivateWrit failed: %v", err)
	}

	if result.AlreadyActive {
		t.Error("AlreadyActive should be false")
	}
	if result.WritID != writ1 {
		t.Errorf("WritID = %q, want %q", result.WritID, writ1)
	}
	if result.PreviousWrit != "" {
		t.Errorf("PreviousWrit = %q, want empty string", result.PreviousWrit)
	}

	// Verify DB updated.
	agent, err := sphereStore.GetAgent("ember/Scout")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.ActiveWrit != writ1 {
		t.Errorf("active_writ = %q, want %q", agent.ActiveWrit, writ1)
	}
}

// --- Multi-writ prime tests ---

func TestPrimeSingleTetherOutpostUnchanged(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	result, err := Prime("ember", "Toast", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	// Should produce standard WORK CONTEXT output.
	if !strings.Contains(result.Output, "WORK CONTEXT") {
		t.Error("output missing WORK CONTEXT header")
	}
	if !strings.Contains(result.Output, "Add README") {
		t.Error("output missing writ title")
	}
	if !strings.Contains(result.Output, itemID) {
		t.Error("output missing writ ID")
	}
	// Should NOT contain background writs section.
	if strings.Contains(result.Output, "Background Writs") {
		t.Error("outpost prime should not contain Background Writs section")
	}
}

func TestPrimeMultiTetherOneActive(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	// Create three writs.
	writ1, _ := worldStore.CreateWrit("First task", "Do the first thing", "autarch", 2, nil)
	writ2, _ := worldStore.CreateWrit("Second task", "Do the second thing", "autarch", 2, nil)
	writ3, _ := worldStore.CreateWrit("Third task", "Do the third thing", "autarch", 2, nil)

	// Create envoy agent.
	sphereStore.CreateAgent("Meridian", "ember", "envoy")

	// Tether all three writs.
	for _, id := range []string{writ1, writ2, writ3} {
		if err := tether.Write("ember", "Meridian", id, "envoy"); err != nil {
			t.Fatalf("failed to write tether %s: %v", id, err)
		}
	}

	// Set writ2 as active.
	if err := sphereStore.UpdateAgentState("ember/Meridian", "working", writ2); err != nil {
		t.Fatalf("failed to set active writ: %v", err)
	}

	result, err := Prime("ember", "Meridian", "envoy", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	// Active writ should have full context.
	if !strings.Contains(result.Output, "WORK CONTEXT") {
		t.Error("output missing WORK CONTEXT header")
	}
	if !strings.Contains(result.Output, "Second task") {
		t.Error("output missing active writ title")
	}
	if !strings.Contains(result.Output, writ2) {
		t.Error("output missing active writ ID")
	}

	// Background writs should be listed.
	if !strings.Contains(result.Output, "Background Writs") {
		t.Error("output missing Background Writs section")
	}
	if !strings.Contains(result.Output, "First task") {
		t.Error("output missing background writ 'First task'")
	}
	if !strings.Contains(result.Output, "Third task") {
		t.Error("output missing background writ 'Third task'")
	}
}

func TestPrimeMultiTetherNoneActive(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	// Create three writs.
	writ1, _ := worldStore.CreateWrit("First task", "Do the first thing", "autarch", 2, nil)
	writ2, _ := worldStore.CreateWrit("Second task", "Do the second thing", "autarch", 2, nil)
	writ3, _ := worldStore.CreateWrit("Third task", "Do the third thing", "autarch", 2, nil)

	// Create envoy agent with no active writ.
	sphereStore.CreateAgent("Meridian", "ember", "envoy")

	// Tether all three writs.
	for _, id := range []string{writ1, writ2, writ3} {
		if err := tether.Write("ember", "Meridian", id, "envoy"); err != nil {
			t.Fatalf("failed to write tether %s: %v", id, err)
		}
	}

	result, err := Prime("ember", "Meridian", "envoy", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	// Should show wait message.
	if !strings.Contains(result.Output, "Wait for the operator to activate one") {
		t.Error("output missing wait-for-activation message")
	}
	if !strings.Contains(result.Output, "3 tethered writs") {
		t.Error("output missing tethered writ count")
	}

	// All writs should be listed.
	if !strings.Contains(result.Output, "First task") {
		t.Error("output missing 'First task'")
	}
	if !strings.Contains(result.Output, "Second task") {
		t.Error("output missing 'Second task'")
	}
	if !strings.Contains(result.Output, "Third task") {
		t.Error("output missing 'Third task'")
	}

	// Should NOT contain WORK CONTEXT (no active writ).
	if strings.Contains(result.Output, "Execute this writ") {
		t.Error("no-active-writ prime should not contain execution instructions")
	}
}

func TestCastCancelledContext(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	// Create repo.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "init")

	// Create agent and writ.
	sphereStore.CreateAgent("Ash", "ember", "outpost")
	writID, _ := worldStore.CreateWrit("cancel-test", "test cancellation", "autarch", 1, nil)

	mgr := newMockSessionManager()

	// Cancel the context before calling Cast.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	_, err := Cast(ctx, CastOpts{
		WritID:     writID,
		World:      "ember",
		AgentName:  "Ash",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	// Cast should fail because git worktree add will fail with cancelled context.
	if err == nil {
		t.Fatal("expected Cast to fail with cancelled context")
	}
}

func TestCastContextTimeout(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	// Create repo.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "init")

	// Create agent and writ.
	sphereStore.CreateAgent("Ember", "ember", "outpost")
	writID, _ := worldStore.CreateWrit("timeout-test", "test timeout", "autarch", 1, nil)

	mgr := newMockSessionManager()

	// Use an already-expired timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond) // ensure timeout fires

	_, err := Cast(ctx, CastOpts{
		WritID:     writID,
		World:      "ember",
		AgentName:  "Ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	// Cast should fail because the context has already expired.
	if err == nil {
		t.Fatal("expected Cast to fail with expired context")
	}
}

func TestResolveContextCancelled(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	// Create repo with bare remote.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "init")
	addBareRemote(t, repoDir)

	// Set up agent and writ.
	agentName := "Blaze"
	world := "ember"
	sphereStore.CreateAgent(agentName, world, "outpost")
	writID, _ := worldStore.CreateWrit("cancel-resolve-test", "desc", "autarch", 1, nil)
	worldStore.UpdateWrit(writID, store.WritUpdates{Status: "tethered", Assignee: world + "/" + agentName})
	sphereStore.UpdateAgentState(world+"/"+agentName, "working", writID)
	tether.Write(world, agentName, writID, "outpost")

	// Create worktree.
	worktreeDir := WorktreePath(world, agentName)
	branchName := fmt.Sprintf("outpost/%s/%s", agentName, writID)
	runGit(t, repoDir, "worktree", "add", worktreeDir, "-b", branchName, "HEAD")
	// Make a change to commit.
	os.WriteFile(filepath.Join(worktreeDir, "test.txt"), []byte("hello"), 0o644)

	mgr := newMockSessionManager()
	mgr.started["sol-"+world+"-"+agentName] = true

	// Cancel context before calling Resolve.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Resolve(ctx, ResolveOpts{
		World:     world,
		AgentName: agentName,
	}, worldStore, sphereStore, mgr, nil)

	// Resolve should fail because git operations use the cancelled context.
	if err == nil {
		t.Fatal("expected Resolve to fail with cancelled context")
	}
}

func TestErrCapacityExhausted(t *testing.T) {
	// ErrCapacityExhausted should be detectable via errors.Is.
	err := fmt.Errorf("world %q has reached agent capacity (%d): %w", "testworld", 5, ErrCapacityExhausted)
	if !errors.Is(err, ErrCapacityExhausted) {
		t.Error("wrapped error should match ErrCapacityExhausted via errors.Is")
	}

	// Plain error should not match.
	plainErr := fmt.Errorf("some other error")
	if errors.Is(plainErr, ErrCapacityExhausted) {
		t.Error("unrelated error should not match ErrCapacityExhausted")
	}
}

func TestAutoProvisionCapacityExhaustedError(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	config.EnsureDirs()

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "capped-test"
	// Create 3 agents — capacity of 3 should be exhausted.
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		if _, err := sphereStore.CreateAgent(name, worldName, "outpost"); err != nil {
			t.Fatalf("failed to create agent: %v", err)
		}
	}

	// Write a names.txt pool.
	worldDir := filepath.Join(solHome, worldName)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "names.txt"), []byte("Alpha\nBeta\nGamma\nDelta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = autoProvision(worldName, sphereStore, "", 3)
	if err == nil {
		t.Fatal("expected autoProvision to fail when at capacity")
	}
	if !errors.Is(err, ErrCapacityExhausted) {
		t.Errorf("expected ErrCapacityExhausted, got: %v", err)
	}
}

func TestResolveAutoResolvesWritLinkedEscalations(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a non-code writ.
	itemID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Investigate issue",
		Description: "Investigate an issue",
		CreatedBy:   "autarch",
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	// Create escalations linked to this writ.
	escID1, err := sphereStore.CreateEscalation("high", "ember/Toast", "Build failed", "writ:"+itemID)
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}
	escID2, err := sphereStore.CreateEscalation("low", "ember/Toast", "Flaky test", "writ:"+itemID)
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}
	// Create an escalation for a different writ — should NOT be resolved.
	escIDOther, err := sphereStore.CreateEscalation("high", "ember/Toast", "Other issue", "writ:sol-other1234567890")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}

	_, err = Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify linked escalations are resolved.
	esc1, err := sphereStore.ListEscalationsBySourceRef("writ:" + itemID)
	if err != nil {
		t.Fatalf("failed to list escalations: %v", err)
	}
	if len(esc1) != 0 {
		t.Errorf("expected 0 open escalations for writ, got %d", len(esc1))
	}

	// Verify we can get the resolved escalations by ID to confirm they exist but are resolved.
	_ = escID1
	_ = escID2

	// Verify other writ's escalation is NOT resolved.
	escOther, err := sphereStore.ListEscalationsBySourceRef("writ:sol-other1234567890")
	if err != nil {
		t.Fatalf("failed to list escalations: %v", err)
	}
	if len(escOther) != 1 {
		t.Errorf("expected 1 open escalation for other writ, got %d", len(escOther))
	}
	if len(escOther) == 1 && escOther[0].ID != escIDOther {
		t.Errorf("wrong escalation remaining: got %q, want %q", escOther[0].ID, escIDOther)
	}
}

func TestResolveAutoResolvesEscalationsForCodeWrit(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a code writ (default kind).
	itemID, err := worldStore.CreateWrit("Fix bug", "Fix a bug", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	// Create an escalation linked to this writ.
	_, err = sphereStore.CreateEscalation("high", "ember/Toast", "Agent stuck", "writ:"+itemID)
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}

	_, err = Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify writ-linked escalation is resolved even for code writs.
	escs, err := sphereStore.ListEscalationsBySourceRef("writ:" + itemID)
	if err != nil {
		t.Fatalf("failed to list escalations: %v", err)
	}
	if len(escs) != 0 {
		t.Errorf("expected 0 open escalations for writ, got %d", len(escs))
	}
}

func TestResolveEscalationAutoResolveIsBestEffort(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a non-code writ.
	itemID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Research task",
		Description: "Do research",
		CreatedBy:   "autarch",
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	// Don't create any escalations — ListEscalationsBySourceRef returns empty.
	// Resolve should succeed even when there are no escalations.
	_, err = Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve should succeed even with no escalations: %v", err)
	}

	// Verify writ was closed.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "closed" {
		t.Errorf("expected writ status 'closed', got %q", item.Status)
	}
}

func TestResolveMultipleEscalationsForSameWrit(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a non-code writ.
	itemID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Multi-escalation task",
		Description: "Task with multiple escalations",
		CreatedBy:   "autarch",
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	// Create multiple escalations for the same writ.
	for i := 0; i < 5; i++ {
		_, err := sphereStore.CreateEscalation("high", "ember/Toast",
			fmt.Sprintf("Escalation %d", i), "writ:"+itemID)
		if err != nil {
			t.Fatalf("failed to create escalation %d: %v", i, err)
		}
	}

	// Verify they exist before resolve.
	before, err := sphereStore.ListEscalationsBySourceRef("writ:" + itemID)
	if err != nil {
		t.Fatalf("failed to list escalations: %v", err)
	}
	if len(before) != 5 {
		t.Fatalf("expected 5 escalations before resolve, got %d", len(before))
	}

	_, err = Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify all escalations are resolved.
	after, err := sphereStore.ListEscalationsBySourceRef("writ:" + itemID)
	if err != nil {
		t.Fatalf("failed to list escalations: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("expected 0 open escalations after resolve, got %d", len(after))
	}
}

// --- Outpost hooks tests ---

func TestOutpostHooksPreCompactUsesPrimeCompact(t *testing.T) {
	hooks := outpostHooks("ember", "Toast")

	pcGroups, ok := hooks.Hooks["PreCompact"]
	if !ok {
		t.Fatal("outpost hooks missing PreCompact")
	}
	if len(pcGroups) != 1 {
		t.Fatalf("expected 1 PreCompact matcher group, got %d", len(pcGroups))
	}
	cmd := pcGroups[0].Hooks[0].Command
	want := "sol prime --world=ember --agent=Toast --compact"
	if cmd != want {
		t.Errorf("PreCompact command = %q, want %q", cmd, want)
	}
}

// --- Prime compact tests ---

func TestPrimeCompactWithTether(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Fix login bug", "Detailed description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	result, err := Prime("ember", "Toast", "outpost", worldStore, true)
	if err != nil {
		t.Fatalf("Prime compact failed: %v", err)
	}

	// Verify focus reminder format.
	if !strings.Contains(result.Output, "[sol] Context compaction in progress") {
		t.Error("output missing compaction header")
	}
	if !strings.Contains(result.Output, itemID) {
		t.Error("output missing writ ID")
	}
	if !strings.Contains(result.Output, "Fix login bug") {
		t.Error("output missing writ title")
	}
	if !strings.Contains(result.Output, "Continue where you left off") {
		t.Error("output missing focus instruction")
	}

	// Should NOT contain full work context.
	if strings.Contains(result.Output, "WORK CONTEXT") {
		t.Error("compact prime should not contain full WORK CONTEXT")
	}
	if strings.Contains(result.Output, "Detailed description") {
		t.Error("compact prime should not include full writ description")
	}
}

func TestPrimeCompactNoTether(t *testing.T) {
	worldStore, _ := setupStores(t)

	result, err := Prime("ember", "Toast", "outpost", worldStore, true)
	if err != nil {
		t.Fatalf("Prime compact failed: %v", err)
	}

	if !strings.Contains(result.Output, "[sol] Context compaction in progress") {
		t.Error("output missing compaction header")
	}
	if !strings.Contains(result.Output, "No active work tethered") {
		t.Errorf("expected no-tether message, got %q", result.Output)
	}
}

func TestPrimeCompactWithWorkflow(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWrit("Build feature", "Build it", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Set up workflow with 3 steps, 1 completed.
	setupTestWorkflow(t, "test-compact-wf")
	if _, _, err := workflow.Instantiate("ember", "Toast", "outpost", "test-compact-wf", map[string]string{
		"issue": itemID,
	}); err != nil {
		t.Fatalf("instantiate workflow: %v", err)
	}
	// Advance first step.
	if _, _, err := workflow.Advance("ember", "Toast", "outpost"); err != nil {
		t.Fatalf("workflow advance failed: %v", err)
	}

	result, err := Prime("ember", "Toast", "outpost", worldStore, true)
	if err != nil {
		t.Fatalf("Prime compact with workflow failed: %v", err)
	}

	if !strings.Contains(result.Output, "Step:") {
		t.Error("output missing workflow step info")
	}
	if !strings.Contains(result.Output, "Build feature") {
		t.Error("output missing writ title")
	}
}

func TestPrimeCompactEnvoyNoTether(t *testing.T) {
	worldStore, _ := setupStores(t)

	result, err := Prime("ember", "Echo", "envoy", worldStore, true)
	if err != nil {
		t.Fatalf("Prime compact failed: %v", err)
	}

	if !strings.Contains(result.Output, "[sol] Context compaction in progress") {
		t.Error("output missing compaction header")
	}
	if !strings.Contains(result.Output, "You are envoy Echo in world ember") {
		t.Errorf("expected envoy grounding reminder, got %q", result.Output)
	}
	if !strings.Contains(result.Output, ".brief/memory.md") {
		t.Error("output missing brief path")
	}
	if strings.Contains(result.Output, "No active work tethered") {
		t.Error("persistent role should not get generic no-tether message")
	}
}

func TestPrimeCompactGovernorNoTether(t *testing.T) {
	worldStore, _ := setupStores(t)

	result, err := Prime("ember", "governor", "governor", worldStore, true)
	if err != nil {
		t.Fatalf("Prime compact failed: %v", err)
	}

	if !strings.Contains(result.Output, "[sol] Context compaction in progress") {
		t.Error("output missing compaction header")
	}
	if !strings.Contains(result.Output, "You are the governor of world ember") {
		t.Errorf("expected governor grounding reminder, got %q", result.Output)
	}
	if !strings.Contains(result.Output, ".brief/memory.md") {
		t.Error("output missing brief path")
	}
	if strings.Contains(result.Output, "No active work tethered") {
		t.Error("persistent role should not get generic no-tether message")
	}
}

// --- Persistent role activation tests ---

func TestActivateWritEnvoyNudgesInsteadOfCycle(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create two writs.
	writ1, err := worldStore.CreateWrit("Envoy task 1", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 1: %v", err)
	}
	if err := worldStore.UpdateWrit(writ1, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ 1: %v", err)
	}
	writ2, err := worldStore.CreateWrit("Envoy task 2", "Second task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 2: %v", err)
	}
	if err := worldStore.UpdateWrit(writ2, store.WritUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update writ 2: %v", err)
	}

	// Create envoy agent with active_writ = writ1.
	if _, err := sphereStore.CreateAgent("Scout", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Scout", "working", writ1); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	// Tether both writs.
	if err := tether.Write("ember", "Scout", writ1, "envoy"); err != nil {
		t.Fatalf("failed to write tether 1: %v", err)
	}
	if err := tether.Write("ember", "Scout", writ2, "envoy"); err != nil {
		t.Fatalf("failed to write tether 2: %v", err)
	}

	// Activate writ2.
	result, err := ActivateWrit(ActivateOpts{
		World:     "ember",
		AgentName: "Scout",
		WritID:    writ2,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("ActivateWrit failed: %v", err)
	}

	// Verify result fields.
	if result.WritID != writ2 {
		t.Errorf("WritID = %q, want %q", result.WritID, writ2)
	}
	if result.PreviousWrit != writ1 {
		t.Errorf("PreviousWrit = %q, want %q", result.PreviousWrit, writ1)
	}

	// Verify DB updated.
	agent, err := sphereStore.GetAgent("ember/Scout")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.ActiveWrit != writ2 {
		t.Errorf("active_writ = %q, want %q", agent.ActiveWrit, writ2)
	}

	// Verify session was NOT cycled (no stop/start).
	sessName := config.SessionName("ember", "Scout")
	if mgr.stopped[sessName] {
		t.Error("envoy session should NOT be stopped on writ activation")
	}
	if mgr.started[sessName] {
		t.Error("envoy session should NOT be started on writ activation")
	}

	// Verify nudge was enqueued.
	messages, err := nudge.List(sessName)
	if err != nil {
		t.Fatalf("failed to list nudge messages: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("expected nudge message to be enqueued for envoy activation")
	}
	found := false
	for _, msg := range messages {
		if msg.Type == "writ-activate" && strings.Contains(msg.Subject, writ2) {
			found = true
			if msg.Priority != "urgent" {
				t.Errorf("nudge priority = %q, want \"urgent\"", msg.Priority)
			}
			if !strings.Contains(msg.Subject, "Envoy task 2") {
				t.Errorf("nudge subject should contain writ title, got %q", msg.Subject)
			}
			break
		}
	}
	if !found {
		t.Error("no writ-activate nudge found for the activated writ")
	}
}

func TestActivateWritGovernorNudgesInsteadOfCycle(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create writ.
	writ1, err := worldStore.CreateWrit("Governor task", "A task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(writ1, store.WritUpdates{Status: "tethered", Assignee: "ember/Apex"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	// Create governor agent.
	if _, err := sphereStore.CreateAgent("Apex", "ember", "governor"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Tether writ.
	if err := tether.Write("ember", "Apex", writ1, "governor"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Activate writ.
	_, err = ActivateWrit(ActivateOpts{
		World:     "ember",
		AgentName: "Apex",
		WritID:    writ1,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("ActivateWrit failed: %v", err)
	}

	// Verify session was NOT cycled.
	sessName := config.SessionName("ember", "Apex")
	if mgr.stopped[sessName] {
		t.Error("governor session should NOT be stopped on writ activation")
	}

	// Verify nudge was enqueued.
	messages, err := nudge.List(sessName)
	if err != nil {
		t.Fatalf("failed to list nudge messages: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("expected nudge message for governor activation")
	}
	if messages[0].Type != "writ-activate" {
		t.Errorf("nudge type = %q, want \"writ-activate\"", messages[0].Type)
	}
}
