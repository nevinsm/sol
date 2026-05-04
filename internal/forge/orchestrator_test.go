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

	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
)

// --- Mock session manager ---

type mockSessionManager struct {
	mu           sync.Mutex
	sessions     map[string]bool   // name -> alive
	captures     map[string]string // name -> output
	injections   []string          // injected text
	startErr     error             // inject start failure
	stopErr      error             // inject stop failure (non-"not found" errors)
	injectErr    error             // inject injection failure
	captureErr   error             // inject capture failure
}

func newMockSessionManager() *mockSessionManager {
	return &mockSessionManager{
		sessions: make(map[string]bool),
		captures: make(map[string]string),
	}
}

func (m *mockSessionManager) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.sessions[name] = true
	return nil
}

func (m *mockSessionManager) Stop(name string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sessions[name] {
		return fmt.Errorf("session %q: %w", name, session.ErrNotFound)
	}
	if m.stopErr != nil {
		return m.stopErr
	}
	delete(m.sessions, name)
	return nil
}

func (m *mockSessionManager) Exists(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[name]
}

func (m *mockSessionManager) Inject(name string, text string, submit bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.injectErr != nil {
		return m.injectErr
	}
	m.injections = append(m.injections, text)
	return nil
}

func (m *mockSessionManager) Capture(name string, lines int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.captureErr != nil {
		return "", m.captureErr
	}
	if output, ok := m.captures[name]; ok {
		return output, nil
	}
	return "", fmt.Errorf("session %q not found", name)
}

// --- Mock launcher ---

// mockLauncher creates a SessionLauncher that delegates to the mock session manager.
// This skips all startup infrastructure (world config, config dir, sphere store).
func mockLauncher(sessMgr *mockSessionManager) SessionLauncher {
	return func(cfg startup.RoleConfig, world, agent string, opts startup.LaunchOpts) (string, error) {
		sessName := "sol-" + world + "-" + agent
		if sessMgr.startErr != nil {
			return "", sessMgr.startErr
		}
		sessMgr.mu.Lock()
		sessMgr.sessions[sessName] = true
		sessMgr.mu.Unlock()
		return sessName, nil
	}
}

// --- Test helpers ---

func setupOrchestratorTest(t *testing.T) (*patrolState, *mockWorldStore, *mockSessionManager) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create runtime dirs.
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)
	os.MkdirAll(filepath.Join(dir, "ember", "forge"), 0o755)

	worktreeDir := filepath.Join(dir, "worktree")
	os.MkdirAll(worktreeDir, 0o755)
	// Create a .git entry so the cleanupSession structural health probe
	// (stat .git + rev-parse) treats the worktree as sound. Tests that
	// want to exercise the broken-worktree path remove this entry.
	os.MkdirAll(filepath.Join(worktreeDir, ".git"), 0o755)

	worldStore := newMockWorldStore()
	sphereStore := newMockSphereStore()
	sessMgr := newMockSessionManager()
	cmdRunner := newMockCmdRunner()

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main" // tests run outside world config — set explicitly
	forge := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		sourceRepo:  dir,
		worktree:    worktreeDir,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		sessions:    sessMgr,
		launcher:    mockLauncher(sessMgr),
		logger:      testLogger(),
		cfg:         forgeCfg,
		cmd:         cmdRunner,
	}

	pcfg := testPatrolConfig()

	fl := &forgeLogger{
		logPath:  filepath.Join(dir, "ember", "forge", "forge.log"),
		maxBytes: pcfg.LogMaxBytes,
		maxFiles: pcfg.LogMaxRotated,
	}
	f, _ := os.Create(fl.logPath)
	fl.logFile = f

	state := &patrolState{
		forge:    forge,
		pcfg:     pcfg,
		fl:       fl,
		eventLog: nil,
		cmd:      cmdRunner,
	}

	return state, worldStore, sessMgr
}

// --- ForgeResult parsing tests ---

func TestReadResultFileSuccess(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	result := ForgeResult{
		Result:       "merged",
		Summary:      "Successfully merged branch",
		FilesChanged: []string{"main.go", "util.go"},
		GateOutput:   "all tests pass",
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	resultPath := filepath.Join(state.forge.worktree, resultFileName)
	os.WriteFile(resultPath, data, 0o644)

	got, err := ReadResult(state.forge.worktree)
	if err != nil {
		t.Fatalf("readResultFile() error: %v", err)
	}
	if got.Result != "merged" {
		t.Errorf("result = %q, want 'merged'", got.Result)
	}
	if got.Summary != "Successfully merged branch" {
		t.Errorf("summary = %q, want 'Successfully merged branch'", got.Summary)
	}
	if len(got.FilesChanged) != 2 {
		t.Errorf("files_changed len = %d, want 2", len(got.FilesChanged))
	}
	if got.GateOutput != "all tests pass" {
		t.Errorf("gate_output = %q, want 'all tests pass'", got.GateOutput)
	}
}

func TestReadResultFileMissing(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	_, err := ReadResult(state.forge.worktree)
	if err == nil {
		t.Fatal("expected error for missing result file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, should contain 'not found'", err.Error())
	}
}

func TestReadResultFileInvalidJSON(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	resultPath := filepath.Join(state.forge.worktree, resultFileName)
	os.WriteFile(resultPath, []byte("not json"), 0o644)

	_, err := ReadResult(state.forge.worktree)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error = %q, should contain 'parse'", err.Error())
	}
}

func TestReadResultFileInvalidResult(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	result := ForgeResult{
		Result:  "unknown_value",
		Summary: "some summary",
	}
	data, _ := json.Marshal(result)
	resultPath := filepath.Join(state.forge.worktree, resultFileName)
	os.WriteFile(resultPath, data, 0o644)

	_, err := ReadResult(state.forge.worktree)
	if err == nil {
		t.Fatal("expected error for invalid result value")
	}
	if !strings.Contains(err.Error(), "invalid result") {
		t.Errorf("error = %q, should contain 'invalid result'", err.Error())
	}
}

func TestReadResultFileConflict(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	result := ForgeResult{
		Result:  "conflict",
		Summary: "Merge conflicts in main.go",
	}
	data, _ := json.Marshal(result)
	resultPath := filepath.Join(state.forge.worktree, resultFileName)
	os.WriteFile(resultPath, data, 0o644)

	got, err := ReadResult(state.forge.worktree)
	if err != nil {
		t.Fatalf("readResultFile() error: %v", err)
	}
	if got.Result != "conflict" {
		t.Errorf("result = %q, want 'conflict'", got.Result)
	}
}

func TestReadResultFileFailed(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	result := ForgeResult{
		Result:  "failed",
		Summary: "Gate tests failed",
	}
	data, _ := json.Marshal(result)
	resultPath := filepath.Join(state.forge.worktree, resultFileName)
	os.WriteFile(resultPath, data, 0o644)

	got, err := ReadResult(state.forge.worktree)
	if err != nil {
		t.Fatalf("readResultFile() error: %v", err)
	}
	if got.Result != "failed" {
		t.Errorf("result = %q, want 'failed'", got.Result)
	}
}

// --- Session lifecycle tests ---

func TestRunMergeSessionNoSessionManager(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	// Remove session manager.
	state.forge.sessions = nil

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	_, err := state.runMergeSession(context.Background(), mr, 1)
	if err == nil {
		t.Fatal("expected error with nil session manager")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error = %q, should contain 'not configured'", err.Error())
	}
}

func TestRunMergeSessionLaunchFailure(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	// Override launcher to return an error.
	state.forge.launcher = func(cfg startup.RoleConfig, world, agent string, opts startup.LaunchOpts) (string, error) {
		return "", fmt.Errorf("tmux not available")
	}

	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test writ",
	}

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	_, err := state.runMergeSession(context.Background(), mr, 1)
	if err == nil {
		t.Fatal("expected error on launch failure")
	}
	if !strings.Contains(err.Error(), "failed to launch merge session") {
		t.Errorf("error = %q, should contain 'failed to launch merge session'", err.Error())
	}
}

func TestRunMergeSessionWritInjection(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID:          "sol-aaa11111",
		Title:       "feat: add auth",
		Description: "Add authentication support.",
	}

	mr := &store.MergeRequest{
		ID:       "mr-001",
		WritID:   "sol-aaa11111",
		Branch:   "outpost/Toast/sol-aaa11111",
		Attempts: 1,
	}

	// Use a short context to cancel monitoring quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	state.runMergeSession(ctx, mr, 1)

	// Verify injection file was written with full context.
	injectionPath := filepath.Join(state.forge.worktree, injectionFileName)
	data, err := os.ReadFile(injectionPath)
	if err != nil {
		t.Fatalf("injection file not written: %v", err)
	}
	injection := string(data)

	// Should contain full injection content (not the stub).
	if !strings.Contains(injection, "### Writ Context") {
		t.Error("injection should contain Writ Context section")
	}
	if !strings.Contains(injection, "feat: add auth") {
		t.Error("injection should contain writ title")
	}
	// Description should NOT be embedded — agent fetches it via CLI.
	if strings.Contains(injection, "Add authentication support.") {
		t.Error("injection should not embed writ description inline")
	}
	if !strings.Contains(injection, "sol writ status sol-aaa11111") {
		t.Error("injection should contain sol writ status command")
	}
	if !strings.Contains(injection, "(sol-aaa11111)") {
		t.Error("injection should contain writ ID in commit instruction")
	}
}

