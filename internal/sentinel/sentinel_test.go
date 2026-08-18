// Package sentinel_test: why are these tests slow under -race?
//
// Profiling (go test -race -cpuprofile=...) shows that ~62% of wall time is
// spent in setupTestEnv → store.OpenSphere / store.OpenWorld → schema
// migrations. The culprit is modernc.org/sqlite, a pure-Go SQLite port that
// uses modernc.org/libc (a C-to-Go translation). modernc.org/libc performs
// heavy unsafe pointer arithmetic; under -race, Go's checkptr validation
// (runtime.checkptrBase, checkptrAlignment, checkptrArithmetic) fires on
// every unsafe dereference, making each migration SQL parse 5–10× slower.
// With 96 tests each opening fresh stores and running all schema migrations,
// this cost multiplies to ~24 s out of 42 s total under -race (vs 1.5 s
// without -race).
//
// Fix: TestMain opens the SQLite stores ONCE and stores them in
// sharedSphereStore / sharedWorldStore. Each test's setupTestEnv resets all
// table rows (DELETE FROM ...) instead of reopening the database. Schema
// migrations run a single time at startup, reducing checkptr overhead from
// 96× to 1×. File-system state (tether dirs, outpost dirs, events log) stays
// isolated via per-test t.TempDir() set as SOL_HOME.

package sentinel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/jsoncontract"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// sharedSphereStore and sharedWorldStore are opened once in TestMain and
// reused across all tests. Each test resets their contents via
// resetSharedStores instead of reopening (which would re-run migrations).
var (
	sharedSphereStore *store.SphereStore
	sharedWorldStore  *store.WorldStore
)

// TestMain opens the shared SQLite stores once for the entire test binary run
// to avoid repeating schema migrations (and their checkptr overhead) for each
// of the ~96 tests. Each test still gets its own SOL_HOME temp directory for
// file-system isolation.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sentinel-shared-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: MkdirTemp: %v\n", err)
		os.Exit(1)
	}

	// Point SOL_HOME at the shared dir so store.OpenSphere/OpenWorld can
	// derive the correct paths. Individual tests redirect SOL_HOME to their
	// own temp dir via t.Setenv; the shared DB files remain open by fd.
	if err := os.Setenv("SOL_HOME", dir); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: Setenv SOL_HOME: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: MkdirAll .store: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: MkdirAll .runtime: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	sharedSphereStore, err = store.OpenSphere()
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: OpenSphere: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	sharedWorldStore, err = store.OpenWorld("ember")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: OpenWorld: %v\n", err)
		sharedSphereStore.Close()
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()

	// Cleanup: close before RemoveAll so WAL files are flushed.
	sharedSphereStore.Close()
	sharedWorldStore.Close()
	os.RemoveAll(dir)
	os.Exit(code)
}

// --- Mock implementations ---

type mockSessions struct {
	mu       sync.Mutex
	alive    map[string]bool
	captures map[string]string // session name → captured output
	started  []string
	stopped  []string
	cycled   []string
	injected []injectCall
	lastCmds map[string]string // session name → last command used in Start/Cycle
}

type injectCall struct {
	Session string
	Text    string
}

func newMockSessions() *mockSessions {
	return &mockSessions{
		alive:    make(map[string]bool),
		captures: make(map[string]string),
		lastCmds: make(map[string]string),
	}
}

func (m *mockSessions) Exists(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alive[name]
}

func (m *mockSessions) Capture(name string, lines int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if output, ok := m.captures[name]; ok {
		return output, nil
	}
	return "", fmt.Errorf("session %q not found", name)
}

func (m *mockSessions) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive[name] = true
	m.started = append(m.started, name)
	m.lastCmds[name] = cmd
	return nil
}

func (m *mockSessions) Stop(name string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alive, name)
	m.stopped = append(m.stopped, name)
	return nil
}

func (m *mockSessions) Cycle(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cycled = append(m.cycled, name)
	m.lastCmds[name] = cmd
	return nil
}

func (m *mockSessions) NudgeSession(name string, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.injected = append(m.injected, injectCall{Session: name, Text: message})
	return nil
}

