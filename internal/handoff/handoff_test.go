package handoff

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/tether"
)

func setupSolHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	return dir
}

func TestCapture(t *testing.T) {
	solHome := setupSolHome(t)

	// Set up tether file.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Set up workflow state.
	wfDir := filepath.Join(solHome, "ember", "outposts", "Toast", ".workflow")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("failed to create workflow dir: %v", err)
	}
	stateJSON := `{"current_step":"implement","completed":["plan"],"status":"running","started_at":"2026-02-27T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(wfDir, "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("failed to write workflow state: %v", err)
	}
	// Minimal instance manifest for step counting.
	instanceJSON := `{"formula":"default-work","work_item_id":"sol-abc12345","variables":{},"instantiated_at":"2026-02-27T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(wfDir, "manifest.json"), []byte(instanceJSON), 0o644); err != nil {
		t.Fatalf("failed to write workflow instance: %v", err)
	}

	// Mock session capture.
	mockCapture := func(name string, lines int) (string, error) {
		return "$ make test\nAll tests passed.\n$", nil
	}

	// Mock git log.
	mockGitLog := func(dir string, count int) ([]string, error) {
		return []string{"abc1234 feat: add login form", "def5678 test: add login tests"}, nil
	}

	state, err := Capture(CaptureOpts{
		World:     "ember",
		AgentName: "Toast",
		Role:      "agent",
		Summary:   "Implemented login form. Tests passing.",
	}, mockCapture, mockGitLog)

	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if state.WorkItemID != "sol-abc12345" {
		t.Errorf("expected WorkItemID sol-abc12345, got %q", state.WorkItemID)
	}
	if state.AgentName != "Toast" {
		t.Errorf("expected AgentName Toast, got %q", state.AgentName)
	}
	if state.World != "ember" {
		t.Errorf("expected World ember, got %q", state.World)
	}
	if state.PreviousSession != "sol-ember-Toast" {
		t.Errorf("expected PreviousSession sol-ember-Toast, got %q", state.PreviousSession)
	}
	if state.Summary != "Implemented login form. Tests passing." {
		t.Errorf("expected summary to match, got %q", state.Summary)
	}
	if !strings.Contains(state.RecentOutput, "All tests passed") {
		t.Errorf("expected recent output to contain test output, got %q", state.RecentOutput)
	}
	if len(state.RecentCommits) != 2 {
		t.Errorf("expected 2 recent commits, got %d", len(state.RecentCommits))
	}
	if state.WorkflowStep != "implement" {
		t.Errorf("expected workflow step 'implement', got %q", state.WorkflowStep)
	}
}

func TestCaptureUsesExplicitWorktreeDir(t *testing.T) {
	setupSolHome(t)

	// Set up tether for an envoy.
	if err := tether.Write("ember", "Alice", "sol-envoy12345", "envoy"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	envoyWorktree := "/custom/envoys/Alice/worktree"

	// Track which directory gitLog receives.
	var capturedDir string
	mockGitLog := func(dir string, count int) ([]string, error) {
		capturedDir = dir
		return []string{"abc1234 feat: envoy work"}, nil
	}

	_, err := Capture(CaptureOpts{
		World:       "ember",
		AgentName:   "Alice",
		Role:        "envoy",
		WorktreeDir: envoyWorktree,
	}, nil, mockGitLog)

	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if capturedDir != envoyWorktree {
		t.Errorf("expected gitLog dir %q, got %q", envoyWorktree, capturedDir)
	}
}

func TestCaptureNoWorkflow(t *testing.T) {
	setupSolHome(t)

	// Set up tether file only, no workflow.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	state, err := Capture(CaptureOpts{
		World:     "ember",
		AgentName: "Toast",
		Role:      "agent",
		Summary:   "Working on it.",
	}, nil, nil)

	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if state.WorkflowStep != "" {
		t.Errorf("expected empty workflow step, got %q", state.WorkflowStep)
	}
	if state.WorkflowProgress != "" {
		t.Errorf("expected empty workflow progress, got %q", state.WorkflowProgress)
	}
}

