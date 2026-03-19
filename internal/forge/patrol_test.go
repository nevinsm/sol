package forge

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

	"github.com/nevinsm/sol/internal/store"
)

// --- Mock command runner ---

type cmdCall struct {
	Dir  string
	Name string
	Args []string
}

type mockCmdRunner struct {
	mu       sync.Mutex
	calls    []cmdCall
	results  map[string]mockCmdResult // key: "name arg1 arg2..."
	fallback func(dir, name string, args ...string) ([]byte, error)
}

type mockCmdResult struct {
	output []byte
	err    error
}

func newMockCmdRunner() *mockCmdRunner {
	return &mockCmdRunner{
		results: make(map[string]mockCmdResult),
	}
}

func (m *mockCmdRunner) On(name string, args ...string) *mockCmdRunner {
	return m
}

func (m *mockCmdRunner) SetResult(key string, output []byte, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[key] = mockCmdResult{output: output, err: err}
}

func (m *mockCmdRunner) Run(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	m.mu.Lock()
	m.calls = append(m.calls, cmdCall{Dir: dir, Name: name, Args: args})
	m.mu.Unlock()

	key := name + " " + strings.Join(args, " ")

	m.mu.Lock()
	result, ok := m.results[key]
	fallback := m.fallback
	m.mu.Unlock()

	if ok {
		return result.output, result.err
	}
	if fallback != nil {
		return fallback(dir, name, args...)
	}
	return nil, nil // default: success with no output
}

func (m *mockCmdRunner) getCalls() []cmdCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]cmdCall, len(m.calls))
	copy(cp, m.calls)
	return cp
}

// --- Test helpers ---

func testPatrolConfig() PatrolConfig {
	cfg := DefaultPatrolConfig()
	cfg.WaitTimeout = 10 * time.Millisecond // don't wait in tests
	cfg.AssessCommand = "echo assessment-stub"
	cfg.AssessTimeout = 1 * time.Second
	cfg.MonitorInterval = 100 * time.Millisecond // fast monitoring in tests
	return cfg
}

func setupPatrolTest(t *testing.T) (*patrolState, *mockWorldStore, *mockCmdRunner) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create runtime dirs.
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)
	os.MkdirAll(filepath.Join(dir, "ember", "forge"), 0o755)

	worldStore := newMockWorldStore()
	sphereStore := newMockSphereStore()
	cmdRunner := newMockCmdRunner()

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main" // tests run outside world config — set explicitly
	forge := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		sourceRepo:  dir,
		worktree:    filepath.Join(dir, "worktree"),
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         forgeCfg,
	}

	pcfg := testPatrolConfig()

	// Create a minimal forge logger that discards output.
	fl := &forgeLogger{
		logPath:  filepath.Join(dir, "ember", "forge", "forge.log"),
		maxBytes: pcfg.LogMaxBytes,
		maxFiles: pcfg.LogMaxRotated,
	}
	// Open log file.
	f, _ := os.Create(fl.logPath)
	fl.logFile = f

	state := &patrolState{
		forge:    forge,
		pcfg:     pcfg,
		fl:       fl,
		eventLog: nil, // no event logging in tests
		cmd:      cmdRunner,
	}

	return state, worldStore, cmdRunner
}

// --- Patrol tests ---

func TestPatrolEmptyQueue(t *testing.T) {
	state, worldStore, _ := setupPatrolTest(t)
	defer state.fl.Close()

	worldStore.mrs = []store.MergeRequest{} // empty queue

	ctx := context.Background()
	state.patrol(ctx)

	// Should have tried to write heartbeat (status=idle).
	hb, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}
	if hb == nil {
		t.Fatal("expected heartbeat to be written")
	}
	if hb.Status != "idle" {
		t.Errorf("heartbeat status = %q, want 'idle'", hb.Status)
	}
	if hb.PatrolCount != 1 {
		t.Errorf("heartbeat patrol_count = %d, want 1", hb.PatrolCount)
	}
}

