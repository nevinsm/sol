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

// --- Mock session manager ---

type mockSessionManager struct {
	mu           sync.Mutex
	sessions     map[string]bool   // name -> alive
	captures     map[string]string // name -> output
	injections   []string          // injected text
	startErr     error             // inject start failure
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

	worldStore := newMockWorldStore()
	sphereStore := newMockSphereStore()
	sessMgr := newMockSessionManager()
	cmdRunner := newMockCmdRunner()

	forge := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		sourceRepo:  dir,
		worktree:    worktreeDir,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		sessions:    sessMgr,
		logger:      testLogger(),
		cfg:         DefaultConfig(),
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

	_, err := state.runMergeSession(context.Background(), mr)
	if err == nil {
		t.Fatal("expected error with nil session manager")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error = %q, should contain 'not configured'", err.Error())
	}
}

func TestRunMergeSessionStartFailure(t *testing.T) {
	state, _, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	sessMgr.startErr = fmt.Errorf("tmux not available")

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	_, err := state.runMergeSession(context.Background(), mr)
	if err == nil {
		t.Fatal("expected error on session start failure")
	}
	if !strings.Contains(err.Error(), "start merge session") {
		t.Errorf("error = %q, should contain 'start merge session'", err.Error())
	}
}

func TestRunMergeSessionInjectFailure(t *testing.T) {
	state, _, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	sessMgr.injectErr = fmt.Errorf("injection failed")

	// Add writ so buildInjection works.
	state.forge.worldStore.(*mockWorldStore).items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test writ",
	}

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	_, err := state.runMergeSession(context.Background(), mr)
	if err == nil {
		t.Fatal("expected error on inject failure")
	}
	if !strings.Contains(err.Error(), "inject merge context") {
		t.Errorf("error = %q, should contain 'inject merge context'", err.Error())
	}

	// Session should have been cleaned up.
	sessionName := mergeSessionName("ember")
	if sessMgr.Exists(sessionName) {
		t.Error("session should have been stopped after inject failure")
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
		ID: "sol-aaa11111", Title: "Test change", Status: "done",
	}

	// Mock git commands for push verification.
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git branch -r --merged origin/main",
		[]byte("  origin/outpost/Toast/sol-aaa11111\n  origin/main"), nil)

	result := &ForgeResult{
		Result:  "merged",
		Summary: "Successfully merged",
	}

	s := state
	s.actOnResult(context.Background(), mr, result, 1)

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != "merged" {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}
	if s.mergesTotal != 1 {
		t.Errorf("mergesTotal = %d, want 1", s.mergesTotal)
	}
	if s.lastError != "" {
		t.Errorf("lastError = %q, want empty", s.lastError)
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
		ID: "sol-aaa11111", Title: "Bad change", Status: "done",
	}

	result := &ForgeResult{
		Result:  "failed",
		Summary: "Gate tests failed: 3 tests",
	}

	state.actOnResult(context.Background(), mr, result, 1)

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != "failed" {
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
		ID: "sol-aaa11111", Title: "Conflicting change", Status: "done", Priority: 2,
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
		ID: "sol-aaa11111", Title: "Test change", Status: "done",
	}

	// Push verification fails: branch not in merged branches.
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git branch -r --merged origin/main",
		[]byte("  origin/main"), nil) // branch not listed

	result := &ForgeResult{
		Result:  "merged",
		Summary: "Merged successfully",
	}

	state.actOnResult(context.Background(), mr, result, 1)

	// MR should be released (not marked merged) because push verification failed.
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-001"]
	worldStore.mu.Unlock()

	if phase != "ready" {
		t.Errorf("MR phase = %q, want 'ready' (released due to push verification failure)", phase)
	}
	if !strings.Contains(state.lastError, "push verification failed") {
		t.Errorf("lastError = %q, should contain 'push verification failed'", state.lastError)
	}
}

// --- Session cleanup tests ---