func TestCaptureNoSummary(t *testing.T) {
	setupSolHome(t)

	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	mockGitLog := func(dir string, count int) ([]string, error) {
		return []string{"abc1234 feat: add login form"}, nil
	}

	state, err := Capture(CaptureOpts{
		World:     "ember",
		AgentName: "Toast",
		Role:      "agent",
	}, nil, mockGitLog)

	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	// Auto-generated summary should contain agent name and work item.
	if !strings.Contains(state.Summary, "Toast") {
		t.Errorf("auto-generated summary missing agent name: %q", state.Summary)
	}
	if !strings.Contains(state.Summary, "sol-abc12345") {
		t.Errorf("auto-generated summary missing work item ID: %q", state.Summary)
	}
	if !strings.Contains(state.Summary, "abc1234") {
		t.Errorf("auto-generated summary missing last commit: %q", state.Summary)
	}
}

func TestWriteAndRead(t *testing.T) {
	setupSolHome(t)

	original := &State{
		WorkItemID:       "sol-abc12345",
		AgentName:        "Toast",
		World:            "ember",
		Role:             "agent",
		PreviousSession:  "sol-ember-Toast",
		Summary:          "Implemented login form.",
		RecentOutput:     "All tests passed.\n$",
		RecentCommits:    []string{"abc1234 feat: add login form", "def5678 test: tests"},
		WorkflowStep:     "implement",
		WorkflowProgress: "1/3 complete",
		HandedOffAt:      time.Date(2026, 2, 27, 10, 30, 0, 0, time.UTC),
		GitStatus:        " M hello.go\n?? new.go",
		GitStash:         "stash@{0}: WIP on main: abc1234 feat",
		DiffStat:         " hello.go | 2 +-\n 1 file changed",
		StepDescription:  "Implement the login form",
	}

	if err := Write(original); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify JSON on disk is valid.
	data, err := os.ReadFile(HandoffPath("ember", "Toast", "agent"))
	if err != nil {
		t.Fatalf("failed to read handoff file: %v", err)
	}
	var diskState State
	if err := json.Unmarshal(data, &diskState); err != nil {
		t.Fatalf("invalid JSON on disk: %v", err)
	}

	// Read back.
	read, err := Read("ember", "Toast", "agent")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if read.WorkItemID != original.WorkItemID {
		t.Errorf("WorkItemID mismatch: got %q, want %q", read.WorkItemID, original.WorkItemID)
	}
	if read.AgentName != original.AgentName {
		t.Errorf("AgentName mismatch: got %q, want %q", read.AgentName, original.AgentName)
	}
	if read.Summary != original.Summary {
		t.Errorf("Summary mismatch: got %q, want %q", read.Summary, original.Summary)
	}
	if len(read.RecentCommits) != len(original.RecentCommits) {
		t.Errorf("RecentCommits length mismatch: got %d, want %d", len(read.RecentCommits), len(original.RecentCommits))
	}
	if read.WorkflowStep != original.WorkflowStep {
		t.Errorf("WorkflowStep mismatch: got %q, want %q", read.WorkflowStep, original.WorkflowStep)
	}
	if read.WorkflowProgress != original.WorkflowProgress {
		t.Errorf("WorkflowProgress mismatch: got %q, want %q", read.WorkflowProgress, original.WorkflowProgress)
	}
	if read.GitStatus != original.GitStatus {
		t.Errorf("GitStatus mismatch: got %q, want %q", read.GitStatus, original.GitStatus)
	}
	if read.GitStash != original.GitStash {
		t.Errorf("GitStash mismatch: got %q, want %q", read.GitStash, original.GitStash)
	}
	if read.DiffStat != original.DiffStat {
		t.Errorf("DiffStat mismatch: got %q, want %q", read.DiffStat, original.DiffStat)
	}
	if read.StepDescription != original.StepDescription {
		t.Errorf("StepDescription mismatch: got %q, want %q", read.StepDescription, original.StepDescription)
	}
}