func (m *mockSessions) getStarted() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.started))
	copy(result, m.started)
	return result
}

func (m *mockSessions) getStopped() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.stopped))
	copy(result, m.stopped)
	return result
}

func (m *mockSessions) getCycled() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.cycled))
	copy(result, m.cycled)
	return result
}

func (m *mockSessions) getInjected() []injectCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]injectCall, len(m.injected))
	copy(result, m.injected)
	return result
}

func (m *mockSessions) getLastCmd(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastCmds[name]
}

// --- Test helpers ---

func setupTestEnv(t *testing.T) (*store.SphereStore, *store.WorldStore) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a fake token so startup.Launch can inject credentials.
	jsoncontract.WriteTestToken(t, dir)

	// Reset shared stores so this test starts with empty tables.
	// Schema migrations already ran once in TestMain — see package comment.
	resetSharedStores(t)

	return sharedSphereStore, sharedWorldStore
}

// resetSharedStores deletes all user-data rows from both shared SQLite stores.
// Tables are cleared inside a single transaction per store so the WAL commit
// (and its fsync) happens once instead of once per table. Tables are deleted
// in FK-safe order (children before parents) because the connection uses
// foreign_keys=ON.
func resetSharedStores(t *testing.T) {
	t.Helper()

	// World (ember.db): delete dependents before writs.
	//   token_usage → agent_history (FK: history_id)
	//   labels, dependencies, merge_requests → writs (FK: writ_id)
	worldTx, err := sharedWorldStore.DB().Begin()
	if err != nil {
		t.Fatalf("resetSharedStores: begin world tx: %v", err)
	}
	defer worldTx.Rollback() //nolint:errcheck
	for _, tbl := range []string{
		"token_usage",
		"labels",
		"dependencies",
		"merge_requests",
		"agent_history",
		"writs",
	} {
		if _, err := worldTx.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatalf("resetSharedStores: clear world table %q: %v", tbl, err)
		}
	}
	if err := worldTx.Commit(); err != nil {
		t.Fatalf("resetSharedStores: commit world tx: %v", err)
	}

	// Sphere (sphere.db): delete dependents before their parents.
	//   caravan_items, caravan_dependencies → caravans
	sphereTx, err := sharedSphereStore.DB().Begin()
	if err != nil {
		t.Fatalf("resetSharedStores: begin sphere tx: %v", err)
	}
	defer sphereTx.Rollback() //nolint:errcheck
	for _, tbl := range []string{
		"caravan_items",
		"caravan_dependencies",
		"caravans",
		"messages",
		"escalations",
		"agents",
		"worlds",
	} {
		if _, err := sphereTx.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatalf("resetSharedStores: clear sphere table %q: %v", tbl, err)
		}
	}
	if err := sphereTx.Commit(); err != nil {
		t.Fatalf("resetSharedStores: commit sphere tx: %v", err)
	}
}

func testConfig() Config {
	return Config{
		World:          "ember",
		PatrolInterval: 50 * time.Millisecond, // Fast for tests.
		MaxRespawns:    2,
		CaptureLines:   80,
		AssessCommand:  "claude -p",
		SolHome:        os.Getenv("SOL_HOME"),
	}
}