// --- monitorSession result file fast-path test ---

func TestMonitorSessionResultFileDetected(t *testing.T) {
	state, _, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	sessionName := mergeSessionName("ember")
	sessMgr.mu.Lock()
	sessMgr.sessions[sessionName] = true
	sessMgr.captures[sessionName] = "Working on merge..."
	sessMgr.mu.Unlock()

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	// Write result file before monitor starts — simulates agent having
	// already written its result.
	result := ForgeResult{
		Result:       "merged",
		Summary:      "Successfully merged branch",
		FilesChanged: []string{"auth.go"},
	}
	data, _ := json.Marshal(result)
	os.WriteFile(filepath.Join(state.forge.worktree, resultFileName), data, 0o644)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	outcome := state.monitorSession(ctx, sessionName, mr, 1)

	if outcome != sessionCompleted {
		t.Errorf("outcome = %d, want sessionCompleted (%d)", outcome, sessionCompleted)
	}

	// Verify session was NOT assessed (no AI assessment call needed).
	// The fast path should return before reaching the output hash comparison
	// and AI assessment logic.
	logData, _ := os.ReadFile(state.fl.logPath)
	logStr := string(logData)
	if !strings.Contains(logStr, "result file detected") {
		t.Error("expected 'result file detected' log message")
	}
	if strings.Contains(logStr, "assessing") {
		t.Error("should not have reached AI assessment — result file fast path should return early")
	}
}

// TestMonitorSessionSilentStretchAssessedAtMostOnce regression-tests the LOW-1
// fix in the writ that closed the silent-assessment loop. Before the fix,
// monitorSession re-ran AI assessment every monitor-interval on a silently-
// stuck session because the lastHash assignment after a "progressing" verdict
// was a no-op (hash == lastHash was already the precondition). The result was
// unbounded AI callouts on a session that never produced new output.
//
// This test simulates the silent stretch by holding a constant capture across
// many monitor ticks and asserts assessMergeSession runs at most once. It
// then changes the capture, lets the loop tick again, and asserts the
// counter advances by exactly one — confirming a *new* quiet stretch is
// allowed exactly one assessment, not zero.
func TestMonitorSessionSilentStretchAssessedAtMostOnce(t *testing.T) {
	state, _, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	// Fast monitor interval so many ticks elapse within the test budget.
	state.pcfg.MonitorInterval = 20 * time.Millisecond
	// Quick assess timeout — assessMergeSession will spawn the configured
	// echo stub, which normalizes to "progressing".
	state.pcfg.AssessTimeout = 1 * time.Second

	sessionName := mergeSessionName("ember")
	sessMgr.mu.Lock()
	sessMgr.sessions[sessionName] = true
	sessMgr.captures[sessionName] = "stuck-output-line-1\nstuck-output-line-2"
	sessMgr.mu.Unlock()

	mr := &store.MergeRequest{
		ID:     "mr-low1",
		WritID: "sol-low1quiet01",
		Branch: "outpost/Toast/sol-low1quiet01",
	}

	// Run the monitor for a window long enough to span many ticks.
	// 400ms at 20ms interval → ~20 ticks. A regressed loop would assess
	// once per tick (excluding the baseline), producing >>1 calls.
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	state.monitorSession(ctx, sessionName, mr, 1)

	if state.assessCallCount > 1 {
		t.Errorf("assessMergeSession called %d times across a single quiet stretch; want at most 1 (LOW-1 regression)",
			state.assessCallCount)
	}
	// The "skipping repeat assessment" log should appear at least once
	// (i.e. the new code path was actually exercised). Without this, the
	// test could pass spuriously if the loop never reached the assessment
	// branch at all.
	logData, _ := os.ReadFile(state.fl.logPath)
	logStr := string(logData)
	if !strings.Contains(logStr, "skipping repeat assessment") {
		t.Errorf("expected 'skipping repeat assessment' log entry; log:\n%s", logStr)
	}

	// Now change the capture to start a new quiet stretch. The next quiet
	// settle should be assessed exactly once more (not zero, not many).
	beforeCount := state.assessCallCount
	sessMgr.mu.Lock()
	sessMgr.captures[sessionName] = "fresh-output-now"
	sessMgr.mu.Unlock()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel2()
	state.monitorSession(ctx2, sessionName, mr, 1)

	delta := state.assessCallCount - beforeCount
	if delta > 1 {
		t.Errorf("after output change a fresh quiet stretch produced %d assessments; want at most 1", delta)
	}
}

// TestAssessMergeSessionNoCommandWarnsOnce regression-tests the LOW-4 fix.
// Before the fix, an unconfigured AssessCommand caused assessMergeSession to
// silently return "progressing" with no log; combined with the LOW-1 bug this
// produced an unbounded silent wait. The fix logs a one-time warning per
// patrolState the first time the empty-AssessCommand fast-path is taken.
//
// This test calls assessMergeSession directly multiple times with no
// AssessCommand configured and asserts the warning appears at most once.
func TestAssessMergeSessionNoCommandWarnsOnce(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	// Empty AssessCommand triggers the fast-path.
	state.pcfg.AssessCommand = ""

	mr := &store.MergeRequest{
		ID:     "mr-low4",
		WritID: "sol-low4nocmd01",
		Branch: "outpost/Toast/sol-low4nocmd01",
	}

	// Call the assessment many times — simulating many monitor ticks where
	// the loop reached the assessment branch.
	for i := 0; i < 10; i++ {
		got := state.assessMergeSession(context.Background(), "sess", "stable output", mr)
		if got != "progressing" {
			t.Fatalf("call #%d: assessment = %q, want 'progressing' (no-command fast-path)", i, got)
		}
	}

	logData, _ := os.ReadFile(state.fl.logPath)
	logStr := string(logData)
	count := strings.Count(logStr, "no assess command configured")
	if count != 1 {
		t.Errorf("'no assess command configured' appeared %d times in log; want exactly 1\nlog:\n%s",
			count, logStr)
	}
	if !state.loggedNoAssessCmd {
		t.Error("loggedNoAssessCmd should be true after first no-command call")
	}
}

// --- actOnResult tests ---

func TestActOnResultMerged(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test change", Status: store.WritDone,
	}

	// Mock git commands for push verification.
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	state.preMergeRef = "deadbeef00000001"
	cmdRunner.SetResult("git log deadbeef00000001..origin/main --oneline --grep sol-aaa11111",
		[]byte("abc1234 Fix auth flow (sol-aaa11111)"), nil)

	result := &ForgeResult{
		Result:  "merged",
		Summary: "Successfully merged",
	}

	s := state
	s.actOnResult(context.Background(), mr, result, 1)

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != store.MRMerged {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}
	if s.mergesTotal != 1 {
		t.Errorf("mergesTotal = %d, want 1", s.mergesTotal)
	}
	if s.lastError != "" {
		t.Errorf("lastError = %q, want empty", s.lastError)
	}
}

func TestActOnResultNoOpMergedSkipsVerifyPush(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test change", Status: store.WritDone,
	}

	// No git commands mocked — verifyPush should NOT be called.
	result := &ForgeResult{
		Result:  "merged",
		Summary: "No-op: work already present on target branch",
		NoOp:    true,
	}

	state.actOnResult(context.Background(), mr, result, 1)

	// Verify MR was marked as merged.
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != store.MRMerged {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}
	if state.mergesTotal != 1 {
		t.Errorf("mergesTotal = %d, want 1", state.mergesTotal)
	}

	// Verify push verification was NOT called for the no-op merge. The
	// ancestor pre-check (git fetch + git merge-base --is-ancestor) DOES
	// run for no-op claims, so we look specifically for the verifyPush
	// signature (`git log ... --grep <writ>`) rather than any fetch.
	cmdRunner := state.cmd.(*mockCmdRunner)
	calls := cmdRunner.getCalls()
	for _, call := range calls {
		if call.Name != "git" || len(call.Args) == 0 || call.Args[0] != "log" {
			continue
		}
		for _, a := range call.Args {
			if a == "--grep" {
				t.Error("verifyPush should not have been called for no-op merge")
			}
		}
	}

	// Verify NO-OP log message was written.
	logData, _ := os.ReadFile(state.fl.logPath)
	logStr := string(logData)
	if !strings.Contains(logStr, "NO-OP") {
		t.Error("expected NO-OP log message for no-op merge")
	}
}

func TestActOnResultNormalMergedStillVerifiesPush(t *testing.T) {
	// This is essentially the same as TestActOnResultMerged but explicit about
	// NoOp=false to ensure normal merges still verify push.
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{
		ID:     "mr-002",
		WritID: "sol-bbb22222",
		Branch: "outpost/Toast/sol-bbb22222",
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-bbb22222"] = &store.Writ{
		ID: "sol-bbb22222", Title: "Normal change", Status: store.WritDone,
	}

	// Mock git commands for push verification — these MUST be called.
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	state.preMergeRef = "deadbeef00000099"
	cmdRunner.SetResult("git log deadbeef00000099..origin/main --oneline --grep sol-bbb22222",
		[]byte("abc1234 Normal change (sol-bbb22222)"), nil)
	cmdRunner.SetResult("git fetch origin main", nil, nil)

	result := &ForgeResult{
		Result:  "merged",
		Summary: "Successfully merged",
		NoOp:    false,
	}

	state.actOnResult(context.Background(), mr, result, 1)

	// Verify push verification was called via sourceRepo — a bare
	// `git fetch origin` (no branch arg) in the source repo, which is
	// verifyPush's signature. updateSourceRepo also fetches in sourceRepo,
	// but it uses `git fetch origin main` (branch arg), so the no-arg
	// fetch is unique to verifyPush.
	calls := cmdRunner.getCalls()
	foundFetch := false
	for _, call := range calls {
		if call.Name == "git" && len(call.Args) == 2 &&
			call.Args[0] == "fetch" && call.Args[1] == "origin" &&
			call.Dir == state.forge.sourceRepo {
			foundFetch = true
			break
		}
	}
	if !foundFetch {
		t.Error("verifyPush should have been called for normal (non-no-op) merge against sourceRepo")
	}

	// Verify MR was marked as merged.
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-002"]
	worldStore.mu.Unlock()

	if phase != store.MRMerged {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}
}