func TestReadNoFile(t *testing.T) {
	setupSolHome(t)

	state, err := Read("ember", "Toast", "agent")
	if err != nil {
		t.Fatalf("Read returned error for missing file: %v", err)
	}
	if state != nil {
		t.Errorf("expected nil state for missing file, got %+v", state)
	}
}

func TestRemove(t *testing.T) {
	setupSolHome(t)

	// Write then remove.
	state := &State{
		WorkItemID: "sol-abc12345",
		AgentName:  "Toast",
		World:      "ember",
		Role:       "agent",
		Summary:    "test",
	}
	if err := Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := Remove("ember", "Toast", "agent"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify file is gone.
	read, err := Read("ember", "Toast", "agent")
	if err != nil {
		t.Fatalf("Read failed after remove: %v", err)
	}
	if read != nil {
		t.Error("expected nil state after remove")
	}

	// Remove non-existent — no error.
	if err := Remove("ember", "Toast", "agent"); err != nil {
		t.Fatalf("Remove non-existent returned error: %v", err)
	}
}

func TestHasHandoff(t *testing.T) {
	setupSolHome(t)

	// No file.
	if HasHandoff("ember", "Toast", "agent") {
		t.Error("expected HasHandoff to be false with no file")
	}

	// Write handoff.
	state := &State{
		WorkItemID: "sol-abc12345",
		AgentName:  "Toast",
		World:      "ember",
		Role:       "agent",
		Summary:    "test",
	}
	if err := Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if !HasHandoff("ember", "Toast", "agent") {
		t.Error("expected HasHandoff to be true after write")
	}
}

func TestGitLog(t *testing.T) {
	// Create temp git repo with commits.
	dir := t.TempDir()

	cmd := exec.Command("git", "-C", dir, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %s: %v", out, err)
	}
	cmd = exec.Command("git", "-C", dir, "config", "user.email", "test@test.com")
	cmd.Run()
	cmd = exec.Command("git", "-C", dir, "config", "user.name", "Test")
	cmd.Run()

	// Create 5 commits.
	for i := 1; i <= 5; i++ {
		msg := "commit " + string(rune('A'-1+i))
		cmd = exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", msg)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit failed: %s: %v", out, err)
		}
	}

	// Request 3 most recent.
	commits, err := GitLog(dir, 3)
	if err != nil {
		t.Fatalf("GitLog failed: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("expected 3 commits, got %d: %v", len(commits), commits)
	}

	// Most recent should be last commit.
	if !strings.Contains(commits[0], "commit E") {
		t.Errorf("expected first commit to contain 'commit E', got %q", commits[0])
	}

	// Empty repo (non-existent dir).
	commits, err = GitLog("/nonexistent/dir", 3)
	if err != nil {
		t.Fatalf("GitLog for nonexistent dir returned error: %v", err)
	}
	if len(commits) != 0 {
		t.Errorf("expected empty slice for nonexistent dir, got %v", commits)
	}
}

// --- Mock types for Exec test ---

type mockSessionMgr struct {
	captureResult  string
	captureErr     error
	stopped        []string
	started        []startCall
	cycled         []startCall
	cycleErr       error
	exists         bool
	injected       []injectCall
	injectErr      error
	captureResults []string // sequential capture results (cycles through them)
	captureIndex   int
}

type startCall struct {
	Name, Workdir, Cmd string
	Env                map[string]string
	Role, World        string
}

type injectCall struct {
	Name, Text string
	Submit     bool
}

func (m *mockSessionMgr) Exists(name string) bool {
	return m.exists
}

func (m *mockSessionMgr) Inject(name string, text string, submit bool) error {
	m.injected = append(m.injected, injectCall{name, text, submit})
	return m.injectErr
}

func (m *mockSessionMgr) Capture(name string, lines int) (string, error) {
	if len(m.captureResults) > 0 {
		result := m.captureResults[m.captureIndex]
		if m.captureIndex < len(m.captureResults)-1 {
			m.captureIndex++
		}
		return result, m.captureErr
	}
	return m.captureResult, m.captureErr
}

