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

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/nudge"
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
	cfg := DefaultPatrolConfig("")
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

// --- ClaimedAt stability tests ---

func TestHeartbeatClaimedAtStableAcrossWrites(t *testing.T) {
	state, _, _ := setupPatrolTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{
		ID:     "mr-stable-001",
		WritID: "sol-stable1111",
		Branch: "outpost/Toast/sol-stable1111",
	}

	// Simulate claiming: set claimedAt once, then write heartbeat multiple times.
	claimTime := time.Now().UTC().Add(-10 * time.Minute) // 10 minutes ago
	state.claimedAt = claimTime

	// First heartbeat write.
	state.writeHeartbeatWithMR("working", 3, mr)
	hb1, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}
	if hb1 == nil {
		t.Fatal("expected heartbeat to be written")
	}
	if hb1.ClaimedAt != claimTime.Format(time.RFC3339) {
		t.Errorf("first write: claimed_at = %q, want %q", hb1.ClaimedAt, claimTime.Format(time.RFC3339))
	}

	// Second heartbeat write (simulates periodic monitor heartbeat).
	time.Sleep(10 * time.Millisecond) // ensure wall clock advances
	state.writeHeartbeatWithMR("working", 3, mr)
	hb2, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}

	// ClaimedAt must be identical across both writes.
	if hb2.ClaimedAt != hb1.ClaimedAt {
		t.Errorf("claimed_at drifted: first=%q, second=%q", hb1.ClaimedAt, hb2.ClaimedAt)
	}

	// Timestamp (liveness) should have advanced, proving the heartbeat was rewritten.
	if !hb2.Timestamp.After(hb1.Timestamp.Add(-time.Second)) {
		t.Error("heartbeat timestamp did not advance between writes")
	}
}

func TestHeartbeatClaimedAtResetsOnNewClaim(t *testing.T) {
	state, _, _ := setupPatrolTest(t)
	defer state.fl.Close()

	mr1 := &store.MergeRequest{ID: "mr-first", WritID: "sol-first1111", Branch: "b1"}
	mr2 := &store.MergeRequest{ID: "mr-second", WritID: "sol-second222", Branch: "b2"}

	// First claim.
	firstClaim := time.Now().UTC().Add(-20 * time.Minute)
	state.claimedAt = firstClaim
	state.writeHeartbeatWithMR("working", 2, mr1)

	hb1, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}
	if hb1.ClaimedAt != firstClaim.Format(time.RFC3339) {
		t.Errorf("first claim: claimed_at = %q, want %q", hb1.ClaimedAt, firstClaim.Format(time.RFC3339))
	}

	// Simulate merge completion clearing claimedAt, then a new claim.
	state.claimedAt = time.Time{} // cleared by executeMergeSession defer

	secondClaim := time.Now().UTC()
	state.claimedAt = secondClaim
	state.writeHeartbeatWithMR("working", 1, mr2)

	hb2, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}
	if hb2.ClaimedAt != secondClaim.Format(time.RFC3339) {
		t.Errorf("second claim: claimed_at = %q, want %q", hb2.ClaimedAt, secondClaim.Format(time.RFC3339))
	}

	// The two claimed_at values must differ.
	if hb1.ClaimedAt == hb2.ClaimedAt {
		t.Error("claimed_at should differ between distinct claims")
	}
}