func TestActOnResultMergedUpdatesSourceRepo(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test change", Status: store.WritDone,
	}

	// Mock git commands for push verification.
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	state.preMergeRef = "deadbeef00000002"
	cmdRunner.SetResult("git log deadbeef00000002..origin/main --oneline --grep sol-aaa11111",
		[]byte("abc1234 Fix auth flow (sol-aaa11111)"), nil)
	cmdRunner.SetResult("git fetch origin main", nil, nil)

	result := &ForgeResult{
		Result:  "merged",
		Summary: "Successfully merged",
	}

	state.actOnResult(context.Background(), mr, result, 1)

	// Verify that git fetch origin main was called on the source repo (not the worktree).
	calls := cmdRunner.getCalls()
	found := false
	for _, call := range calls {
		if call.Name == "git" &&
			len(call.Args) == 3 &&
			call.Args[0] == "fetch" && call.Args[1] == "origin" && call.Args[2] == "main" &&
			call.Dir == state.forge.sourceRepo {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected git fetch origin main to be called on source repo after successful merge")
		for _, call := range calls {
			t.Logf("  call: dir=%s name=%s args=%v", call.Dir, call.Name, call.Args)
		}
	}
}

func TestUpdateSourceRepoAdvancesLocalBranch(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin main", nil, nil)
	cmdRunner.SetResult("git update-ref refs/heads/main origin/main", nil, nil)

	state.updateSourceRepo(context.Background())

	calls := cmdRunner.getCalls()

	// Verify fetch was called on the source repo.
	fetchFound := false
	for _, call := range calls {
		if call.Name == "git" &&
			len(call.Args) == 3 &&
			call.Args[0] == "fetch" && call.Args[1] == "origin" && call.Args[2] == "main" &&
			call.Dir == state.forge.sourceRepo {
			fetchFound = true
			break
		}
	}
	if !fetchFound {
		t.Error("expected git fetch origin main to be called on source repo")
		for _, call := range calls {
			t.Logf("  call: dir=%s name=%s args=%v", call.Dir, call.Name, call.Args)
		}
	}

	// Verify update-ref was called on the source repo to advance the local branch.
	updateRefFound := false
	for _, call := range calls {
		if call.Name == "git" &&
			len(call.Args) == 3 &&
			call.Args[0] == "update-ref" &&
			call.Args[1] == "refs/heads/main" &&
			call.Args[2] == "origin/main" &&
			call.Dir == state.forge.sourceRepo {
			updateRefFound = true
			break
		}
	}
	if !updateRefFound {
		t.Error("expected git update-ref refs/heads/main origin/main to be called on source repo")
		for _, call := range calls {
			t.Logf("  call: dir=%s name=%s args=%v", call.Dir, call.Name, call.Args)
		}
	}
}

func TestUpdateSourceRepoSkipsUpdateRefOnFetchFailure(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin main", nil, fmt.Errorf("network error"))

	state.updateSourceRepo(context.Background())

	calls := cmdRunner.getCalls()

	// Verify update-ref was NOT called when fetch failed.
	for _, call := range calls {
		if call.Name == "git" && len(call.Args) > 0 && call.Args[0] == "update-ref" {
			t.Errorf("git update-ref should not be called when fetch fails, got: dir=%s args=%v", call.Dir, call.Args)
		}
	}
}

func TestUpdateSourceRepoNoSourceRepo(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	state.forge.sourceRepo = ""

	cmdRunner := state.cmd.(*mockCmdRunner)

	state.updateSourceRepo(context.Background())

	calls := cmdRunner.getCalls()
	if len(calls) != 0 {
		t.Errorf("expected no git calls when sourceRepo is empty, got %d calls", len(calls))
	}
}

func TestActOnResultFailed(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Bad change", Status: store.WritDone,
	}

	result := &ForgeResult{
		Result:  "failed",
		Summary: "Gate tests failed: 3 tests",
	}

	state.actOnResult(context.Background(), mr, result, 1)

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != store.MRFailed {
		t.Errorf("MR phase = %q, want 'failed'", phase)
	}
	if !strings.Contains(state.lastError, "merge failed") {
		t.Errorf("lastError = %q, should contain 'merge failed'", state.lastError)
	}
}

func TestActOnResultConflict(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	// Set up git repo for CreateResolutionTask rev-parse.
	repoDir := t.TempDir()
	run(t, "git", "init", repoDir)
	run(t, "git", "-C", repoDir, "commit", "--allow-empty", "-m", "init")
	state.forge.worktree = repoDir

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Conflicting change", Status: store.WritDone, Priority: 2,
	}

	result := &ForgeResult{
		Result:  "conflict",
		Summary: "Merge conflicts in main.go",
	}

	state.actOnResult(context.Background(), mr, result, 1)

	// Verify resolution task was created.
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
		t.Error("expected resolution task to be created for conflict result")
	}

	// Verify MR phase is "ready" — BlockMergeRequest (called by
	// CreateResolutionTask) sets phase=ready and clears claimed_by/claimed_at.
	// No separate UpdateMergeRequestPhase call is needed.
	for _, mr := range worldStore.mrs {
		if mr.ID == "mr-001" {
			if mr.Phase != store.MRReady {
				t.Errorf("MR phase = %q, want 'ready' after successful resolution task creation", mr.Phase)
			}
			if mr.ResolutionCount != 1 {
				t.Errorf("MR resolution_count = %d, want 1 after conflict", mr.ResolutionCount)
			}
			break
		}
	}
}

// TestActOnResultConflictResolutionTaskFails verifies that when CreateResolutionTask
// fails the MR is marked failed rather than released back to "ready". Releasing to
// "ready" without a blocker task causes an infinite retry loop until MaxAttempts is
// exhausted.
func TestActOnResultConflictResolutionTaskFails(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	// Do NOT add the writ to worldStore.items — CreateResolutionTask will call
	// GetWrit, fail to find it, and return an error.
	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}
	worldStore.mrs = []store.MergeRequest{*mr}

	result := &ForgeResult{
		Result:  "conflict",
		Summary: "Merge conflicts in main.go",
	}

	state.actOnResult(context.Background(), mr, result, 1)

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()

	// MR must be marked failed — not released to "ready".
	phase := worldStore.phaseUpdates["mr-001"]
	if phase != store.MRFailed {
		t.Errorf("MR phase = %q, want 'failed' when CreateResolutionTask fails", phase)
	}
	if !strings.Contains(state.lastError, "merge conflict") {
		t.Errorf("lastError = %q, should contain 'merge conflict'", state.lastError)
	}
}

func TestActOnResultPushVerificationFails(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{
		ID:       "mr-001",
		WritID:   "sol-aaa11111",
		Branch:   "outpost/Toast/sol-aaa11111",
		Attempts: 1,
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test change", Status: store.WritDone,
	}

	// Push verification fails: writ ID not found in new commits.
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	state.preMergeRef = "deadbeef00000003"
	cmdRunner.SetResult("git log deadbeef00000003..origin/main --oneline --grep sol-aaa11111",
		[]byte(""), nil) // writ not found

	result := &ForgeResult{
		Result:  "merged",
		Summary: "Merged successfully",
	}

	state.actOnResult(context.Background(), mr, result, 1)

	// MR should be marked failed (not released) because push verification failed.
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != store.MRFailed {
		t.Errorf("MR phase = %q, want 'failed' (MarkFailed on push verification failure)", phase)
	}
	if !strings.Contains(state.lastError, "push verification failed") {
		t.Errorf("lastError = %q, should contain 'push verification failed'", state.lastError)
	}
}

func TestActOnResultPushVerificationFailureSkipsSourceRepoUpdate(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{
		ID:       "mr-001",
		WritID:   "sol-aaa11111",
		Branch:   "outpost/Toast/sol-aaa11111",
		Attempts: 1,
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test change", Status: store.WritDone,
	}

	// Push verification fails: writ ID not found in new commits.
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	state.preMergeRef = "deadbeef00000004"
	cmdRunner.SetResult("git log deadbeef00000004..origin/main --oneline --grep sol-aaa11111",
		[]byte(""), nil) // writ not found

	result := &ForgeResult{
		Result:  "merged",
		Summary: "Merged successfully",
	}

	state.actOnResult(context.Background(), mr, result, 1)

	// Verify that git fetch origin main was NOT called on the source repo.
	calls := cmdRunner.getCalls()
	for _, call := range calls {
		if call.Name == "git" &&
			len(call.Args) == 3 &&
			call.Args[0] == "fetch" && call.Args[1] == "origin" && call.Args[2] == "main" &&
			call.Dir == state.forge.sourceRepo {
			t.Error("git fetch origin main should NOT be called on source repo when push verification fails")
		}
	}
}