func (m *mockSessionMgr) Stop(name string, force bool) error {
	m.stopped = append(m.stopped, name)
	return nil
}

func (m *mockSessionMgr) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.started = append(m.started, startCall{name, workdir, cmd, env, role, world})
	return nil
}

func (m *mockSessionMgr) Cycle(name, workdir, cmd string, env map[string]string, role, world string) error {
	if m.cycleErr != nil {
		return m.cycleErr
	}
	m.cycled = append(m.cycled, startCall{name, workdir, cmd, env, role, world})
	return nil
}

type mockSphereStore struct {
	messages []msgCall
}

type msgCall struct {
	Sender, Recipient, Subject, Body string
	Priority                         int
	MsgType                          string
}

func (m *mockSphereStore) SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error) {
	m.messages = append(m.messages, msgCall{sender, recipient, subject, body, priority, msgType})
	return "msg-00000001", nil
}

func TestExec(t *testing.T) {
	solHome := setupSolHome(t)

	// Set up tether file.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree directory.
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Toast", "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	mgr := &mockSessionMgr{captureResult: "$ make test\nAll tests passed."}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:     "ember",
		AgentName: "Toast",
		Summary:   "Implemented login form.",
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Verify handoff file was written.
	if !HasHandoff("ember", "Toast", "agent") {
		t.Error("expected handoff file to exist after Exec")
	}

	// Verify session was cycled (not stopped+started).
	if len(mgr.stopped) != 0 {
		t.Errorf("expected no Stop calls (Cycle used instead), got %v", mgr.stopped)
	}
	if len(mgr.started) != 0 {
		t.Errorf("expected no Start calls (Cycle used instead), got %v", mgr.started)
	}
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
	if mgr.cycled[0].Name != "sol-ember-Toast" {
		t.Errorf("expected session name sol-ember-Toast, got %q", mgr.cycled[0].Name)
	}
	if mgr.cycled[0].Workdir != worktreeDir {
		t.Errorf("expected workdir %q, got %q", worktreeDir, mgr.cycled[0].Workdir)
	}
	// Session command should include --settings flag for reliable hook discovery.
	if !strings.Contains(mgr.cycled[0].Cmd, "--settings") {
		t.Errorf("expected session command to include --settings, got %q", mgr.cycled[0].Cmd)
	}
	if mgr.cycled[0].Role != "agent" {
		t.Errorf("expected role agent, got %q", mgr.cycled[0].Role)
	}

	// Verify mail was sent to self.
	if len(ts.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(ts.messages))
	}
	msg := ts.messages[0]
	if msg.Sender != "ember/Toast" {
		t.Errorf("expected sender ember/Toast, got %q", msg.Sender)
	}
	if msg.Recipient != "ember/Toast" {
		t.Errorf("expected recipient ember/Toast, got %q", msg.Recipient)
	}
	if msg.Subject != "HANDOFF: sol-abc12345" {
		t.Errorf("expected subject 'HANDOFF: sol-abc12345', got %q", msg.Subject)
	}
	if msg.Priority != 2 {
		t.Errorf("expected priority 2, got %d", msg.Priority)
	}
	if msg.MsgType != "notification" {
		t.Errorf("expected msgType notification, got %q", msg.MsgType)
	}
}

func TestExecNoTetherCyclesSession(t *testing.T) {
	solHome := setupSolHome(t)

	// Create governor directory (no tether for governor).
	govDir := filepath.Join(solHome, "ember", "governor")
	if err := os.MkdirAll(govDir, 0o755); err != nil {
		t.Fatalf("failed to create governor dir: %v", err)
	}

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:       "ember",
		AgentName:   "governor",
		Role:        "governor",
		WorktreeDir: govDir,
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed for no-tether governor: %v", err)
	}

	// No handoff file should be written (no tether).
	if HasHandoff("ember", "governor", "governor") {
		t.Error("expected no handoff file for governor without tether")
	}

	// Session should be cycled (not stopped+started).
	if len(mgr.stopped) != 0 {
		t.Errorf("expected no Stop calls, got %v", mgr.stopped)
	}
	if len(mgr.started) != 0 {
		t.Errorf("expected no Start calls, got %v", mgr.started)
	}
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
	if mgr.cycled[0].Workdir != govDir {
		t.Errorf("expected workdir %q, got %q", govDir, mgr.cycled[0].Workdir)
	}
	if mgr.cycled[0].Role != "governor" {
		t.Errorf("expected role governor, got %q", mgr.cycled[0].Role)
	}
	// No mail sent (no tether).
	if len(ts.messages) != 0 {
		t.Errorf("expected 0 messages for no-tether handoff, got %d", len(ts.messages))
	}
}