func createWrit(t *testing.T, worldStore *store.WorldStore, id, title string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := worldStore.DB().Exec(
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'open', 3, 'test', ?, ?)`,
		id, title, now, now,
	)
	if err != nil {
		t.Fatalf("failed to create writ %q: %v", id, err)
	}
}

// --- Tests ---

func TestRegisterAgent(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.Register(); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	agent, err := sphereStore.GetAgent("ember/sentinel")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.Role != "sentinel" {
		t.Errorf("agent role = %q, want %q", agent.Role, "sentinel")
	}
	if agent.State != store.AgentIdle {
		t.Errorf("agent state = %q, want %q", agent.State, store.AgentIdle)
	}
}

func TestRegisterAgentIdempotent(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.Register(); err != nil {
		t.Fatalf("Register() first call error: %v", err)
	}
	if err := w.Register(); err != nil {
		t.Fatalf("Register() second call error: %v", err)
	}

	// Should still be the same agent.
	agent, err := sphereStore.GetAgent("ember/sentinel")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.Role != "sentinel" {
		t.Errorf("agent role = %q, want %q", agent.Role, "sentinel")
	}
}

func TestRunLifecycle(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.PatrolInterval = 100 * time.Millisecond

	w := New(cfg, sphereStore, worldStore, mock, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Poll until the sentinel registers and marks itself working.
	// Run() calls Register() then UpdateAgentState("working") synchronously
	// before the first patrol, so this normally completes in milliseconds.
	var (
		agent *store.Agent
		err   error
	)
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		agent, err = sphereStore.GetAgent("ember/sentinel")
		if err == nil && agent.State == store.AgentWorking {
			break
		}
	}
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.State != store.AgentWorking {
		t.Errorf("agent state during run = %q, want %q", agent.State, store.AgentWorking)
	}

	// Wait for context to expire.
	if err := <-done; err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Agent should be idle after shutdown.
	agent, err = sphereStore.GetAgent("ember/sentinel")
	if err != nil {
		t.Fatalf("GetAgent() after run: %v", err)
	}
	if agent.State != store.AgentIdle {
		t.Errorf("agent state after run = %q, want %q", agent.State, store.AgentIdle)
	}
}

func TestPatrolHealthyAgents(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create 3 working agents with live sessions and changing output.
	for _, name := range []string{"Toast", "Jasper", "Sage"} {
		sphereStore.CreateAgent(name, "ember", "outpost")
		sphereStore.UpdateAgentState("ember/"+name, store.AgentWorking, "sol-"+name)
		sessName := "sol-ember-" + name
		mock.alive[sessName] = true
		mock.captures[sessName] = "output for " + name
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions should have been started or stopped.
	if started := mock.getStarted(); len(started) != 0 {
		t.Errorf("expected 0 sessions started, got %d: %v", len(started), started)
	}
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped, got %d: %v", len(stopped), stopped)
	}
}

func TestPatrolSkipsWhenWorldSleeping(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Put the world to sleep.
	worldCfg := config.DefaultWorldConfig()
	worldCfg.World.Sleeping = true
	if err := config.WriteWorldConfig("ember", worldCfg); err != nil {
		t.Fatalf("WriteWorldConfig() error: %v", err)
	}

	// Create a stalled agent (working state, no live session, tethered) —
	// this would normally trigger a respawn.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	createWrit(t, worldStore, "sol-abc1234500000000", "Test task")
	if err := tether.Write("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions should have been started — patrol must skip agent work while sleeping.
	if started := mock.getStarted(); len(started) != 0 {
		t.Errorf("expected 0 sessions started (world sleeping), got %d: %v", len(started), started)
	}

	// Heartbeat should have been written so prefect does not restart the sentinel.
	hb, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat() error: %v", err)
	}
	if hb == nil {
		t.Fatal("expected heartbeat to be written, got nil")
	}
}

func TestPatrolIgnoresIdleClean(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an idle agent with no session and no tether.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	// State is idle (default), no session, no tether.

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions started or stopped.
	if started := mock.getStarted(); len(started) != 0 {
		t.Errorf("expected 0 sessions started, got %d", len(started))
	}
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped, got %d", len(stopped))
	}
}

func TestPatrolIgnoresNonMonitored(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create agents with non-monitored roles (sentinel is excluded by filter).
	sphereStore.CreateAgent("sentinel", "ember", "sentinel")

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No actions taken.
	if started := mock.getStarted(); len(started) != 0 {
		t.Errorf("expected 0 sessions started, got %d", len(started))
	}
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped, got %d", len(stopped))
	}
}

func TestSentinelIgnoresEnvoy(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an envoy agent with a dead session.
	sphereStore.CreateAgent("Scout", "ember", "envoy")
	sphereStore.UpdateAgentState("ember/Scout", store.AgentWorking, "sol-envoy123")
	// Session is NOT alive.

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions should have been started or stopped.
	if started := mock.getStarted(); len(started) != 0 {
		t.Errorf("expected 0 sessions started for envoy, got %d: %v", len(started), started)
	}
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped for envoy, got %d: %v", len(stopped), stopped)
	}
}

func TestPatrolIgnoresForgeAgents(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working forge agent with a dead session.
	// Sentinel should NOT monitor it (prefect handles forge via heartbeat).
	sphereStore.CreateAgent("forge", "ember", "forge")
	sphereStore.UpdateAgentState("ember/forge", store.AgentWorking, "")
	// Session is NOT alive — sentinel should not attempt respawn.

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions should have been started (forge is not sentinel's responsibility).
	started := mock.getStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 sessions started (forge not monitored by sentinel), got %d: %v", len(started), started)
	}
}

// --- Escalation creation tests ---

// readEvents reads all events from the logger's event file and returns those
// matching the given event type.
func readEvents(t *testing.T, solHome, eventType string) []events.Event {
	t.Helper()
	path := filepath.Join(solHome, ".events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("failed to read events file: %v", err)
	}
	var matched []events.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ev events.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type == eventType {
			matched = append(matched, ev)
		}
	}
	return matched
}

// --- Error path tests ---

// listAgentsErrorSphere wraps a real SphereStore and forces ListAgents to
// return a configured error. All other methods delegate to the embedded store.
// Used to simulate persistent infrastructure failures (e.g. sphere DB locked)
// inside patrol so we can assert the run loop surfaces the error.
type listAgentsErrorSphere struct {
	*store.SphereStore
	err error
}

func (s *listAgentsErrorSphere) ListAgents(world, state string) ([]store.Agent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.SphereStore.ListAgents(world, state)
}

// TestRunPatrolLoggedSurfacesError verifies that when patrol returns an error,
// the run-loop wrapper (used at both the initial-patrol and per-tick call
// sites) emits a sentinel_error event with action="patrol" so persistent
// infrastructure failures are observable. Regression test for CWE-391
// (Unchecked Error Condition) — see sol-974d39599fe86c88.
func TestRunPatrolLoggedSurfacesError(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	logger := events.NewLogger(cfg.SolHome)
	failing := &listAgentsErrorSphere{
		SphereStore: sphereStore,
		err:         fmt.Errorf("sphere db is locked"),
	}

	w := New(cfg, failing, worldStore, mock, logger)

	// Drive a single patrol — the wrapper must catch the error and emit it
	// rather than discarding it (the prior bug at sentinel.go:372 / :389).
	w.runPatrolLogged(context.Background())

	evts := readEvents(t, cfg.SolHome, "sentinel_error")
	if len(evts) == 0 {
		t.Fatal("expected sentinel_error event from failed patrol, got none")
	}

	var found bool
	for _, ev := range evts {
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			continue
		}
		if payload["action"] != "patrol" {
			continue
		}
		errMsg, _ := payload["error"].(string)
		if !strings.Contains(errMsg, "sphere db is locked") {
			t.Errorf("event error = %q, want it to contain %q", errMsg, "sphere db is locked")
		}
		found = true
		break
	}
	if !found {
		t.Errorf("expected sentinel_error with action=%q, got events: %+v", "patrol", evts)
	}
}

// TestRunPatrolLoggedSuccessEmitsNoError verifies that the successful patrol
// path is unchanged: no sentinel_error event is emitted when patrol returns
// nil. Guards against regressions where the wrapper accidentally logs spurious
// errors on the happy path.
func TestRunPatrolLoggedSuccessEmitsNoError(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	logger := events.NewLogger(cfg.SolHome)
	w := New(cfg, sphereStore, worldStore, mock, logger)

	// Patrol with no agents and no failure injection — should return nil.
	w.runPatrolLogged(context.Background())

	evts := readEvents(t, cfg.SolHome, "sentinel_error")
	for _, ev := range evts {
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			continue
		}
		if payload["action"] == "patrol" {
			t.Errorf("unexpected sentinel_error with action=patrol on success path: %+v", ev)
		}
	}
}