// --- Context cancellation during push verification ---

// TestActOnResultContextCancelDuringVerifyDefersVerification verifies that when
// context is cancelled during push verification, the MR is deferred (not marked
// failed) and can be reclaimed on the next patrol even if attempts were at
// maxAttempts. This is the regression test for the "MR stuck in ready phase
// with exhausted attempts" bug.
func TestActOnResultContextCancelDuringVerifyDefersVerification(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{
		ID:       "mr-001",
		WritID:   "sol-aaa11111",
		Branch:   "outpost/Toast/sol-aaa11111",
		Phase:    store.MRClaimed,
		Attempts: 3, // at maxAttempts
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test change", Status: store.WritDone,
	}

	// Make push verification fail so it retries, and cancel context during retry.
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, fmt.Errorf("network error"))

	result := &ForgeResult{
		Result:  "merged",
		Summary: "Successfully merged",
	}

	// Use a cancelled context to simulate shutdown during verification.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	state.actOnResult(ctx, mr, result, 1)

	// Verify DeferMergeRequestVerification was called (not UpdateMergeRequestPhase).
	worldStore.mu.Lock()
	deferred := worldStore.deferredMRs
	phase, phaseSet := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if len(deferred) != 1 || deferred[0] != "mr-001" {
		t.Fatalf("expected DeferMergeRequestVerification called with mr-001, got: %v", deferred)
	}

	// Verify phase was NOT updated via UpdateMergeRequestPhase (no failed/ready).
	if phaseSet {
		t.Errorf("UpdateMergeRequestPhase should not have been called, but phase set to %q", phase)
	}

	// Verify the mock applied the deferral correctly: phase=ready, attempts decremented.
	worldStore.mu.Lock()
	updatedMR := worldStore.mrs[0]
	worldStore.mu.Unlock()

	if updatedMR.Phase != store.MRReady {
		t.Errorf("MR phase = %q, want 'ready'", updatedMR.Phase)
	}
	if updatedMR.Attempts != 2 {
		t.Errorf("MR attempts = %d, want 2 (decremented from 3)", updatedMR.Attempts)
	}
}

// --- Session cleanup tests ---

func TestCleanupSession(t *testing.T) {
	state, _, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	sessionName := mergeSessionName("ember")
	sessMgr.sessions[sessionName] = true

	// Create result file and injection file.
	resultPath := filepath.Join(state.forge.worktree, resultFileName)
	os.WriteFile(resultPath, []byte(`{"result":"merged"}`), 0o644)
	injectionPath := filepath.Join(state.forge.worktree, injectionFileName)
	os.WriteFile(injectionPath, []byte("injection context"), 0o644)

	state.cleanupSession()

	// Session should be stopped.
	if sessMgr.Exists(sessionName) {
		t.Error("session should have been stopped")
	}

	// Result file should be removed.
	if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
		t.Error("result file should have been removed")
	}

	// Injection file should be removed.
	if _, err := os.Stat(injectionPath); !os.IsNotExist(err) {
		t.Error("injection file should have been removed")
	}
}

func TestCleanupSessionNoResultFile(t *testing.T) {
	state, _, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	sessionName := mergeSessionName("ember")
	sessMgr.sessions[sessionName] = true

	// No result file — cleanup should still work.
	state.cleanupSession()

	if sessMgr.Exists(sessionName) {
		t.Error("session should have been stopped")
	}
}

// --- normalizeAssessment tests ---

func TestNormalizeAssessment(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"progressing", "progressing"},
		{"PROGRESSING", "progressing"},
		{"stuck", "stuck"},
		{"STUCK", "stuck"},
		{"idle", "idle"},
		{"IDLE", "idle"},
		{"The agent appears to be stuck in a loop", "stuck"},
		{"Agent is idle and waiting", "idle"},
		{"Agent is progressing normally", "progressing"},
		{"unknown status", "progressing"}, // default
		{"", "progressing"},               // default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeAssessment(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeAssessment(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// --- mergeSessionName test ---

func TestMergeSessionName(t *testing.T) {
	name := mergeSessionName("ember")
	if name != "sol-ember-forge-merge" {
		t.Errorf("mergeSessionName = %q, want 'sol-ember-forge-merge'", name)
	}
}

// --- ForgeResult JSON round-trip test ---

func TestForgeResultJSON(t *testing.T) {
	result := ForgeResult{
		Result:       "merged",
		Summary:      "Clean merge of feature branch",
		FilesChanged: []string{"internal/auth/handler.go", "internal/auth/handler_test.go"},
		GateOutput:   "ok  github.com/example/app  0.5s",
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var parsed ForgeResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if parsed.Result != "merged" {
		t.Errorf("result = %q, want 'merged'", parsed.Result)
	}
	if parsed.Summary != "Clean merge of feature branch" {
		t.Errorf("summary = %q, want 'Clean merge of feature branch'", parsed.Summary)
	}
	if len(parsed.FilesChanged) != 2 {
		t.Errorf("files_changed len = %d, want 2", len(parsed.FilesChanged))
	}
	if parsed.GateOutput != "ok  github.com/example/app  0.5s" {
		t.Errorf("gate_output = %q, want test output", parsed.GateOutput)
	}
}

func TestForgeResultOmitsEmptyGateOutput(t *testing.T) {
	result := ForgeResult{
		Result:  "merged",
		Summary: "Clean merge",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)
	if strings.Contains(jsonStr, "gate_output") {
		t.Errorf("JSON should omit empty gate_output, got: %s", jsonStr)
	}
}

// --- Session-based patrol integration test ---

func TestPatrolWithSessionManager(t *testing.T) {
	state, worldStore, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-001", Phase: store.MRReady, WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111"},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Fix auth flow", Status: store.WritDone,
	}

	sessionName := mergeSessionName("ember")

	// Pre-populate captures for the session.
	sessMgr.mu.Lock()
	sessMgr.captures[sessionName] = "Done! All work complete."
	sessMgr.mu.Unlock()

	// Mock git commands for push verification.
	// Note: runMergeSession calls git rev-parse origin/main to capture the pre-merge
	// ref. The mock returns nil/nil (empty success), so preMergeRef will be "", and
	// tryVerifyPush falls back to searching the last 200 commits on origin/main.
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git log origin/main -200 --oneline --grep sol-aaa11111",
		[]byte("abc1234 Fix auth flow (sol-aaa11111)"), nil)

	// Use a goroutine to simulate the session completing: write result file,
	// then exit (delete session). The result file must be written after
	// runMergeSession calls CleanForgeResult.
	go func() {
		for i := 0; i < 100; i++ {
			sessMgr.mu.Lock()
			exists := sessMgr.sessions[sessionName]
			sessMgr.mu.Unlock()
			if exists {
				// Simulate session writing result then exiting.
				time.Sleep(50 * time.Millisecond)
				result := ForgeResult{
					Result:       "merged",
					Summary:      "Successfully merged branch",
					FilesChanged: []string{"auth.go"},
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

	state.executeMergeSession(ctx, &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}, 1)

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != store.MRMerged {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}
}

// --- verifyPush test ---

func TestVerifyPushSuccess(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond
	state.preMergeRef = "deadbeef00000005"

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git log deadbeef00000005..origin/main --oneline --grep sol-aaa11111",
		[]byte("abc1234 Fix auth flow (sol-aaa11111)"), nil)

	mr := &store.MergeRequest{
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	vr := state.verifyPush(context.Background(), mr)
	if !vr.landed {
		t.Errorf("verifyPush() landed = false, want true (err=%v)", vr.err)
	}
	if vr.err != nil {
		t.Errorf("verifyPush() err = %v, want nil", vr.err)
	}
	if vr.via != "source" {
		t.Errorf("verifyPush() via = %q, want %q", vr.via, "source")
	}
}

func TestVerifyPushWritNotFound(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond
	state.preMergeRef = "deadbeef00000006"

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git log deadbeef00000006..origin/main --oneline --grep sol-aaa11111",
		[]byte(""), nil) // writ not found in ref range

	mr := &store.MergeRequest{
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	vr := state.verifyPush(context.Background(), mr)
	if vr.landed {
		t.Fatal("expected landed=false when writ not found in commits")
	}
	if vr.err == nil {
		t.Fatal("expected error when writ not found in commits")
	}
	if !strings.Contains(vr.err.Error(), "not found in commits") {
		t.Errorf("error = %q, should contain 'not found in commits'", vr.err.Error())
	}
	if vr.via != "source" {
		t.Errorf("via = %q, want %q (primary source path took the not-found branch)", vr.via, "source")
	}
}

func TestVerifyPushFetchFails(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, fmt.Errorf("network error"))

	mr := &store.MergeRequest{
		Branch: "outpost/Toast/sol-aaa11111",
	}

	vr := state.verifyPush(context.Background(), mr)
	if vr.landed {
		t.Fatal("expected landed=false on fetch failure")
	}
	if vr.err == nil {
		t.Fatal("expected error on fetch failure")
	}
}

func TestVerifyPushRetriesThenSucceeds(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond
	state.preMergeRef = "deadbeef00000007"

	// Use a counter-based fallback to fail fetch on the first attempt
	// and succeed on subsequent attempts.
	var fetchCount int
	var mu sync.Mutex
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.fallback = func(dir, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		mu.Lock()
		defer mu.Unlock()
		if key == "git fetch origin" {
			fetchCount++
			if fetchCount <= 1 {
				return nil, fmt.Errorf("transient network error")
			}
			return nil, nil
		}
		if key == "git log deadbeef00000007..origin/main --oneline --grep sol-aaa11111" {
			return []byte("abc1234 Fix auth flow (sol-aaa11111)"), nil
		}
		return nil, nil
	}

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	vr := state.verifyPush(context.Background(), mr)
	if !vr.landed {
		t.Fatalf("verifyPush() should land after retry, got: %+v", vr)
	}
	if vr.err != nil {
		t.Errorf("verifyPush() err = %v, want nil after successful retry", vr.err)
	}
	if vr.via != "source" {
		t.Errorf("verifyPush() via = %q, want %q", vr.via, "source")
	}

	mu.Lock()
	if fetchCount < 2 {
		t.Errorf("expected at least 2 fetch attempts, got %d", fetchCount)
	}
	mu.Unlock()
}

func TestVerifyPushRespectsContextCancellation(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Second // long enough to detect cancellation

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, fmt.Errorf("network error"))

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the retry select picks it up.
	cancel()

	vr := state.verifyPush(ctx, mr)
	if vr.landed {
		t.Fatal("expected landed=false on cancelled context")
	}
	if vr.err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if vr.err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", vr.err)
	}
}