func TestExecWithExplicitRole(t *testing.T) {
	solHome := setupSolHome(t)

	// Set up tether file for an envoy with active work.
	if err := tether.Write("ember", "Alice", "sol-envoy12345", "envoy"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	envoyDir := filepath.Join(solHome, "ember", "envoys", "Alice", "worktree")
	if err := os.MkdirAll(envoyDir, 0o755); err != nil {
		t.Fatalf("failed to create envoy dir: %v", err)
	}

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:       "ember",
		AgentName:   "Alice",
		Role:        "envoy",
		WorktreeDir: envoyDir,
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Handoff file should be written (has tether).
	if !HasHandoff("ember", "Alice", "envoy") {
		t.Error("expected handoff file for tethered envoy")
	}

	// Session should be cycled with envoy role.
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
	if mgr.cycled[0].Role != "envoy" {
		t.Errorf("expected role envoy, got %q", mgr.cycled[0].Role)
	}
	if mgr.cycled[0].Workdir != envoyDir {
		t.Errorf("expected workdir %q, got %q", envoyDir, mgr.cycled[0].Workdir)
	}
}

func TestCaptureGitStatusStashDiff(t *testing.T) {
	setupSolHome(t)

	// Set up tether file.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create a real git repo for worktree to capture git status/stash/diff.
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Run()
	}

	// Create a file and commit it.
	os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main"), 0o644)
	exec.Command("git", "-C", dir, "add", "hello.go").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "initial").Run()

	// Create uncommitted changes for git status and diff stat.
	os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\nfunc main() {}"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main"), 0o644)

	state, err := Capture(CaptureOpts{
		World:       "ember",
		AgentName:   "Toast",
		Role:        "agent",
		Summary:     "Working",
		WorktreeDir: dir,
	}, nil, GitLog)

	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	// GitStatus should contain the modified file.
	if !strings.Contains(state.GitStatus, "hello.go") {
		t.Errorf("expected GitStatus to contain hello.go, got %q", state.GitStatus)
	}

	// DiffStat should contain the modified file.
	if !strings.Contains(state.DiffStat, "hello.go") {
		t.Errorf("expected DiffStat to contain hello.go, got %q", state.DiffStat)
	}

	// GitStash should be empty (nothing stashed).
	if state.GitStash != "" {
		t.Errorf("expected empty GitStash, got %q", state.GitStash)
	}
}

func TestMarkConsumedAndHasHandoff(t *testing.T) {
	setupSolHome(t)

	state := &State{
		WorkItemID: "sol-abc12345",
		AgentName:  "Toast",
		World:      "ember",
		Role:       "agent",
		Summary:    "test",
	}
	if err := Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// HasHandoff should return true for unconsumed.
	if !HasHandoff("ember", "Toast", "agent") {
		t.Error("expected HasHandoff to be true before consume")
	}

	// Mark consumed.
	if err := MarkConsumed("ember", "Toast", "agent"); err != nil {
		t.Fatalf("MarkConsumed failed: %v", err)
	}

	// HasHandoff should return false after consume.
	if HasHandoff("ember", "Toast", "agent") {
		t.Error("expected HasHandoff to be false after consume")
	}

	// File should still exist on disk.
	read, err := Read("ember", "Toast", "agent")
	if err != nil {
		t.Fatalf("Read failed after consume: %v", err)
	}
	if read == nil {
		t.Fatal("expected state to be non-nil after consume")
	}
	if !read.Consumed {
		t.Error("expected Consumed to be true after MarkConsumed")
	}

	// Next Write() overwrites with fresh (unconsumed) state.
	state.Summary = "fresh handoff"
	if err := Write(state); err != nil {
		t.Fatalf("overwrite Write failed: %v", err)
	}
	if !HasHandoff("ember", "Toast", "agent") {
		t.Error("expected HasHandoff to be true after overwrite")
	}
	read, _ = Read("ember", "Toast", "agent")
	if read.Consumed {
		t.Error("expected Consumed to be false after fresh Write")
	}
}