func TestPatrolPaused(t *testing.T) {
	state, _, _ := setupPatrolTest(t)
	defer state.fl.Close()

	// Set forge as paused.
	if err := SetForgePaused("ember"); err != nil {
		t.Fatalf("SetForgePaused error: %v", err)
	}

	ctx := context.Background()
	state.patrol(ctx)

	hb, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}
	if hb == nil {
		t.Fatal("expected heartbeat to be written")
	}
	if hb.Status != "paused" {
		t.Errorf("heartbeat status = %q, want 'paused'", hb.Status)
	}
}

func TestPatrolClaimRace(t *testing.T) {
	state, worldStore, _ := setupPatrolTest(t)
	defer state.fl.Close()

	// MR is ready but will be claimed by someone else.
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-001", Phase: store.MRClaimed, WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111"},
	}

	ctx := context.Background()
	state.patrol(ctx)

	// Should not have claimed anything (all already claimed).
	hb, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}
	if hb.Status != "idle" {
		t.Errorf("heartbeat status = %q, want 'idle'", hb.Status)
	}
}

// --- Heartbeat tests ---

func TestWriteReadHeartbeat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	hb := &Heartbeat{
		Timestamp:   time.Now().UTC(),
		Status:      "idle",
		PatrolCount: 42,
		QueueDepth:  3,
		MergesTotal: 7,
	}

	if err := WriteHeartbeat("myworld", hb); err != nil {
		t.Fatalf("WriteHeartbeat error: %v", err)
	}

	read, err := ReadHeartbeat("myworld")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}
	if read == nil {
		t.Fatal("expected non-nil heartbeat")
	}
	if read.PatrolCount != 42 {
		t.Errorf("patrol_count = %d, want 42", read.PatrolCount)
	}
	if read.QueueDepth != 3 {
		t.Errorf("queue_depth = %d, want 3", read.QueueDepth)
	}
	if read.MergesTotal != 7 {
		t.Errorf("merges_total = %d, want 7", read.MergesTotal)
	}
	if read.Status != "idle" {
		t.Errorf("status = %q, want 'idle'", read.Status)
	}
}

func TestReadHeartbeatNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	hb, err := ReadHeartbeat("nonexistent")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}
	if hb != nil {
		t.Error("expected nil heartbeat for nonexistent world")
	}
}

func TestHeartbeatIsStale(t *testing.T) {
	hb := &Heartbeat{
		Timestamp: time.Now().Add(-10 * time.Minute),
	}
	if !hb.IsStale(5 * time.Minute) {
		t.Error("expected heartbeat to be stale")
	}
	if hb.IsStale(15 * time.Minute) {
		t.Error("expected heartbeat to not be stale")
	}
}

// --- Log rotation tests ---

func TestLogRotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "forge.log")

	fl := &forgeLogger{
		logPath:  logPath,
		maxBytes: 100, // tiny for testing
		maxFiles: 2,
	}
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	fl.logFile = f

	// Write enough to trigger rotation.
	for i := 0; i < 20; i++ {
		fl.Log("TEST", fmt.Sprintf("line %d with some padding to fill the log", i))
	}
	fl.Close()

	// Verify rotated files exist.
	if _, err := os.Stat(logPath + ".1"); os.IsNotExist(err) {
		t.Error("expected forge.log.1 to exist after rotation")
	}

	// Current log file should still exist.
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("expected forge.log to exist after rotation")
	}
}

func TestLogRotationReopenFailure(t *testing.T) {
	dir := t.TempDir()

	// Create a real log file that the logger will use initially.
	realLogPath := filepath.Join(dir, "real.log")
	f, err := os.Create(realLogPath)
	if err != nil {
		t.Fatal(err)
	}

	// Write enough data to exceed maxBytes threshold.
	f.WriteString(strings.Repeat("x", 200) + "\n")

	fl := &forgeLogger{
		logFile:  f,
		// Set logPath to a path under a non-existent directory so reopen fails.
		logPath:  filepath.Join(dir, "nonexistent", "subdir", "forge.log"),
		maxBytes: 100, // smaller than the data already written
		maxFiles: 2,
	}

	// maybeRotate should:
	// 1. Close fl.logFile (succeeds)
	// 2. Try to rename files (fails silently — paths don't exist)
	// 3. Try to reopen at logPath (fails — parent dir doesn't exist)
	// 4. Log to stderr and set logFile=nil
	fl.mu.Lock()
	fl.maybeRotate()
	logFileIsNil := fl.logFile == nil
	fl.mu.Unlock()

	if !logFileIsNil {
		t.Error("logFile should be nil after rotation reopen failure")
	}

	// Subsequent Log() calls should not panic — stdout logging still works.
	fl.Log("TEST", "after rotation failure — should not panic")
}