// --- buildMergeAssessmentPrompt test ---

func TestBuildMergeAssessmentPrompt(t *testing.T) {
	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	prompt := buildMergeAssessmentPrompt(mr, "some output here", 3*time.Minute)

	if !strings.Contains(prompt, "outpost/Toast/sol-aaa11111") {
		t.Error("prompt should contain branch name")
	}
	if !strings.Contains(prompt, "sol-aaa11111") {
		t.Error("prompt should contain writ ID")
	}
	if !strings.Contains(prompt, "some output here") {
		t.Error("prompt should contain captured output")
	}
	if !strings.Contains(prompt, "progressing|stuck|idle") {
		t.Error("prompt should list valid statuses")
	}
	if !strings.Contains(prompt, "3m0s") {
		t.Error("prompt should contain the monitor interval")
	}

	// Verify a non-default interval is reflected in the prompt.
	prompt10 := buildMergeAssessmentPrompt(mr, "some output here", 10*time.Minute)
	if !strings.Contains(prompt10, "10m0s") {
		t.Error("prompt should reflect configured monitor interval, not hardcoded value")
	}
}

// --- Leftover session cleanup test ---

func TestRunMergeSessionCleansUpLeftoverSession(t *testing.T) {
	state, worldStore, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test writ",
	}

	// Pre-create a leftover session.
	sessionName := mergeSessionName("ember")
	sessMgr.sessions[sessionName] = true

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	// Session start will succeed (old one cleaned up first).
	// But monitor will need to run, so use a short-lived context
	// to cancel quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := state.runMergeSession(ctx, mr, 1)
	// Should get cancelled error (not a leftover session error).
	if err == nil {
		t.Log("no error returned (session may have completed)")
	} else if !strings.Contains(err.Error(), "cancelled") {
		// Any error other than "cancelled" is acceptable as long as it's not
		// about the leftover session.
		if strings.Contains(err.Error(), "already exists") {
			t.Errorf("should have cleaned up leftover session, got: %v", err)
		}
	}

	// Verify the log mentions cleanup.
	logData, _ := os.ReadFile(state.fl.logPath)
	if !strings.Contains(string(logData), "CLEANUP") {
		t.Error("expected CLEANUP log entry for leftover session")
	}
}

// --- Cleanup and worktree verification tests ---

func TestCleanupSessionVerifiesCleanWorktree(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	cmdRunner := state.cmd.(*mockCmdRunner)
	// Mock all cleanup git commands to succeed.
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git clean -fd", nil, nil)
	cmdRunner.SetResult("git status --porcelain", nil, nil) // clean worktree

	state.cleanupSession()

	// Verify git status --porcelain was called for verification.
	calls := cmdRunner.getCalls()
	found := false
	for _, call := range calls {
		if call.Name == "git" && len(call.Args) > 0 && call.Args[0] == "status" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected git status --porcelain to be called during cleanup verification")
	}
}

func TestCleanupSessionResetsToOriginTarget(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git clean -fd", nil, nil)
	cmdRunner.SetResult("git status --porcelain", nil, nil)

	state.cleanupSession()

	// Verify git reset --hard origin/main was called (advances HEAD to target).
	calls := cmdRunner.getCalls()
	found := false
	for _, call := range calls {
		if call.Name == "git" && len(call.Args) >= 3 &&
			call.Args[0] == "reset" && call.Args[1] == "--hard" && call.Args[2] == "origin/main" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected git reset --hard origin/main to advance HEAD to target branch")
		for _, call := range calls {
			t.Logf("  call: dir=%s name=%s args=%v", call.Dir, call.Name, call.Args)
		}
	}
}

func TestCleanupSessionDirtyWorktreeLogsError(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git clean -fd", nil, nil)
	// Simulate dirty worktree after cleanup.
	cmdRunner.SetResult("git status --porcelain", []byte("M dirty-file.go\n"), nil)

	state.cleanupSession()

	// The error is logged but cleanup still completes — verify by checking
	// that result file removal was attempted (cleanup continued past the error).
	// The test succeeds if cleanupSession didn't panic.
}

// TestCleanupSessionDirtyWorktreePausesAndEscalates verifies that when the
// worktree remains dirty after reset+clean, the forge is paused and a
// high-severity escalation is created (CF-M10). Without this, the next
// runMergeSession pre-flight would fail every queued MR for the same reason
// and burn MaxAttempts on each within minutes.
func TestCleanupSessionDirtyWorktreePausesAndEscalates(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	// Sanity: forge starts unpaused.
	if IsForgePaused(state.forge.world) {
		t.Fatalf("forge should not be paused at test start")
	}
	defer ClearForgePaused(state.forge.world)

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git clean -fd", nil, nil)
	// Simulate dirty worktree that survives reset+clean (e.g. permission denied
	// on a tracked file, or a git hook re-creating files).
	cmdRunner.SetResult("git status --porcelain", []byte("M stubborn-file.go\n"), nil)

	state.cleanupSession()

	// Forge should now be paused.
	if !IsForgePaused(state.forge.world) {
		t.Error("forge should be paused after dirty-worktree cleanup")
	}

	// A high-severity escalation should have been created with the dirty
	// worktree source_ref.
	sphereStore := state.forge.sphereStore.(*mockSphereStore)
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	if len(sphereStore.escalations) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(sphereStore.escalations))
	}
	esc := sphereStore.escalations[0]
	if esc.severity != "high" {
		t.Errorf("escalation severity = %q, want 'high'", esc.severity)
	}
	if esc.source != "ember/forge" {
		t.Errorf("escalation source = %q, want 'ember/forge'", esc.source)
	}
	if !strings.Contains(esc.description, "dirty") || !strings.Contains(esc.description, "manual reset") {
		t.Errorf("escalation description should mention dirty worktree and manual reset, got: %s", esc.description)
	}
	if esc.sourceRef != "forge:ember:dirty-worktree" {
		t.Errorf("escalation sourceRef = %q, want 'forge:ember:dirty-worktree'", esc.sourceRef)
	}
}

// TestCleanupSessionDirtyWorktreeCoalescesEscalations verifies that repeated
// dirty-worktree events do not flood the escalation queue — the helper checks
// for an existing open escalation with the same source_ref and skips creating
// a duplicate.
func TestCleanupSessionDirtyWorktreeCoalescesEscalations(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	defer ClearForgePaused(state.forge.world)

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git clean -fd", nil, nil)
	cmdRunner.SetResult("git status --porcelain", []byte("M stubborn-file.go\n"), nil)

	// Two cleanups in a row (simulates two consecutive failed merge attempts).
	state.cleanupSession()
	state.cleanupSession()

	sphereStore := state.forge.sphereStore.(*mockSphereStore)
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	if len(sphereStore.escalations) != 1 {
		t.Errorf("expected 1 (coalesced) escalation, got %d", len(sphereStore.escalations))
	}
}

// TestCleanupSessionDirtyWorktreeBlocksRetries verifies the end-to-end effect
// of the pause: after a dirty-worktree cleanup, the patrol path observes the
// pause and skips merge attempts entirely, so MaxAttempts is not burned on
// queued MRs.
func TestCleanupSessionDirtyWorktreeBlocksRetries(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	defer ClearForgePaused(state.forge.world)

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git reset --hard origin/main", nil, nil)
	cmdRunner.SetResult("git clean -fd", nil, nil)
	cmdRunner.SetResult("git status --porcelain", []byte("M stubborn-file.go\n"), nil)

	state.cleanupSession()

	if !IsForgePaused(state.forge.world) {
		t.Fatal("forge should be paused after dirty-worktree cleanup")
	}

	// Queue an MR and run patrol — it should bail at the pause check without
	// touching the MR or incrementing attempts.
	mr := store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
		Phase:  store.MRReady,
	}
	worldStore.mrs = []store.MergeRequest{mr}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Title: "test"}

	state.patrol(context.Background())

	// MR should remain in ready phase, no attempt recorded.
	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()
	if phase, ok := worldStore.phaseUpdates["mr-001"]; ok {
		t.Errorf("MR phase was updated to %q while paused, expected no update", phase)
	}
}