func TestCleanupSession(t *testing.T) {
	state, _, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	sessionName := mergeSessionName("ember")
	sessMgr.sessions[sessionName] = true

	// Create result file.
	resultPath := filepath.Join(state.forge.worktree, resultFileName)
	os.WriteFile(resultPath, []byte(`{"result":"merged"}`), 0o644)

	state.cleanupSession()

	// Session should be stopped.
	if sessMgr.Exists(sessionName) {
		t.Error("session should have been stopped")
	}

	// Result file should be removed.
	if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
		t.Error("result file should have been removed")
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

// --- buildInjection test ---

func TestBuildInjectionStub(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Fix auth flow",
	}

	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	injection := state.buildInjection(mr)

	if !strings.Contains(injection, "outpost/Toast/sol-aaa11111") {
		t.Error("injection should contain branch name")
	}
	if !strings.Contains(injection, "sol-aaa11111") {
		t.Error("injection should contain writ ID")
	}
	if !strings.Contains(injection, resultFileName) {
		t.Error("injection should reference result file")
	}
	if !strings.Contains(injection, "Fix auth flow") {
		t.Error("injection should contain writ title")
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
		{ID: "mr-001", Phase: "ready", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111"},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Fix auth flow", Status: "done",
	}

	sessionName := mergeSessionName("ember")

	// Simulate the session completing immediately by having it not exist
	// when monitorSession checks. The result file is pre-written.
	// After session starts, we mark it as not existing to simulate exit.
	sessMgr.mu.Lock()
	sessMgr.captures[sessionName] = "Done! All work complete."
	sessMgr.mu.Unlock()

	// Write result file before patrol runs.
	result := ForgeResult{
		Result:       "merged",
		Summary:      "Successfully merged branch",
		FilesChanged: []string{"auth.go"},
	}
	data, _ := json.Marshal(result)
	resultPath := filepath.Join(state.forge.worktree, resultFileName)
	os.WriteFile(resultPath, data, 0o644)

	// Mock git commands for push verification.
	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git branch -r --merged origin/main",
		[]byte("  origin/outpost/Toast/sol-aaa11111\n  origin/main"), nil)

	// We need the monitor to exit quickly. Use a context with timeout.
	// But monitorSession uses a 3-minute ticker, so we need to simulate
	// the session not existing when the first tick fires.
	// Instead, let's test the integration by calling executeMergeSession directly
	// but have the session disappear immediately after inject.

	// Trick: after session starts and inject succeeds, mark session as not alive.
	// This means when monitorSession first checks Exists(), it returns false.
	origInject := sessMgr.injectErr
	_ = origInject

	// Use a goroutine to stop the session shortly after inject.
	go func() {
		// Wait for session to be created.
		for i := 0; i < 100; i++ {
			sessMgr.mu.Lock()
			exists := sessMgr.sessions[sessionName]
			hasInjections := len(sessMgr.injections) > 0
			sessMgr.mu.Unlock()
			if exists && hasInjections {
				time.Sleep(50 * time.Millisecond)
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

	if phase != "merged" {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}
}

// --- verifyPush test ---

func TestVerifyPushSuccess(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git branch -r --merged origin/main",
		[]byte("  origin/outpost/Toast/sol-aaa11111\n  origin/main"), nil)

	mr := &store.MergeRequest{
		Branch: "outpost/Toast/sol-aaa11111",
	}

	err := state.verifyPush(context.Background(), mr)
	if err != nil {
		t.Errorf("verifyPush() error: %v", err)
	}
}

func TestVerifyPushBranchNotMerged(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git branch -r --merged origin/main",
		[]byte("  origin/main"), nil) // branch NOT in merged list

	mr := &store.MergeRequest{
		Branch: "outpost/Toast/sol-aaa11111",
	}

	err := state.verifyPush(context.Background(), mr)
	if err == nil {
		t.Fatal("expected error when branch not merged")
	}
	if !strings.Contains(err.Error(), "not found in merged branches") {
		t.Errorf("error = %q, should contain 'not found in merged branches'", err.Error())
	}
}

func TestVerifyPushFetchFails(t *testing.T) {
	state, _, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, fmt.Errorf("network error"))

	mr := &store.MergeRequest{
		Branch: "outpost/Toast/sol-aaa11111",
	}

	err := state.verifyPush(context.Background(), mr)
	if err == nil {
		t.Fatal("expected error on fetch failure")
	}
}

// --- buildMergeAssessmentPrompt test ---

func TestBuildMergeAssessmentPrompt(t *testing.T) {
	mr := &store.MergeRequest{
		ID:     "mr-001",
		WritID: "sol-aaa11111",
		Branch: "outpost/Toast/sol-aaa11111",
	}

	prompt := buildMergeAssessmentPrompt(mr, "some output here")

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

	_, err := state.runMergeSession(ctx, mr)
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