func TestMarkerWriteReadRemove(t *testing.T) {
	setupSolHome(t)

	// No marker initially.
	ts, reason, err := ReadMarker("ember", "Toast", "agent")
	if err != nil {
		t.Fatalf("ReadMarker returned error for missing marker: %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("expected zero timestamp for missing marker, got %v", ts)
	}

	// Write marker.
	if err := WriteMarker("ember", "Toast", "agent", "session handoff"); err != nil {
		t.Fatalf("WriteMarker failed: %v", err)
	}

	// Read marker.
	ts, reason, err = ReadMarker("ember", "Toast", "agent")
	if err != nil {
		t.Fatalf("ReadMarker failed: %v", err)
	}
	if ts.IsZero() {
		t.Error("expected non-zero timestamp after WriteMarker")
	}
	if time.Since(ts) > 5*time.Second {
		t.Errorf("marker timestamp too old: %v", ts)
	}
	if reason != "session handoff" {
		t.Errorf("expected reason 'session handoff', got %q", reason)
	}

	// Remove marker.
	if err := RemoveMarker("ember", "Toast", "agent"); err != nil {
		t.Fatalf("RemoveMarker failed: %v", err)
	}

	// Should be gone.
	ts, _, err = ReadMarker("ember", "Toast", "agent")
	if err != nil {
		t.Fatalf("ReadMarker after remove returned error: %v", err)
	}
	if !ts.IsZero() {
		t.Error("expected zero timestamp after RemoveMarker")
	}

	// Remove again — no-op.
	if err := RemoveMarker("ember", "Toast", "agent"); err != nil {
		t.Fatalf("RemoveMarker (second time) returned error: %v", err)
	}
}

func TestExecWritesMarker(t *testing.T) {
	solHome := setupSolHome(t)

	// Set up tether file.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree directory.
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Toast", "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	mgr := &mockSessionMgr{captureResult: "$ make test\nAll tests passed."}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:     "ember",
		AgentName: "Toast",
		Summary:   "Implemented login form.",
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Verify marker was written.
	markerTS, reason, err := ReadMarker("ember", "Toast", "agent")
	if err != nil {
		t.Fatalf("ReadMarker failed: %v", err)
	}
	if markerTS.IsZero() {
		t.Error("expected marker to be written after Exec")
	}
	if reason != "session handoff" {
		t.Errorf("expected reason 'session handoff', got %q", reason)
	}
}

func TestExecSkipsWhenResolveInProgress(t *testing.T) {
	solHome := setupSolHome(t)

	// Set up tether file.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree directory.
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Toast", "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	// Create resolve lock (simulating resolve in progress).
	agentDir := filepath.Join(solHome, "ember", "outposts", "Toast")
	lockPath := filepath.Join(agentDir, ".resolve_in_progress")
	if err := os.WriteFile(lockPath, []byte("sol-abc12345"), 0o644); err != nil {
		t.Fatalf("failed to write resolve lock: %v", err)
	}

	mgr := &mockSessionMgr{captureResult: "$ make test\nAll tests passed."}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:     "ember",
		AgentName: "Toast",
		Summary:   "Trying to handoff during resolve.",
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec should succeed (skip handoff): %v", err)
	}

	// Session should NOT be cycled — handoff was skipped.
	if len(mgr.cycled) != 0 {
		t.Errorf("expected 0 Cycle calls (resolve lock), got %d", len(mgr.cycled))
	}
	if len(mgr.stopped) != 0 {
		t.Errorf("expected 0 Stop calls (resolve lock), got %d", len(mgr.stopped))
	}
	if len(mgr.started) != 0 {
		t.Errorf("expected 0 Start calls (resolve lock), got %d", len(mgr.started))
	}

	// Resolve lock should still exist (not removed by handoff).
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("resolve lock should still exist after skipped handoff")
	}
}