// TestMonitorSessionHeartbeatPropagatesQueueDepth verifies the CF-M11 fix:
// the monitor heartbeat must record the queue depth passed by the caller
// (i.e. the same value patrol writes), not a hard-coded zero.
func TestMonitorSessionHeartbeatPropagatesQueueDepth(t *testing.T) {
	state, _, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	// Use a very short monitor interval so the first heartbeat fires quickly.
	state.pcfg.MonitorInterval = 50 * time.Millisecond

	sessionName := mergeSessionName("ember")
	sessMgr.mu.Lock()
	sessMgr.sessions[sessionName] = true
	sessMgr.captures[sessionName] = "Working on merge..."
	sessMgr.mu.Unlock()

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	const expectedDepth = 7

	// Cancel after the monitor has fired its initial-delay heartbeat write.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	state.monitorSession(ctx, sessionName, mr, expectedDepth)

	// Read the heartbeat back and assert the queue depth was preserved.
	hb, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat: %v", err)
	}
	if hb == nil {
		t.Fatal("heartbeat not written")
	}
	if hb.Status != "working" {
		t.Errorf("heartbeat status = %q, want 'working'", hb.Status)
	}
	if hb.QueueDepth != expectedDepth {
		t.Errorf("heartbeat queue_depth = %d, want %d (must match patrol-supplied value, not hard-coded 0)",
			hb.QueueDepth, expectedDepth)
	}
	if hb.CurrentMR != "mr-001" {
		t.Errorf("heartbeat current_mr = %q, want 'mr-001'", hb.CurrentMR)
	}
}

// TestMonitorAndPatrolHeartbeatsAgreeOnQueueDepth is the cross-cutting
// invariant test for CF-M11: a monitor heartbeat written during a merge and
// the patrol heartbeat written when the queue is observed both must record
// the same queue_depth field for a given depth value.
func TestMonitorAndPatrolHeartbeatsAgreeOnQueueDepth(t *testing.T) {
	state, _, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	const depth = 4

	// Patrol-style write.
	state.writeHeartbeat("idle", depth)
	patrolHB, err := ReadHeartbeat("ember")
	if err != nil || patrolHB == nil {
		t.Fatalf("patrol heartbeat read: hb=%v err=%v", patrolHB, err)
	}

	// Monitor-style write via monitorSession.
	state.pcfg.MonitorInterval = 50 * time.Millisecond
	sessionName := mergeSessionName("ember")
	sessMgr.mu.Lock()
	sessMgr.sessions[sessionName] = true
	sessMgr.captures[sessionName] = "merge progress"
	sessMgr.mu.Unlock()
	mr := &store.MergeRequest{
		ID:     "mr-002",
		WritID: "sol-bbb22222",
		Branch: "outpost/Toast/sol-bbb22222",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	state.monitorSession(ctx, sessionName, mr, depth)

	monitorHB, err := ReadHeartbeat("ember")
	if err != nil || monitorHB == nil {
		t.Fatalf("monitor heartbeat read: hb=%v err=%v", monitorHB, err)
	}

	if patrolHB.QueueDepth != monitorHB.QueueDepth {
		t.Errorf("patrol queue_depth=%d, monitor queue_depth=%d — monitor must agree with patrol",
			patrolHB.QueueDepth, monitorHB.QueueDepth)
	}
	if monitorHB.QueueDepth != depth {
		t.Errorf("monitor queue_depth=%d, want %d", monitorHB.QueueDepth, depth)
	}
}

func TestRunMergeSessionDirtyWorktreeReturnsError(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test writ",
	}

	cmdRunner := state.cmd.(*mockCmdRunner)
	// Simulate dirty worktree.
	cmdRunner.SetResult("git status --porcelain", []byte("M dirty-file.go\n"), nil)

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	_, err := state.runMergeSession(context.Background(), mr, 1)
	if err == nil {
		t.Fatal("expected error when worktree is dirty")
	}
	if !strings.Contains(err.Error(), "worktree not clean") {
		t.Errorf("error = %q, should contain 'worktree not clean'", err.Error())
	}
}

// --- Stop() error handling tests ---

func TestRunMergeSessionStopError(t *testing.T) {
	state, worldStore, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test writ",
	}

	// Pre-create a leftover session that will fail to stop.
	sessionName := mergeSessionName("ember")
	sessMgr.mu.Lock()
	sessMgr.sessions[sessionName] = true
	sessMgr.stopErr = fmt.Errorf("tmux server not running")
	sessMgr.mu.Unlock()

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	_, err := state.runMergeSession(context.Background(), mr, 1)
	if err == nil {
		t.Fatal("expected error when Stop() fails with unexpected error")
	}
	if !strings.Contains(err.Error(), "failed to stop leftover merge session") {
		t.Errorf("error = %q, should contain 'failed to stop leftover merge session'", err.Error())
	}
}

func TestRunMergeSessionStopNotFoundIsNonFatal(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test writ",
	}

	// No session exists — Stop() will return "not found", which is non-fatal.
	// The test should proceed past Stop() and fail at a later point (context
	// cancellation) rather than at Stop().
	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := state.runMergeSession(ctx, mr, 1)
	// Should NOT get a Stop()-related error.
	if err != nil && strings.Contains(err.Error(), "failed to stop") {
		t.Errorf("not-found Stop() error should be non-fatal, got: %v", err)
	}
}

func TestCleanForgeResultErrorAbortsSession(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test writ",
	}

	// Create a directory where the result file would be — os.Remove on a
	// non-empty directory fails, simulating a locked/unremovable file.
	resultDir := filepath.Join(state.forge.worktree, resultFileName)
	os.MkdirAll(filepath.Join(resultDir, "subdir"), 0o755)

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	_, err := state.runMergeSession(context.Background(), mr, 1)
	if err == nil {
		t.Fatal("expected error when CleanForgeResult fails")
	}
	if !strings.Contains(err.Error(), "failed to clean stale result file") {
		t.Errorf("error = %q, should contain 'failed to clean stale result file'", err.Error())
	}
}

// --- resolveGitLockPath tests ---

func TestResolveGitLockPathWorktree(t *testing.T) {
	// Simulate a git worktree add worktree where .git is a file.
	dir := t.TempDir()
	gitdir := filepath.Join(dir, "actual-gitdir")
	os.MkdirAll(gitdir, 0o755)

	// Write .git file with gitdir pointer.
	dotGit := filepath.Join(dir, ".git")
	os.WriteFile(dotGit, []byte("gitdir: "+gitdir+"\n"), 0o644)

	lockPath := resolveGitLockPath(dir)
	expected := filepath.Join(gitdir, "index.lock")
	if lockPath != expected {
		t.Errorf("resolveGitLockPath() = %q, want %q", lockPath, expected)
	}
}

func TestResolveGitLockPathWorktreeRelative(t *testing.T) {
	// Simulate a git worktree with a relative gitdir path.
	dir := t.TempDir()
	gitdir := filepath.Join(dir, ".actual-git")
	os.MkdirAll(gitdir, 0o755)

	// Write .git file with relative gitdir pointer.
	dotGit := filepath.Join(dir, ".git")
	os.WriteFile(dotGit, []byte("gitdir: .actual-git\n"), 0o644)

	lockPath := resolveGitLockPath(dir)
	expected := filepath.Join(dir, ".actual-git", "index.lock")
	if lockPath != expected {
		t.Errorf("resolveGitLockPath() = %q, want %q", lockPath, expected)
	}
}

func TestResolveGitLockPathDirectory(t *testing.T) {
	// Simulate a standalone clone where .git is a directory.
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)

	lockPath := resolveGitLockPath(dir)
	expected := filepath.Join(dir, ".git", "index.lock")
	if lockPath != expected {
		t.Errorf("resolveGitLockPath() = %q, want %q", lockPath, expected)
	}
}

func TestResolveGitLockPathNoGit(t *testing.T) {
	// No .git at all — falls back to directory assumption.
	dir := t.TempDir()
	lockPath := resolveGitLockPath(dir)
	expected := filepath.Join(dir, ".git", "index.lock")
	if lockPath != expected {
		t.Errorf("resolveGitLockPath() = %q, want %q", lockPath, expected)
	}
}

// TestVerifyPushUsesSourceRepo asserts that verifyPush runs its git fetch +
// log-grep against s.forge.sourceRepo (the managed clone), NOT against the
// forge worktree. Running against the worktree is the bug that caused
// false-negative verification failures when the worktree went structurally
// broken mid-merge-session.
func TestVerifyPushUsesSourceRepo(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond
	state.preMergeRef = "deadbeefsource01"

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git log deadbeefsource01..origin/main --oneline --grep sol-aaa11111",
		[]byte("abc1234 Fix (sol-aaa11111)"), nil)

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}
	if vr := state.verifyPush(context.Background(), mr); !vr.landed {
		t.Fatalf("verifyPush: landed=false err=%v", vr.err)
	}

	if state.forge.sourceRepo == state.forge.worktree {
		t.Fatal("test precondition: sourceRepo and worktree must differ for this assertion")
	}

	calls := cmdRunner.getCalls()
	var fetchCalls, logCalls []cmdCall
	for _, c := range calls {
		if c.Name != "git" || len(c.Args) == 0 {
			continue
		}
		if c.Args[0] == "fetch" && len(c.Args) >= 2 && c.Args[1] == "origin" && len(c.Args) == 2 {
			fetchCalls = append(fetchCalls, c)
		}
		if c.Args[0] == "log" {
			logCalls = append(logCalls, c)
		}
	}
	if len(fetchCalls) == 0 {
		t.Fatal("expected at least one verifyPush fetch call")
	}
	for _, c := range fetchCalls {
		if c.Dir != state.forge.sourceRepo {
			t.Errorf("verifyPush fetch ran in %q, want sourceRepo %q (running against worktree is the bug)",
				c.Dir, state.forge.sourceRepo)
		}
		if c.Dir == state.forge.worktree {
			t.Errorf("verifyPush fetch must not run against forge worktree %q", c.Dir)
		}
	}
	if len(logCalls) == 0 {
		t.Fatal("expected at least one verifyPush log call")
	}
	for _, c := range logCalls {
		if c.Dir != state.forge.sourceRepo {
			t.Errorf("verifyPush log ran in %q, want sourceRepo %q", c.Dir, state.forge.sourceRepo)
		}
	}
}