// --- lastNLines test ---

func TestLastNLines(t *testing.T) {
	input := "line1\nline2\nline3\nline4\nline5"
	result := lastNLines(input, 3)
	if result != "line3\nline4\nline5" {
		t.Errorf("lastNLines = %q, want 'line3\\nline4\\nline5'", result)
	}

	// Less than n lines.
	result = lastNLines(input, 10)
	if result != input {
		t.Errorf("lastNLines should return full input when fewer than n lines")
	}
}

// --- Unblock test ---

func TestPatrolUnblocks(t *testing.T) {
	state, worldStore, _ := setupPatrolTest(t)
	defer state.fl.Close()

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-001", Phase: store.MRReady, BlockedBy: "sol-resolved1"},
	}
	worldStore.items["sol-resolved1"] = &store.Writ{ID: "sol-resolved1", Status: store.WritClosed}

	ctx := context.Background()
	state.patrol(ctx)

	// Verify unblocked.
	worldStore.mu.Lock()
	mr := worldStore.mrs[0]
	worldStore.mu.Unlock()

	if mr.BlockedBy != "" {
		t.Errorf("MR blocked_by = %q, want empty (should be unblocked)", mr.BlockedBy)
	}
}

// --- HeartbeatPath test ---

func TestHeartbeatPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	path := HeartbeatPath("myworld")
	expected := filepath.Join(dir, "myworld", "forge", "heartbeat.json")
	if path != expected {
		t.Errorf("HeartbeatPath = %q, want %q", path, expected)
	}
}

// --- LogPath test ---

func TestLogPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	path := LogPath("myworld")
	expected := filepath.Join(dir, "myworld", "forge", "forge.log")
	if path != expected {
		t.Errorf("LogPath = %q, want %q", path, expected)
	}
}

// --- Heartbeat JSON structure test ---

func TestHeartbeatJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	hb := &Heartbeat{
		Timestamp:   now,
		Status:      "idle",
		PatrolCount: 47,
		QueueDepth:  3,
		CurrentMR:   "",
		LastMerge:   now,
		MergesTotal: 12,
	}

	data, err := json.MarshalIndent(hb, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var parsed Heartbeat
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if parsed.PatrolCount != 47 {
		t.Errorf("patrol_count = %d, want 47", parsed.PatrolCount)
	}
	if parsed.QueueDepth != 3 {
		t.Errorf("queue_depth = %d, want 3", parsed.QueueDepth)
	}
	if parsed.MergesTotal != 12 {
		t.Errorf("merges_total = %d, want 12", parsed.MergesTotal)
	}
}

// --- Enriched heartbeat tests ---

func TestHeartbeatJSONWithNewFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	hb := &Heartbeat{
		Timestamp:   now,
		Status:      "working",
		PatrolCount: 10,
		QueueDepth:  2,
		CurrentMR:   "mr-001",
		CurrentWrit: "sol-aaa11111",
		ClaimedAt:   now.Format(time.RFC3339),
		LastMerge:   now,
		MergesTotal: 5,
		LastError:   "sync failed: timeout",
	}

	data, err := json.MarshalIndent(hb, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify JSON string contains expected fields.
	jsonStr := string(data)
	for _, field := range []string{"current_mr", "current_writ", "claimed_at", "last_error"} {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("JSON should contain field %q, got: %s", field, jsonStr)
		}
	}

	// Verify round-trip.
	var parsed Heartbeat
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed.CurrentMR != "mr-001" {
		t.Errorf("current_mr = %q, want 'mr-001'", parsed.CurrentMR)
	}
	if parsed.CurrentWrit != "sol-aaa11111" {
		t.Errorf("current_writ = %q, want 'sol-aaa11111'", parsed.CurrentWrit)
	}
	if parsed.ClaimedAt != now.Format(time.RFC3339) {
		t.Errorf("claimed_at = %q, want %q", parsed.ClaimedAt, now.Format(time.RFC3339))
	}
	if parsed.LastError != "sync failed: timeout" {
		t.Errorf("last_error = %q, want 'sync failed: timeout'", parsed.LastError)
	}
}

func TestHeartbeatOmitsEmptyNewFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	hb := &Heartbeat{
		Timestamp:   now,
		Status:      "idle",
		PatrolCount: 5,
		QueueDepth:  0,
		MergesTotal: 3,
	}

	data, err := json.MarshalIndent(hb, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)
	// With omitempty, empty string fields should not appear.
	for _, field := range []string{"current_mr", "current_writ", "claimed_at", "last_error"} {
		if strings.Contains(jsonStr, field) {
			t.Errorf("JSON should omit empty field %q, got: %s", field, jsonStr)
		}
	}
}

// --- Session-based patrol path tests ---

// TestPatrolSessionPathSuccessfulMerge exercises the patrol() dispatch to
// executeMergeSession (ADR-0028) when sessions are configured. A goroutine
// simulates the Claude session completing with a "merged" result file.
func TestPatrolSessionPathSuccessfulMerge(t *testing.T) {
	state, worldStore, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-sess-001", Phase: store.MRReady, WritID: "sol-sess1111", Branch: "outpost/Toast/sol-sess1111"},
	}
	worldStore.items["sol-sess1111"] = &store.Writ{
		ID: "sol-sess1111", Title: "Session merge test", Status: store.WritDone,
	}

	sessionName := mergeSessionName("ember")

	// Set up mock git commands for push verification.
	// runMergeSession calls git rev-parse origin/main to capture the pre-merge ref;
	// mock returns nil/nil (empty string), so preMergeRef="" and tryVerifyPush falls
	// back to searching all commits on origin/main (no range prefix).
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git log origin/main --oneline --grep sol-sess1111",
		[]byte("abc1234 Session merge test (sol-sess1111)"), nil)
	state.verifyRetryDelay = time.Millisecond

	// Goroutine simulates the Claude session: waits until session starts,
	// then writes a "merged" result file and exits the session.
	go func() {
		for i := 0; i < 200; i++ {
			sessMgr.mu.Lock()
			exists := sessMgr.sessions[sessionName]
			sessMgr.mu.Unlock()
			if exists {
				time.Sleep(30 * time.Millisecond)
				result := ForgeResult{
					Result:       "merged",
					Summary:      "Merged via session path",
					FilesChanged: []string{"main.go"},
				}
				data, _ := json.Marshal(result)
				os.WriteFile(filepath.Join(state.forge.worktree, resultFileName), data, 0o644)
				sessMgr.mu.Lock()
				delete(sessMgr.sessions, sessionName)
				sessMgr.mu.Unlock()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	state.patrol(ctx)

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-sess-001"]
	worldStore.mu.Unlock()

	if phase != store.MRMerged {
		t.Errorf("MR phase = %q, want 'merged' (session path through patrol)", phase)
	}
}

// TestPatrolSessionPathMaxAttemptsMarksFailed verifies that when the session
// path encounters a launch failure and MaxAttempts has been reached, patrol
// calls MarkFailed so the MR transitions to "failed".
func TestPatrolSessionPathMaxAttemptsMarksFailed(t *testing.T) {
	state, worldStore, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	// Set MaxAttempts = 1 so the first claim-then-fail triggers MarkFailed.
	state.forge.cfg.MaxAttempts = 1

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-maxatt-001", Phase: store.MRReady, WritID: "sol-maxatt1", Branch: "outpost/Toast/sol-maxatt1"},
	}
	worldStore.items["sol-maxatt1"] = &store.Writ{
		ID: "sol-maxatt1", Title: "Max attempts test", Status: store.WritDone,
	}

	// Inject a session launch failure so runMergeSession returns an error,
	// which causes executeMergeSession to call Release → MarkFailed.
	sessMgr.startErr = fmt.Errorf("simulated launch failure")

	ctx := context.Background()
	state.patrol(ctx)

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-maxatt-001"]
	worldStore.mu.Unlock()

	if phase != store.MRFailed {
		t.Errorf("MR phase = %q, want 'failed' (max attempts reached via session path)", phase)
	}
}
