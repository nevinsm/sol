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

	forge := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		sourceRepo:  dir,
		worktree:    filepath.Join(dir, "worktree"),
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         DefaultConfig(),
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
		{ID: "mr-001", Phase: "claimed", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111"},
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

func TestPatrolCleanMerge(t *testing.T) {
	state, worldStore, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-001", Phase: "ready", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111"},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Fix auth flow", Status: "done",
	}

	// Mock git commands for a successful merge cycle.
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git rev-parse --short HEAD", []byte("abc1234"), nil)
	cmdRunner.SetResult("git merge --squash origin/outpost/Toast/sol-aaa11111", nil, nil)
	// git diff --cached --quiet returns exit 1 = has changes
	cmdRunner.SetResult("git diff --cached --quiet", nil, fmt.Errorf("exit 1"))
	// Quality gates pass.
	gateCmd := "go test ./..."
	cmdRunner.SetResult("sh -c "+gateCmd, nil, nil)
	// Push succeeds.
	cmdRunner.SetResult("git commit -m Fix auth flow (sol-aaa11111)", nil, nil)
	cmdRunner.SetResult("git rev-parse --short HEAD~1", []byte("abc1234"), nil)
	cmdRunner.SetResult("git rev-parse --short HEAD", []byte("def5678"), nil)
	cmdRunner.SetResult("git push origin HEAD:main", nil, nil)

	ctx := context.Background()
	state.patrol(ctx)

	// Verify MR marked as merged.
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != "merged" {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}

	// Verify merges counter.
	if state.mergesTotal != 1 {
		t.Errorf("mergesTotal = %d, want 1", state.mergesTotal)
	}

	hb, _ := ReadHeartbeat("ember")
	if hb.MergesTotal != 1 {
		t.Errorf("heartbeat merges_total = %d, want 1", hb.MergesTotal)
	}
}

func TestPatrolEmptyDiff(t *testing.T) {
	state, worldStore, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-001", Phase: "ready", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111"},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "No-op change", Status: "done",
	}

	// Mock: merge succeeds but diff is empty (exit 0 from diff --cached --quiet).
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git rev-parse --short HEAD", []byte("abc1234"), nil)
	cmdRunner.SetResult("git merge --squash origin/outpost/Toast/sol-aaa11111", nil, nil)
	cmdRunner.SetResult("git diff --cached --quiet", nil, nil) // exit 0 = no diff

	ctx := context.Background()
	state.patrol(ctx)

	// Verify MR marked as merged (empty diff on first attempt -> auto-merge).
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != "merged" {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}
}

func TestPatrolEmptyDiffAfterResolution(t *testing.T) {
	state, worldStore, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()

	// MR has Attempts: 1 — after ClaimMergeRequest increments it becomes 2,
	// meaning this MR was reclaimed after conflict resolution.
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-002", Phase: "ready", WritID: "sol-bbb22222", Branch: "outpost/Toast/sol-bbb22222", Attempts: 1},
	}
	worldStore.items["sol-bbb22222"] = &store.Writ{
		ID: "sol-bbb22222", Title: "Lost change", Status: "done",
	}

	// Mock: merge succeeds but diff is empty (exit 0 from diff --cached --quiet).
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git rev-parse --short HEAD", []byte("def5678"), nil)
	cmdRunner.SetResult("git merge --squash origin/outpost/Toast/sol-bbb22222", nil, nil)
	cmdRunner.SetResult("git diff --cached --quiet", nil, nil) // exit 0 = no diff

	ctx := context.Background()
	state.patrol(ctx)

	// Verify MR marked as failed (empty diff after resolution is suspicious).
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-002"]
	worldStore.mu.Unlock()

	if phase != "failed" {
		t.Errorf("MR phase = %q, want 'failed'", phase)
	}

	// Verify SUSPECT log line was emitted.
	logData, err := os.ReadFile(state.fl.logPath)
	if err != nil {
		t.Fatalf("failed to read forge log: %v", err)
	}
	if !strings.Contains(string(logData), "SUSPECT") {
		t.Errorf("forge log missing SUSPECT entry; got:\n%s", string(logData))
	}

	// Verify an escalation was created (MarkFailed creates one).
	sphereStore := state.forge.sphereStore.(*mockSphereStore)
	sphereStore.mu.Lock()
	escCount := len(sphereStore.escalations)
	sphereStore.mu.Unlock()

	if escCount == 0 {
		t.Error("expected an escalation to be created for empty-after-resolution")
	}
}

func TestPatrolConflict(t *testing.T) {
	state, worldStore, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()

	// Set up git repo for CreateResolutionTask rev-parse.
	repoDir := t.TempDir()
	run(t, "git", "init", repoDir)
	run(t, "git", "-C", repoDir, "commit", "--allow-empty", "-m", "init")
	state.forge.worktree = repoDir

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-001", Phase: "ready", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111"},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Conflicting change", Status: "done", Priority: 2,
	}

	// Mock: merge produces conflict.
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git rev-parse --short HEAD", []byte("abc1234"), nil)
	cmdRunner.SetResult("git merge --squash origin/outpost/Toast/sol-aaa11111",
		[]byte("CONFLICT (content): Merge conflict in main.go"), fmt.Errorf("merge conflict"))
	cmdRunner.SetResult("git merge --abort", nil, nil)

	ctx := context.Background()
	state.patrol(ctx)

	// Verify a resolution task was created (MR should be blocked).
	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()

	foundResolution := false
	for _, w := range worldStore.items {
		if strings.Contains(w.Title, "Resolve merge conflicts") {
			foundResolution = true
			break
		}
	}
	if !foundResolution {
		t.Error("expected resolution task to be created")
	}
}