// TestVerifyPushFallbackToLsRemote asserts that when the primary fetch
// against sourceRepo fails, verifyPush attempts a `git ls-remote` +
// targeted shallow fetch fallback instead of immediately returning an
// error. The fallback path gives operators an authoritative view of
// origin even when the managed clone cannot refresh its remote refs.
func TestVerifyPushFallbackToLsRemote(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond

	cmdRunner := state.cmd.(*mockCmdRunner)
	// Primary fetch fails — triggers fallback.
	cmdRunner.SetResult("git fetch origin", nil, fmt.Errorf("transient network error"))
	// ls-remote succeeds with a remote HEAD.
	cmdRunner.SetResult("git ls-remote origin refs/heads/main",
		[]byte("abc1234deadbeef5555\trefs/heads/main\n"), nil)

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}
	// We don't care about the outcome — only that ls-remote was attempted.
	_ = state.verifyPush(context.Background(), mr)

	calls := cmdRunner.getCalls()
	lsRemoteCalls := 0
	for _, c := range calls {
		if c.Name != "git" || len(c.Args) == 0 || c.Args[0] != "ls-remote" {
			continue
		}
		lsRemoteCalls++
		if c.Dir != state.forge.sourceRepo {
			t.Errorf("ls-remote ran in %q, want sourceRepo %q", c.Dir, state.forge.sourceRepo)
		}
		// Expect ls-remote origin refs/heads/<target>
		foundBranchRef := false
		for _, a := range c.Args {
			if a == "refs/heads/main" {
				foundBranchRef = true
			}
		}
		if !foundBranchRef {
			t.Errorf("ls-remote args should include refs/heads/main, got %v", c.Args)
		}
	}
	if lsRemoteCalls == 0 {
		t.Error("expected ls-remote fallback to be attempted after primary fetch failure")
	}
}

// TestVerifyPushSourceRepoEmptyFails verifies that an unconfigured source
// repo is treated as a verification failure (rather than silently passing
// or falling back to the broken worktree).
func TestVerifyPushSourceRepoEmptyFails(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond
	state.forge.sourceRepo = ""

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}
	vr := state.verifyPush(context.Background(), mr)
	if vr.landed {
		t.Fatal("expected landed=false when source repo is not configured")
	}
	if vr.err == nil {
		t.Fatal("expected error when source repo is not configured")
	}
	if !strings.Contains(vr.err.Error(), "source repo not configured") {
		t.Errorf("error = %q, should mention source repo not configured", vr.err.Error())
	}
	if vr.via != "" {
		t.Errorf("via = %q, want empty (no path ran)", vr.via)
	}
}

// TestCleanupSessionDetectsBrokenWorktree asserts cleanupSession probes the
// worktree's structural health and routes to the recovery path when .git is
// missing. Recovery success must NOT raise a dirty-worktree escalation.
func TestCleanupSessionDetectsBrokenWorktree(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	defer ClearForgePaused(state.forge.world)

	// Break the worktree: remove the .git entry created by the test helper.
	if err := os.RemoveAll(filepath.Join(state.forge.worktree, ".git")); err != nil {
		t.Fatalf("failed to break worktree: %v", err)
	}

	recovered := false
	state.recoverWorktree = func() error {
		recovered = true
		return nil
	}

	// Track whether any reset/clean was attempted — the broken path must skip it.
	cmdRunner := state.cmd.(*mockCmdRunner)

	state.cleanupSession()

	if !recovered {
		t.Error("expected recoverWorktree to be called when worktree is structurally broken")
	}
	if IsForgePaused(state.forge.world) {
		t.Error("forge must NOT be paused when recreation succeeds")
	}
	sphereStore := state.forge.sphereStore.(*mockSphereStore)
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	if len(sphereStore.escalations) != 0 {
		t.Errorf("expected no escalations on successful recreation, got %d", len(sphereStore.escalations))
	}

	// Assert reset/clean were NOT attempted on a broken worktree — these
	// commands would just return exit 128 and log noise.
	for _, c := range cmdRunner.getCalls() {
		if c.Name != "git" || len(c.Args) == 0 {
			continue
		}
		switch c.Args[0] {
		case "reset", "clean":
			t.Errorf("broken-worktree path should not attempt git %s, got: dir=%s args=%v",
				c.Args[0], c.Dir, c.Args)
		}
	}
}

// TestCleanupSessionBrokenWorktreeRecreationFails verifies that when
// EnsureWorktree fails during broken-worktree recovery, the forge is paused
// and a distinct broken-worktree escalation (not dirty-worktree) is raised.
func TestCleanupSessionBrokenWorktreeRecreationFails(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	defer ClearForgePaused(state.forge.world)

	// Break the worktree.
	if err := os.RemoveAll(filepath.Join(state.forge.worktree, ".git")); err != nil {
		t.Fatalf("failed to break worktree: %v", err)
	}

	state.recoverWorktree = func() error {
		return fmt.Errorf("source repo unreachable")
	}

	state.cleanupSession()

	if !IsForgePaused(state.forge.world) {
		t.Error("forge should be paused after recreation failure")
	}

	sphereStore := state.forge.sphereStore.(*mockSphereStore)
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	if len(sphereStore.escalations) != 1 {
		t.Fatalf("expected 1 escalation after recreation failure, got %d", len(sphereStore.escalations))
	}
	esc := sphereStore.escalations[0]
	if esc.severity != "high" {
		t.Errorf("escalation severity = %q, want 'high'", esc.severity)
	}
	if esc.source != "ember/forge" {
		t.Errorf("escalation source = %q, want 'ember/forge'", esc.source)
	}
	if esc.sourceRef != "forge:ember:broken-worktree" {
		t.Errorf("escalation sourceRef = %q, want 'forge:ember:broken-worktree' (must differ from dirty-worktree)",
			esc.sourceRef)
	}
	if !strings.Contains(esc.description, "structurally broken") {
		t.Errorf("escalation description should mention 'structurally broken', got: %s", esc.description)
	}
}

// TestCleanupSessionBrokenWorktreeCoalescesEscalations verifies that
// repeated broken-worktree failures coalesce around a single open
// escalation rather than flooding the queue.
func TestCleanupSessionBrokenWorktreeCoalescesEscalations(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	defer ClearForgePaused(state.forge.world)

	if err := os.RemoveAll(filepath.Join(state.forge.worktree, ".git")); err != nil {
		t.Fatalf("failed to break worktree: %v", err)
	}

	state.recoverWorktree = func() error {
		return fmt.Errorf("source repo unreachable")
	}

	state.cleanupSession()
	state.cleanupSession()

	sphereStore := state.forge.sphereStore.(*mockSphereStore)
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	if len(sphereStore.escalations) != 1 {
		t.Errorf("expected 1 (coalesced) broken-worktree escalation, got %d", len(sphereStore.escalations))
	}
}

func TestVerifyPushFallbackUses200Commits(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond // fast retries for test

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)

	// No preMergeRef — should use -200 fallback.
	state.preMergeRef = ""
	cmdRunner.SetResult("git log origin/main -200 --oneline --grep sol-aaa11111",
		[]byte("abc1234 Fix (sol-aaa11111)"), nil)

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	vr := state.verifyPush(context.Background(), mr)
	if !vr.landed {
		t.Fatalf("verifyPush should land with 200-commit fallback: %+v", vr)
	}

	// Verify -200 was used (not -50).
	calls := cmdRunner.getCalls()
	found := false
	for _, call := range calls {
		if call.Name == "git" && len(call.Args) >= 3 &&
			call.Args[0] == "log" && call.Args[2] == "-200" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected git log with -200 flag in fallback mode")
	}
}