func TestCooldownEnforced(t *testing.T) {
	solHome := setupSolHome(t)

	// Create governor directory (no tether — exempt from cooldown).
	govDir := filepath.Join(solHome, "ember", "governor")
	if err := os.MkdirAll(govDir, 0o755); err != nil {
		t.Fatalf("failed to create governor dir: %v", err)
	}

	// Write a fresh marker for an outpost agent.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Toast", "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	// Write a very recent marker (simulates just-completed handoff).
	if err := WriteMarker("ember", "Toast", "agent", "previous handoff"); err != nil {
		t.Fatalf("WriteMarker failed: %v", err)
	}

	// Governor should NOT be affected by cooldown (exempt).
	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}
	start := time.Now()
	err := Exec(ExecOpts{
		World:       "ember",
		AgentName:   "governor",
		Role:        "governor",
		WorktreeDir: govDir,
	}, mgr, ts, nil)
	if err != nil {
		t.Fatalf("governor Exec failed: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("governor handoff should be exempt from cooldown")
	}
}

func TestExecCycleFallback(t *testing.T) {
	solHome := setupSolHome(t)

	// Create governor directory (no tether).
	govDir := filepath.Join(solHome, "ember", "governor")
	if err := os.MkdirAll(govDir, 0o755); err != nil {
		t.Fatalf("failed to create governor dir: %v", err)
	}

	mgr := &mockSessionMgr{cycleErr: fmt.Errorf("respawn-pane failed")}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:       "ember",
		AgentName:   "governor",
		Role:        "governor",
		WorktreeDir: govDir,
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec should succeed with fallback: %v", err)
	}

	// Cycle was attempted but failed.
	if len(mgr.cycled) != 0 {
		t.Errorf("expected no successful Cycle calls, got %d", len(mgr.cycled))
	}
	// Fallback: Stop then Start.
	if len(mgr.stopped) != 1 {
		t.Errorf("expected 1 Stop call (fallback), got %d", len(mgr.stopped))
	}
	if len(mgr.started) != 1 {
		t.Fatalf("expected 1 Start call (fallback), got %d", len(mgr.started))
	}
	if mgr.started[0].Role != "governor" {
		t.Errorf("expected role governor, got %q", mgr.started[0].Role)
	}
}