func TestHeartbeatClaimedAtOmittedWhenZero(t *testing.T) {
	state, _, _ := setupPatrolTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{ID: "mr-zero", WritID: "sol-zero11111", Branch: "b1"}

	// claimedAt is zero (default) — should not produce a claimed_at field.
	state.writeHeartbeatWithMR("working", 1, mr)

	hb, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}
	if hb.ClaimedAt != "" {
		t.Errorf("claimed_at should be empty when claimedAt is zero, got %q", hb.ClaimedAt)
	}
	// MR fields should still be populated.
	if hb.CurrentMR != "mr-zero" {
		t.Errorf("current_mr = %q, want 'mr-zero'", hb.CurrentMR)
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
	// back to searching the last 200 commits on origin/main.
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git log origin/main -200 --oneline --grep sol-sess1111",
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

// TestWaitLogsDroppedNonTargetNudges regression-tests the LOW-3 fix in the
// writ that closed the forge wait observability gap. Before the fix, the
// forge wait loop drained the nudge queue, kept MR_READY/FORGE_RESUMED, and
// silently discarded all other nudge types — operators debugging "I sent a
// nudge type X to the forge and got no signal" had no log to look at.
//
// The fix logs each dropped nudge via s.fl.Log("WAIT", ...). This test
// enqueues both a non-target nudge and an MR_READY nudge, calls wait, and
// asserts (a) wait returned (because MR_READY is observed) and (b) the
// dropped non-target nudge produced a "WAIT" log entry naming the type.
func TestWaitLogsDroppedNonTargetNudges(t *testing.T) {
	state, _, _ := setupPatrolTest(t)
	defer state.fl.Close()

	sessName := config.SessionName(state.forge.world, "forge")
	queueDir := config.NudgeQueueDir(sessName)
	if err := os.MkdirAll(queueDir, 0o755); err != nil {
		t.Fatalf("failed to create nudge queue dir: %v", err)
	}

	// Enqueue a non-target nudge first (older timestamp), then MR_READY
	// (newer timestamp). nudge.Drain returns messages in FIFO order, so
	// both are observed in the same Drain() pass and the wait loop must
	// log the drop AND wake on MR_READY.
	now := time.Now().UTC()
	dropMsg := nudge.Message{
		Sender:    "test-operator",
		Type:      "LOAD_BALANCE",
		Subject:   "rebalance hint",
		Priority:  "normal",
		CreatedAt: now,
	}
	if err := nudge.Enqueue(sessName, dropMsg); err != nil {
		t.Fatalf("failed to enqueue non-target nudge: %v", err)
	}
	wakeMsg := nudge.Message{
		Sender:    "test-dispatch",
		Type:      "MR_READY",
		Subject:   "queue advanced",
		Priority:  "normal",
		CreatedAt: now.Add(time.Millisecond),
	}
	if err := nudge.Enqueue(sessName, wakeMsg); err != nil {
		t.Fatalf("failed to enqueue MR_READY nudge: %v", err)
	}

	// Use a generous WaitTimeout so the test can fail loudly if wait
	// blocks instead of waking on MR_READY. The deadline still bounds
	// runtime — wait() will return as soon as it sees MR_READY.
	state.pcfg.WaitTimeout = 5 * time.Second

	start := time.Now()
	state.wait(context.Background())
	elapsed := time.Since(start)

	// wait() should have returned essentially immediately (after one
	// drain cycle) — well under WaitTimeout. Allow some slack for CI.
	if elapsed > 2*time.Second {
		t.Errorf("wait() took %v; expected near-immediate wake on MR_READY", elapsed)
	}

	logData, _ := os.ReadFile(state.fl.logPath)
	logStr := string(logData)
	if !strings.Contains(logStr, "WAIT") {
		t.Errorf("expected a WAIT log entry for dropped nudge; log:\n%s", logStr)
	}
	if !strings.Contains(logStr, "LOAD_BALANCE") {
		t.Errorf("WAIT log should name the dropped nudge type %q; log:\n%s", "LOAD_BALANCE", logStr)
	}
	if !strings.Contains(logStr, "ignored") {
		t.Errorf("WAIT log should describe the drop reason; log:\n%s", logStr)
	}
}

// TestForgeLoggerSurfacesWriteErrorToStderr regression-tests the LOW-5 fix:
// before the fix, forgeLogger.Log silently swallowed log-file write failures,
// so a disk-full or permission-flipped log file produced /dev/null logging
// with no operator signal. The fix surfaces the first write failure to
// stderr exactly once via the writeFailHook seam (which production leaves
// nil and which the runtime stderr write also fires for).
//
// We force a write error by closing the underlying *os.File while leaving
// the pointer set on the logger; subsequent WriteString calls return
// "file already closed". The hook fires only inside reportWriteErr — so a
// hook-recorded error proves the error path ran.
func TestForgeLoggerSurfacesWriteErrorToStderr(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "forge.log")

	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create log file: %v", err)
	}

	var hookErrs []error
	var hookMu sync.Mutex
	fl := &forgeLogger{
		logFile:  f,
		logPath:  logPath,
		maxBytes: 0, // disable rotation; we want the write itself to fail
		maxFiles: 0,
		writeFailHook: func(err error) {
			hookMu.Lock()
			defer hookMu.Unlock()
			hookErrs = append(hookErrs, err)
		},
	}

	// Close the underlying file so subsequent writes return an error,
	// but keep fl.logFile non-nil so Log() takes the write path.
	if err := f.Close(); err != nil {
		t.Fatalf("close log file: %v", err)
	}

	// First failing write — should fire the hook once and set the
	// loggedWriteErr flag.
	fl.Log("TEST", "first write after close")

	hookMu.Lock()
	first := len(hookErrs)
	hookMu.Unlock()
	if first != 1 {
		t.Fatalf("expected hook to fire exactly once on first failed write, got %d", first)
	}

	// Second failing write — must NOT re-fire the hook (warn-once contract).
	fl.Log("TEST", "second write after close")

	hookMu.Lock()
	second := len(hookErrs)
	gotErr := hookErrs[0]
	hookMu.Unlock()
	if second != 1 {
		t.Errorf("expected hook to fire exactly once across many failed writes, got %d", second)
	}

	if gotErr == nil {
		t.Error("hook received nil error; want write failure error")
	}

	// Idle() takes the same write path; verify it also surfaces the error
	// once when triggered fresh on a logger that has not yet warned.
	dir2 := t.TempDir()
	logPath2 := filepath.Join(dir2, "forge.log")
	f2, err := os.Create(logPath2)
	if err != nil {
		t.Fatalf("create second log file: %v", err)
	}
	var idleHookFired int
	fl2 := &forgeLogger{
		logFile:  f2,
		logPath:  logPath2,
		maxBytes: 0,
		maxFiles: 0,
		writeFailHook: func(err error) {
			idleHookFired++
		},
	}
	f2.Close()
	fl2.Idle("idle after close")
	if idleHookFired != 1 {
		t.Errorf("Idle path: expected hook to fire once on failed write, got %d", idleHookFired)
	}
}