// TestVerifyPushReturnsStructuredResult asserts the verifyPush return type
// carries the three fields the caller depends on: landed, via, and err.
// This pins the contract in place so future additions (new verification
// paths, richer error categorisation) cannot silently drop fields.
func TestVerifyPushReturnsStructuredResult(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond
	state.preMergeRef = "deadbeefstruct01"

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git log deadbeefstruct01..origin/main --oneline --grep sol-struct01",
		[]byte("abc1234 Struct (sol-struct01)"), nil)

	mr := &store.MergeRequest{
		ID:     "mr-struct-001",
		WritID: "sol-struct01",
		Branch: "outpost/Toast/sol-struct01",
	}

	vr := state.verifyPush(context.Background(), mr)

	// Assign each field to a typed local so a silent rename/removal fails
	// to compile rather than fails at runtime.
	var landed bool = vr.landed
	var via string = vr.via
	var err error = vr.err

	if !landed {
		t.Errorf("landed = false, want true")
	}
	if via != "source" {
		t.Errorf("via = %q, want %q", via, "source")
	}
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// TestUpdateSourceRepoRunsWhenVerifiedViaSource mocks the primary (source)
// verification path to succeed and asserts updateSourceRepo runs, advancing
// the managed repo's local ref via `git fetch origin <branch>` +
// `git update-ref`.
func TestUpdateSourceRepoRunsWhenVerifiedViaSource(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond
	state.preMergeRef = "deadbeefsource99"

	mr := &store.MergeRequest{
		ID:     "mr-src-001",
		WritID: "sol-src00001",
		Branch: "outpost/Toast/sol-src00001",
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-src00001"] = &store.Writ{
		ID: "sol-src00001", Title: "Src path", Status: store.WritDone,
	}

	cmdRunner := state.cmd.(*mockCmdRunner)
	// Primary source path succeeds on first attempt.
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git log deadbeefsource99..origin/main --oneline --grep sol-src00001",
		[]byte("abc1234 Src path (sol-src00001)"), nil)
	// updateSourceRepo's fetch + update-ref — mocked so we can watch for them.
	cmdRunner.SetResult("git fetch origin main", nil, nil)
	cmdRunner.SetResult("git update-ref refs/heads/main origin/main", nil, nil)

	result := &ForgeResult{Result: "merged", Summary: "merged via source"}
	state.actOnResult(context.Background(), mr, result, 1)

	// Assert updateSourceRepo was called: its distinct signature is
	// `git fetch origin main` (branch-scoped fetch, not verifyPush's
	// bare `git fetch origin`) followed by `git update-ref` in sourceRepo.
	calls := cmdRunner.getCalls()
	fetchBranchSeen := false
	updateRefSeen := false
	for _, c := range calls {
		if c.Name != "git" || len(c.Args) == 0 {
			continue
		}
		if c.Args[0] == "fetch" && len(c.Args) == 3 &&
			c.Args[1] == "origin" && c.Args[2] == "main" &&
			c.Dir == state.forge.sourceRepo {
			fetchBranchSeen = true
		}
		if c.Args[0] == "update-ref" && len(c.Args) == 3 &&
			c.Args[1] == "refs/heads/main" && c.Args[2] == "origin/main" &&
			c.Dir == state.forge.sourceRepo {
			updateRefSeen = true
		}
	}
	if !fetchBranchSeen {
		t.Error("updateSourceRepo's `git fetch origin main` was not called — source-path verification should still advance managed ref")
	}
	if !updateRefSeen {
		t.Error("updateSourceRepo's `git update-ref` was not called — source-path verification should still advance managed ref")
	}

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-src-001"]
	worldStore.mu.Unlock()
	if phase != store.MRMerged {
		t.Errorf("MR phase = %q, want %q", phase, store.MRMerged)
	}
}

// TestUpdateSourceRepoRunsWhenVerifiedViaLsRemote mocks the primary fetch
// to fail so verification falls back to ls-remote + shallow fetch, then
// asserts updateSourceRepo is STILL invoked. The whole point of the
// refactor: any authoritative confirmation — regardless of path — must
// advance the managed repo's local ref.
func TestUpdateSourceRepoRunsWhenVerifiedViaLsRemote(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond
	state.preMergeRef = "deadbeeflsrem001"

	mr := &store.MergeRequest{
		ID:     "mr-lsr-001",
		WritID: "sol-lsr00001",
		Branch: "outpost/Toast/sol-lsr00001",
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-lsr00001"] = &store.Writ{
		ID: "sol-lsr00001", Title: "Ls-remote path", Status: store.WritDone,
	}

	remoteHead := "abc1234deadbeef9999"

	cmdRunner := state.cmd.(*mockCmdRunner)
	// Primary fetch fails every attempt so retries also take the fallback.
	cmdRunner.SetResult("git fetch origin", nil, fmt.Errorf("network down"))
	// ls-remote returns a remote HEAD.
	cmdRunner.SetResult("git ls-remote origin refs/heads/main",
		[]byte(remoteHead+"\trefs/heads/main\n"), nil)
	// Shallow fetch of that commit succeeds.
	cmdRunner.SetResult("git fetch --depth=200 origin "+remoteHead, nil, nil)
	// Log-grep against the discovered commit finds the writ.
	cmdRunner.SetResult("git log deadbeeflsrem001.."+remoteHead+" --oneline --grep sol-lsr00001",
		[]byte("abc1234 Ls-remote path (sol-lsr00001)"), nil)
	// updateSourceRepo's fetch is a branch-scoped fetch; even though the
	// bare `git fetch origin` is mocked to fail, `git fetch origin main`
	// has its own explicit result keyed on the full arg list.
	cmdRunner.SetResult("git fetch origin main", nil, nil)
	cmdRunner.SetResult("git update-ref refs/heads/main origin/main", nil, nil)

	result := &ForgeResult{Result: "merged", Summary: "merged via ls-remote"}
	state.actOnResult(context.Background(), mr, result, 1)

	calls := cmdRunner.getCalls()

	// Sanity: ls-remote fallback was actually exercised.
	lsRemoteSeen := false
	for _, c := range calls {
		if c.Name == "git" && len(c.Args) >= 1 && c.Args[0] == "ls-remote" {
			lsRemoteSeen = true
		}
	}
	if !lsRemoteSeen {
		t.Fatal("ls-remote fallback was not exercised — test precondition failed")
	}

	// The real assertion: updateSourceRepo still ran.
	fetchBranchSeen := false
	updateRefSeen := false
	for _, c := range calls {
		if c.Name != "git" || len(c.Args) == 0 {
			continue
		}
		if c.Args[0] == "fetch" && len(c.Args) == 3 &&
			c.Args[1] == "origin" && c.Args[2] == "main" &&
			c.Dir == state.forge.sourceRepo {
			fetchBranchSeen = true
		}
		if c.Args[0] == "update-ref" && len(c.Args) == 3 &&
			c.Args[1] == "refs/heads/main" && c.Args[2] == "origin/main" &&
			c.Dir == state.forge.sourceRepo {
			updateRefSeen = true
		}
	}
	if !fetchBranchSeen {
		t.Error("updateSourceRepo's `git fetch origin main` was not called after ls-remote-path confirmation — managed ref did not advance")
	}
	if !updateRefSeen {
		t.Error("updateSourceRepo's `git update-ref` was not called after ls-remote-path confirmation — managed ref did not advance")
	}

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-lsr-001"]
	worldStore.mu.Unlock()
	if phase != store.MRMerged {
		t.Errorf("MR phase = %q, want %q", phase, store.MRMerged)
	}
}

// TestUpdateSourceRepoSkippedWhenNotLanded asserts that when every
// verification path reports not-landed, updateSourceRepo is NOT called,
// the MR is marked failed, and the writ is reopened. This is the
// failure-path counterpart — advancing the local ref on a failed push
// would misrepresent origin state.
func TestUpdateSourceRepoSkippedWhenNotLanded(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()
	state.verifyRetryDelay = time.Millisecond
	state.preMergeRef = "deadbeefnoland01"

	mr := &store.MergeRequest{
		ID:       "mr-nol-001",
		WritID:   "sol-nol00001",
		Branch:   "outpost/Toast/sol-nol00001",
		Attempts: 1,
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-nol00001"] = &store.Writ{
		ID: "sol-nol00001", Title: "Not landed", Status: store.WritDone,
	}

	cmdRunner := state.cmd.(*mockCmdRunner)
	// Primary fetch succeeds; log-grep returns no matches → not landed.
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git log deadbeefnoland01..origin/main --oneline --grep sol-nol00001",
		[]byte(""), nil)

	result := &ForgeResult{Result: "merged", Summary: "claims merged but push missing"}
	state.actOnResult(context.Background(), mr, result, 1)

	// Assert updateSourceRepo did NOT run. `git update-ref` is unique to
	// updateSourceRepo in this flow, and branch-scoped `git fetch origin main`
	// is also only issued by updateSourceRepo (verifyPush uses bare
	// `git fetch origin`).
	for _, c := range cmdRunner.getCalls() {
		if c.Name != "git" || len(c.Args) == 0 {
			continue
		}
		if c.Args[0] == "update-ref" {
			t.Errorf("update-ref must NOT be called when verification did not confirm the push: args=%v", c.Args)
		}
		if c.Args[0] == "fetch" && len(c.Args) == 3 &&
			c.Args[1] == "origin" && c.Args[2] == "main" {
			t.Errorf("branch-scoped `git fetch origin main` (updateSourceRepo) must NOT be called when verification did not confirm the push")
		}
	}

	// MR marked failed, writ reopened.
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-nol-001"]
	writ := worldStore.items["sol-nol00001"]
	worldStore.mu.Unlock()

	if phase != store.MRFailed {
		t.Errorf("MR phase = %q, want %q (verification failure path)", phase, store.MRFailed)
	}
	if writ == nil || writ.Status != store.WritOpen {
		t.Errorf("writ should be reopened after failed verification; got status=%v", writ)
	}
	if !strings.Contains(state.lastError, "push verification failed") {
		t.Errorf("lastError = %q, should contain 'push verification failed'", state.lastError)
	}
}