func TestExecEnvoyBriefSave(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	solHome := setupSolHome(t)

	// Set up envoy with brief directory.
	envoyDir := filepath.Join(solHome, "ember", "envoys", "Alice")
	briefDir := filepath.Join(envoyDir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatalf("failed to create brief dir: %v", err)
	}
	// Worktree for the envoy (used by Cycle).
	worktreeDir := filepath.Join(envoyDir, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	mgr := &mockSessionMgr{
		exists: true,
		// Simulate: changing output, then stable (agent done saving brief).
		captureResults: []string{"working...", "saving brief...", "done", "done", "done"},
	}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:       "ember",
		AgentName:   "Alice",
		Role:        "envoy",
		WorktreeDir: worktreeDir,
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Brief save prompt should have been injected.
	if len(mgr.injected) != 1 {
		t.Fatalf("expected 1 Inject call, got %d", len(mgr.injected))
	}
	if mgr.injected[0].Text != BriefSavePrompt {
		t.Errorf("expected BriefSavePrompt, got %q", mgr.injected[0].Text)
	}
	if !mgr.injected[0].Submit {
		t.Error("expected Inject submit=true")
	}

	// Session should still be cycled after brief save.
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
	if mgr.cycled[0].Role != "envoy" {
		t.Errorf("expected role envoy, got %q", mgr.cycled[0].Role)
	}
}

func TestExecGovernorBriefSave(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	solHome := setupSolHome(t)

	// Set up governor with brief directory.
	govDir := filepath.Join(solHome, "ember", "governor")
	briefDir := filepath.Join(govDir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatalf("failed to create brief dir: %v", err)
	}

	mgr := &mockSessionMgr{
		exists:         true,
		captureResults: []string{"stable", "stable", "stable"},
	}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:       "ember",
		AgentName:   "governor",
		Role:        "governor",
		WorktreeDir: govDir,
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Brief save prompt should have been injected.
	if len(mgr.injected) != 1 {
		t.Fatalf("expected 1 Inject call for governor, got %d", len(mgr.injected))
	}
	if mgr.injected[0].Text != BriefSavePrompt {
		t.Errorf("expected BriefSavePrompt, got %q", mgr.injected[0].Text)
	}

	// Session should be cycled.
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
}

func TestExecBriefSaveTimeout(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	solHome := setupSolHome(t)

	// Set up envoy with brief directory.
	envoyDir := filepath.Join(solHome, "ember", "envoys", "Bob")
	briefDir := filepath.Join(envoyDir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatalf("failed to create brief dir: %v", err)
	}
	worktreeDir := filepath.Join(envoyDir, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	mgr := &mockSessionMgr{
		exists: true,
		// Output keeps changing — never stabilizes (simulates unresponsive agent).
		captureResults: []string{"a", "b", "c", "d", "e", "f", "g", "h"},
	}
	ts := &mockSphereStore{}

	start := time.Now()
	err := Exec(ExecOpts{
		World:       "ember",
		AgentName:   "Bob",
		Role:        "envoy",
		WorktreeDir: worktreeDir,
	}, mgr, ts, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Exec should succeed even on brief save timeout: %v", err)
	}

	// Should complete quickly (stub timeouts are ~200ms max).
	if elapsed > 5*time.Second {
		t.Errorf("expected quick completion with stub timeouts, took %v", elapsed)
	}

	// Brief save was attempted.
	if len(mgr.injected) != 1 {
		t.Fatalf("expected 1 Inject call, got %d", len(mgr.injected))
	}

	// Session should still be cycled despite timeout.
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call after timeout, got %d", len(mgr.cycled))
	}
}

func TestExecNoBriefSaveForOutpost(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	solHome := setupSolHome(t)

	// Set up outpost agent (no brief directory needed — they don't use briefs).
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Toast", "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	mgr := &mockSessionMgr{captureResult: "test output"}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:     "ember",
		AgentName: "Toast",
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// No brief save for outpost role.
	if len(mgr.injected) != 0 {
		t.Errorf("expected 0 Inject calls for outpost, got %d", len(mgr.injected))
	}

	// Session should be cycled normally.
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
}

func TestExecNoBriefSaveForForge(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	solHome := setupSolHome(t)

	// Set up forge directory.
	forgeDir := filepath.Join(solHome, "ember", "forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatalf("failed to create forge dir: %v", err)
	}

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:       "ember",
		AgentName:   "forge",
		Role:        "forge",
		WorktreeDir: forgeDir,
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// No brief save for forge role.
	if len(mgr.injected) != 0 {
		t.Errorf("expected 0 Inject calls for forge, got %d", len(mgr.injected))
	}
}

func TestExecNoBriefSaveWithoutBriefDir(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	solHome := setupSolHome(t)

	// Set up envoy WITHOUT brief directory (hasn't been initialized yet).
	envoyDir := filepath.Join(solHome, "ember", "envoys", "Carol")
	worktreeDir := filepath.Join(envoyDir, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:       "ember",
		AgentName:   "Carol",
		Role:        "envoy",
		WorktreeDir: worktreeDir,
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// No brief save when .brief/ doesn't exist.
	if len(mgr.injected) != 0 {
		t.Errorf("expected 0 Inject calls without brief dir, got %d", len(mgr.injected))
	}

	// Session should still be cycled.
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
}