func TestPatrolGateFailBranchCaused(t *testing.T) {
	state, worldStore, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-001", Phase: "ready", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111"},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Bad code", Status: "done",
	}

	// Merge clean.
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git rev-parse --short HEAD", []byte("abc1234"), nil)
	cmdRunner.SetResult("git merge --squash origin/outpost/Toast/sol-aaa11111", nil, nil)
	cmdRunner.SetResult("git diff --cached --quiet", nil, fmt.Errorf("exit 1"))

	// Gates fail.
	gateCmd := "go test ./..."
	cmdRunner.SetResult("sh -c "+gateCmd, []byte("FAIL: test_auth.go:42"), fmt.Errorf("exit 1"))

	// Scotty Test: stash, base passes.
	cmdRunner.SetResult("git stash", nil, nil)
	// Base passes (no error).
	// The second invocation of "sh -c go test ./..." (on base) should pass.
	// Since our mock matches by key, we need to handle this differently.
	// Use fallback to track call order.
	gateCallCount := 0
	cmdRunner.mu.Lock()
	delete(cmdRunner.results, "sh -c "+gateCmd)
	cmdRunner.fallback = func(dir, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if key == "sh -c "+gateCmd {
			gateCallCount++
			if gateCallCount == 1 {
				return []byte("FAIL: test_auth.go:42"), fmt.Errorf("exit 1")
			}
			return nil, nil // base passes
		}
		return nil, nil
	}
	cmdRunner.mu.Unlock()

	cmdRunner.SetResult("git stash drop", nil, nil)

	ctx := context.Background()
	state.patrol(ctx)

	// Verify MR marked as failed.
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != "failed" {
		t.Errorf("MR phase = %q, want 'failed'", phase)
	}
}

func TestPatrolGateFailPreExisting(t *testing.T) {
	state, worldStore, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-001", Phase: "ready", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111"},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Good code", Status: "done",
	}

	// Merge clean.
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git rev-parse --short HEAD", []byte("abc1234"), nil)
	cmdRunner.SetResult("git merge --squash origin/outpost/Toast/sol-aaa11111", nil, nil)
	cmdRunner.SetResult("git diff --cached --quiet", nil, fmt.Errorf("exit 1"))

	// Both gate runs fail (pre-existing).
	gateCmd := "go test ./..."
	cmdRunner.SetResult("sh -c "+gateCmd, []byte("FAIL: pre-existing"), fmt.Errorf("exit 1"))
	cmdRunner.SetResult("git stash", nil, nil)
	cmdRunner.SetResult("git stash pop", nil, nil)

	// Push succeeds.
	cmdRunner.SetResult("git commit -m Good code (sol-aaa11111)", nil, nil)
	cmdRunner.SetResult("git rev-parse --short HEAD~1", []byte("abc1234"), nil)
	cmdRunner.SetResult("git rev-parse --short HEAD", []byte("def5678"), nil)
	cmdRunner.SetResult("git push origin HEAD:main", nil, nil)

	ctx := context.Background()
	state.patrol(ctx)

	// Pre-existing failure → should still be merged.
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != "merged" {
		t.Errorf("MR phase = %q, want 'merged' (pre-existing failure should proceed)", phase)
	}
}

func TestPatrolPushRejected(t *testing.T) {
	state, worldStore, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-001", Phase: "ready", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Attempts: 1},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Normal change", Status: "done",
	}

	// Merge and gates pass.
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git rev-parse --short HEAD", []byte("abc1234"), nil)
	cmdRunner.SetResult("git merge --squash origin/outpost/Toast/sol-aaa11111", nil, nil)
	cmdRunner.SetResult("git diff --cached --quiet", nil, fmt.Errorf("exit 1"))
	cmdRunner.SetResult("sh -c go test ./...", nil, nil)
	cmdRunner.SetResult("git commit -m Normal change (sol-aaa11111)", nil, nil)
	cmdRunner.SetResult("git rev-parse --short HEAD~1", []byte("abc1234"), nil)
	cmdRunner.SetResult("git rev-parse --short HEAD", []byte("def5678"), nil)
	// Push rejected.
	cmdRunner.SetResult("git push origin HEAD:main",
		[]byte("error: failed to push some refs"), fmt.Errorf("exit 1"))

	ctx := context.Background()
	state.patrol(ctx)

	// MR should be released back to ready (not merged, not failed — under max attempts).
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != "ready" {
		t.Errorf("MR phase = %q, want 'ready' (push rejected, released)", phase)
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
		{ID: "mr-001", Phase: "ready", BlockedBy: "sol-resolved1"},
	}
	worldStore.items["sol-resolved1"] = &store.Writ{ID: "sol-resolved1", Status: "closed"}

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
