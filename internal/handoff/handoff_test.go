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

	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

func setupSolHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	return dir
}

// registerMinimalRole registers a minimal startup config for a role in tests.
// The config only provides a WorktreeDir function — no persona, hooks, or
// system prompt — so the startup path succeeds without side effects.
func registerMinimalRole(t *testing.T, role, worktreeDir string) {
	t.Helper()
	startup.Register(role, startup.RoleConfig{
		WorktreeDir: func(w, a string) string { return worktreeDir },
	})
	t.Cleanup(func() { startup.Register(role, startup.RoleConfig{}) })
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
	instanceJSON := `{"workflow":"default-work","writ_id":"sol-abc12345","variables":{},"instantiated_at":"2026-02-27T10:00:00Z"}`
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

	if state.WritID != "sol-abc12345" {
		t.Errorf("expected WritID sol-abc12345, got %q", state.WritID)
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

func TestCaptureWithActiveWrit(t *testing.T) {
	solHome := setupSolHome(t)

	// Set up multiple tethers (persistent agent scenario).
	if err := tether.Write("ember", "Toast", "sol-writ-aaa", "agent"); err != nil {
		t.Fatalf("failed to write tether 1: %v", err)
	}
	if err := tether.Write("ember", "Toast", "sol-writ-bbb", "agent"); err != nil {
		t.Fatalf("failed to write tether 2: %v", err)
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

	// Sphere store with active writ set to the second tether.
	sphere := &mockSphereStore{
		agents: map[string]*store.Agent{
			"ember/Toast": {ID: "ember/Toast", Name: "Toast", World: "ember", ActiveWrit: "sol-writ-bbb"},
		},
	}

	mockGitLog := func(dir string, count int) ([]string, error) {
		return []string{"abc1234 feat: working on bbb"}, nil
	}

	state, err := Capture(CaptureOpts{
		World:     "ember",
		AgentName: "Toast",
		Role:      "agent",
		Sphere:    sphere,
	}, nil, mockGitLog)

	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	// ActiveWritID should be set from DB.
	if state.ActiveWritID != "sol-writ-bbb" {
		t.Errorf("expected ActiveWritID sol-writ-bbb, got %q", state.ActiveWritID)
	}
	// WritID should also be set to the active writ (not the first tether).
	if state.WritID != "sol-writ-bbb" {
		t.Errorf("expected WritID sol-writ-bbb (from active writ), got %q", state.WritID)
	}
	// Git log should have been captured (writ-specific context).
	if len(state.RecentCommits) != 1 {
		t.Errorf("expected 1 recent commit, got %d", len(state.RecentCommits))
	}
}

func TestCaptureNoActiveWrit(t *testing.T) {
	setupSolHome(t)

	// No tethers and no active writ in DB.
	sphere := &mockSphereStore{
		agents: map[string]*store.Agent{
			"ember/Toast": {ID: "ember/Toast", Name: "Toast", World: "ember", ActiveWrit: ""},
		},
	}

	mockCapture := func(name string, lines int) (string, error) {
		return "$ idle output\n$", nil
	}

	state, err := Capture(CaptureOpts{
		World:     "ember",
		AgentName: "Toast",
		Role:      "agent",
		Sphere:    sphere,
	}, mockCapture, nil)

	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	// No active writ and no tether — general state only.
	if state.ActiveWritID != "" {
		t.Errorf("expected empty ActiveWritID, got %q", state.ActiveWritID)
	}
	if state.WritID != "" {
		t.Errorf("expected empty WritID, got %q", state.WritID)
	}
	// Session output should still be captured.
	if !strings.Contains(state.RecentOutput, "idle output") {
		t.Errorf("expected recent output to contain 'idle output', got %q", state.RecentOutput)
	}
	// No writ-specific fields.
	if state.GitStatus != "" {
		t.Errorf("expected empty GitStatus, got %q", state.GitStatus)
	}
	if state.WorkflowStep != "" {
		t.Errorf("expected empty WorkflowStep, got %q", state.WorkflowStep)
	}
	// Auto-generated summary should say no active work.
	if !strings.Contains(state.Summary, "No active work") {
		t.Errorf("expected summary to mention 'No active work', got %q", state.Summary)
	}
}

func TestResumeAfterHandoffRestoresActiveWrit(t *testing.T) {
	// Verify that BuildResumeState includes active writ for resume restoration.
	state := &State{
		WritID:       "sol-writ-bbb",
		ActiveWritID: "sol-writ-bbb",
		AgentName:    "Toast",
		World:        "ember",
		WorkflowStep: "implement",
	}

	resumeState := state.BuildResumeState("compact")

	if resumeState.ClaimedResource != "sol-writ-bbb" {
		t.Errorf("expected ClaimedResource sol-writ-bbb, got %q", resumeState.ClaimedResource)
	}
	if resumeState.NewActiveWrit != "sol-writ-bbb" {
		t.Errorf("expected NewActiveWrit sol-writ-bbb, got %q", resumeState.NewActiveWrit)
	}
	if resumeState.Reason != "compact" {
		t.Errorf("expected reason compact, got %q", resumeState.Reason)
	}
	if resumeState.CurrentStep != "implement" {
		t.Errorf("expected CurrentStep implement, got %q", resumeState.CurrentStep)
	}
}

func TestBuildResumeStateNoActiveWrit(t *testing.T) {
	// When ActiveWritID is empty, NewActiveWrit should not be set.
	state := &State{
		WritID:    "sol-writ-aaa",
		AgentName: "Toast",
		World:     "ember",
	}

	resumeState := state.BuildResumeState("manual")

	if resumeState.ClaimedResource != "sol-writ-aaa" {
		t.Errorf("expected ClaimedResource sol-writ-aaa, got %q", resumeState.ClaimedResource)
	}
	if resumeState.NewActiveWrit != "" {
		t.Errorf("expected empty NewActiveWrit, got %q", resumeState.NewActiveWrit)
	}
}

func TestCompactRecoveryWithActiveWrit(t *testing.T) {
	setupSolHome(t)

	// Set up tethers and active writ.
	if err := tether.Write("ember", "Toast", "sol-writ-aaa", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}
	if err := tether.Write("ember", "Toast", "sol-writ-bbb", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	sphere := &mockSphereStore{
		agents: map[string]*store.Agent{
			"ember/Toast": {ID: "ember/Toast", Name: "Toast", World: "ember", ActiveWrit: "sol-writ-bbb"},
		},
	}

	// Capture state with active writ.
	state, err := Capture(CaptureOpts{
		World:     "ember",
		AgentName: "Toast",
		Role:      "agent",
		Sphere:    sphere,
	}, nil, nil)

	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	// Write the handoff file.
	if err := Write(state); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read it back — simulates recovery after crash.
	recovered, err := Read("ember", "Toast", "agent")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Active writ should be preserved through serialization.
	if recovered.ActiveWritID != "sol-writ-bbb" {
		t.Errorf("expected recovered ActiveWritID sol-writ-bbb, got %q", recovered.ActiveWritID)
	}
	if recovered.WritID != "sol-writ-bbb" {
		t.Errorf("expected recovered WritID sol-writ-bbb, got %q", recovered.WritID)
	}

	// BuildResumeState from recovered state should include active writ.
	resumeState := recovered.BuildResumeState("compact")
	if resumeState.NewActiveWrit != "sol-writ-bbb" {
		t.Errorf("expected NewActiveWrit sol-writ-bbb in resume state, got %q", resumeState.NewActiveWrit)
	}
}

func TestCaptureResumeStateWithActiveWrit(t *testing.T) {
	setupSolHome(t)

	// Tether exists but active writ in DB is different.
	if err := tether.Write("ember", "Toast", "sol-writ-aaa", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	sphere := &mockSphereStore{
		agents: map[string]*store.Agent{
			"ember/Toast": {ID: "ember/Toast", Name: "Toast", World: "ember", ActiveWrit: "sol-writ-bbb"},
		},
	}

	resumeState := CaptureResumeState("ember", "Toast", "agent", "compact", sphere)

	// Should use active writ from DB, not tether.
	if resumeState.ClaimedResource != "sol-writ-bbb" {
		t.Errorf("expected ClaimedResource sol-writ-bbb (from DB), got %q", resumeState.ClaimedResource)
	}
	if resumeState.NewActiveWrit != "sol-writ-bbb" {
		t.Errorf("expected NewActiveWrit sol-writ-bbb, got %q", resumeState.NewActiveWrit)
	}
}

func TestCaptureResumeStateWithoutSphere(t *testing.T) {
	setupSolHome(t)

	// Tether exists, no sphere store.
	if err := tether.Write("ember", "Toast", "sol-writ-aaa", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	resumeState := CaptureResumeState("ember", "Toast", "agent", "compact", nil)

	// Should fall back to tether.
	if resumeState.ClaimedResource != "sol-writ-aaa" {
		t.Errorf("expected ClaimedResource sol-writ-aaa (from tether), got %q", resumeState.ClaimedResource)
	}
	if resumeState.NewActiveWrit != "" {
		t.Errorf("expected empty NewActiveWrit (no sphere), got %q", resumeState.NewActiveWrit)
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

	// Auto-generated summary should contain agent name and writ.
	if !strings.Contains(state.Summary, "Toast") {
		t.Errorf("auto-generated summary missing agent name: %q", state.Summary)
	}
	if !strings.Contains(state.Summary, "sol-abc12345") {
		t.Errorf("auto-generated summary missing writ ID: %q", state.Summary)
	}
	if !strings.Contains(state.Summary, "abc1234") {
		t.Errorf("auto-generated summary missing last commit: %q", state.Summary)
	}
}

func TestWriteAndRead(t *testing.T) {
	setupSolHome(t)

	original := &State{
		WritID:       "sol-abc12345",
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

	if read.WritID != original.WritID {
		t.Errorf("WritID mismatch: got %q, want %q", read.WritID, original.WritID)
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
		WritID: "sol-abc12345",
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
		WritID: "sol-abc12345",
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

func (m *mockSessionMgr) NudgeSession(name string, message string) error {
	return nil
}

func (m *mockSessionMgr) WaitForIdle(name string, timeout time.Duration) error {
	return nil
}

type mockSphereStore struct {
	messages   []msgCall
	agents     map[string]*store.Agent // agent ID → agent (for GetAgent)
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

func (m *mockSphereStore) GetAgent(id string) (*store.Agent, error) {
	if m.agents != nil {
		if a, ok := m.agents[id]; ok {
			return a, nil
		}
	}
	return nil, fmt.Errorf("agent %q not found", id)
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

	registerMinimalRole(t, "agent", worktreeDir)

	mgr := &mockSessionMgr{captureResult: "$ make test\nAll tests passed."}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Toast",
		Summary:       "Implemented login form.",
		StartupSphere: &mockStartupSphere{},
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

func TestExecGetAgentFailureFallsBackToTether(t *testing.T) {
	solHome := setupSolHome(t)

	// Set up tether file — this is the fallback when GetAgent fails.
	if err := tether.Write("ember", "Toast", "sol-tether999", "agent"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree directory.
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Toast", "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	registerMinimalRole(t, "agent", worktreeDir)

	mgr := &mockSessionMgr{captureResult: "$ working"}
	// No agents in map — GetAgent will return error for "ember/Toast".
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Toast",
		Summary:       "Testing GetAgent failure fallback.",
		StartupSphere: &mockStartupSphere{},
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Handoff file should be written (tether fallback provides hasWork=true).
	if !HasHandoff("ember", "Toast", "agent") {
		t.Error("expected handoff file to exist after Exec with GetAgent failure")
	}

	// Verify the handoff state used the tether writ ID (not an active writ from DB).
	state, err := Read("ember", "Toast", "agent")
	if err != nil {
		t.Fatalf("Read handoff state failed: %v", err)
	}
	if state.WritID != "sol-tether999" {
		t.Errorf("expected WritID sol-tether999 (from tether fallback), got %q", state.WritID)
	}
	if state.ActiveWritID != "" {
		t.Errorf("expected empty ActiveWritID (GetAgent failed), got %q", state.ActiveWritID)
	}

	// Session should be cycled.
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}

	// Mail should be sent (hasWork was true via tether).
	if len(ts.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(ts.messages))
	}
	if ts.messages[0].Subject != "HANDOFF: sol-tether999" {
		t.Errorf("expected subject 'HANDOFF: sol-tether999', got %q", ts.messages[0].Subject)
	}
}

func TestExecNoTetherCyclesSession(t *testing.T) {
	solHome := setupSolHome(t)

	// Create governor directory (no tether for governor).
	govDir := filepath.Join(solHome, "ember", "governor")
	if err := os.MkdirAll(govDir, 0o755); err != nil {
		t.Fatalf("failed to create governor dir: %v", err)
	}

	registerMinimalRole(t, "governor", govDir)

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "governor",
		Role:          "governor",
		WorktreeDir:   govDir,
		StartupSphere: &mockStartupSphere{},
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

	registerMinimalRole(t, "envoy", envoyDir)

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Alice",
		Role:          "envoy",
		WorktreeDir:   envoyDir,
		StartupSphere: &mockStartupSphere{},
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
		WritID: "sol-abc12345",
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

	registerMinimalRole(t, "agent", worktreeDir)

	mgr := &mockSessionMgr{captureResult: "$ make test\nAll tests passed."}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Toast",
		Summary:       "Implemented login form.",
		StartupSphere: &mockStartupSphere{},
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Verify marker was written. The marker is written BEFORE the cycle
	// operation so it survives process death from respawn-pane -k.
	markerTS, reason, err := ReadMarker("ember", "Toast", "agent")
	if err != nil {
		t.Fatalf("ReadMarker failed: %v", err)
	}
	if markerTS.IsZero() {
		t.Error("expected marker to be written after Exec")
	}
	if reason != "unknown" {
		t.Errorf("expected reason 'unknown', got %q", reason)
	}
}

// TestExecWritesMarkerBeforeCycle verifies the marker is written before
// the cycle operation, not after. In production, cycleOp uses respawn-pane -k
// which kills the calling process — any WriteMarker after the cycle call
// would be dead code. This test confirms the marker survives a successful cycle.
func TestExecWritesMarkerBeforeCycle(t *testing.T) {
	solHome := setupSolHome(t)

	world := "ember"
	agentName := "MarkerBot"
	roleName := "testrole-marker"

	worktreeDir := filepath.Join(solHome, world, "outposts", agentName, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	startup.Register(roleName, startup.RoleConfig{
		WorktreeDir: func(w, a string) string { return worktreeDir },
	})

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         world,
		AgentName:     agentName,
		Role:          roleName,
		WorktreeDir:   worktreeDir,
		Reason:        "compact",
		StartupSphere: &mockStartupSphere{},
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Verify marker was written with the correct reason.
	markerTS, reason, err := ReadMarker(world, agentName, roleName)
	if err != nil {
		t.Fatalf("ReadMarker failed: %v", err)
	}
	if markerTS.IsZero() {
		t.Fatal("expected marker to be written")
	}
	if reason != "compact" {
		t.Errorf("expected reason 'compact', got %q", reason)
	}

	// Verify cycle still happened (marker didn't prevent it).
	if len(mgr.cycled) != 1 {
		t.Errorf("expected 1 Cycle call, got %d", len(mgr.cycled))
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

	registerMinimalRole(t, "governor", govDir)

	// Governor should NOT be affected by cooldown (exempt).
	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}
	start := time.Now()
	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "governor",
		Role:          "governor",
		WorktreeDir:   govDir,
		StartupSphere: &mockStartupSphere{},
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

	registerMinimalRole(t, "governor", govDir)

	mgr := &mockSessionMgr{cycleErr: fmt.Errorf("respawn-pane failed")}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "governor",
		Role:          "governor",
		WorktreeDir:   govDir,
		StartupSphere: &mockStartupSphere{},
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

	registerMinimalRole(t, "envoy", worktreeDir)

	mgr := &mockSessionMgr{
		exists: true,
		// Simulate: changing output, then stable (agent done saving brief).
		captureResults: []string{"working...", "saving brief...", "done", "done", "done"},
	}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Alice",
		Role:          "envoy",
		WorktreeDir:   worktreeDir,
		StartupSphere: &mockStartupSphere{},
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

	registerMinimalRole(t, "governor", govDir)

	mgr := &mockSessionMgr{
		exists:         true,
		captureResults: []string{"stable", "stable", "stable"},
	}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "governor",
		Role:          "governor",
		WorktreeDir:   govDir,
		StartupSphere: &mockStartupSphere{},
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

	registerMinimalRole(t, "envoy", worktreeDir)

	mgr := &mockSessionMgr{
		exists: true,
		// Output keeps changing — never stabilizes (simulates unresponsive agent).
		captureResults: []string{"a", "b", "c", "d", "e", "f", "g", "h"},
	}
	ts := &mockSphereStore{}

	start := time.Now()
	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Bob",
		Role:          "envoy",
		WorktreeDir:   worktreeDir,
		StartupSphere: &mockStartupSphere{},
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

	registerMinimalRole(t, "agent", worktreeDir)

	mgr := &mockSessionMgr{captureResult: "test output"}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Toast",
		StartupSphere: &mockStartupSphere{},
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

	registerMinimalRole(t, "forge", forgeDir)

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "forge",
		Role:          "forge",
		WorktreeDir:   forgeDir,
		StartupSphere: &mockStartupSphere{},
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

	registerMinimalRole(t, "envoy", worktreeDir)

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Carol",
		Role:          "envoy",
		WorktreeDir:   worktreeDir,
		StartupSphere: &mockStartupSphere{},
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

func TestExecSelfHandoffSkipsBriefSave(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	solHome := setupSolHome(t)

	// Simulate self-handoff: set SOL_AGENT and SOL_WORLD to match the target.
	t.Setenv("SOL_AGENT", "Alice")
	t.Setenv("SOL_WORLD", "ember")

	// Set up envoy with brief directory.
	envoyDir := filepath.Join(solHome, "ember", "envoys", "Alice")
	briefDir := filepath.Join(envoyDir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatalf("failed to create brief dir: %v", err)
	}
	worktreeDir := filepath.Join(envoyDir, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	registerMinimalRole(t, "envoy", worktreeDir)

	mgr := &mockSessionMgr{
		exists:         true,
		captureResults: []string{"stable", "stable", "stable"},
	}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Alice",
		Role:          "envoy",
		WorktreeDir:   worktreeDir,
		StartupSphere: &mockStartupSphere{},
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Self-handoff should skip briefSave — no inject calls.
	if len(mgr.injected) != 0 {
		t.Errorf("expected 0 Inject calls for self-handoff, got %d", len(mgr.injected))
	}

	// Session should still be cycled.
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
}

func TestExecExternalHandoffStillRunsBriefSave(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	solHome := setupSolHome(t)

	// Simulate external handoff: SOL_AGENT/SOL_WORLD don't match the target.
	t.Setenv("SOL_AGENT", "Bob")
	t.Setenv("SOL_WORLD", "other-world")

	// Set up envoy with brief directory.
	envoyDir := filepath.Join(solHome, "ember", "envoys", "Alice")
	briefDir := filepath.Join(envoyDir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatalf("failed to create brief dir: %v", err)
	}
	worktreeDir := filepath.Join(envoyDir, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	registerMinimalRole(t, "envoy", worktreeDir)

	mgr := &mockSessionMgr{
		exists:         true,
		captureResults: []string{"stable", "stable", "stable"},
	}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Alice",
		Role:          "envoy",
		WorktreeDir:   worktreeDir,
		StartupSphere: &mockStartupSphere{},
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// External handoff should still run briefSave.
	if len(mgr.injected) != 1 {
		t.Fatalf("expected 1 Inject call for external handoff, got %d", len(mgr.injected))
	}
	if mgr.injected[0].Text != BriefSavePrompt {
		t.Errorf("expected BriefSavePrompt, got %q", mgr.injected[0].Text)
	}

	// Session should be cycled.
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
}

func TestExecSelfHandoffPartialMatchStillRunsBriefSave(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	solHome := setupSolHome(t)

	// Partial match: same agent name, different world.
	t.Setenv("SOL_AGENT", "Alice")
	t.Setenv("SOL_WORLD", "other-world")

	// Set up envoy with brief directory.
	envoyDir := filepath.Join(solHome, "ember", "envoys", "Alice")
	briefDir := filepath.Join(envoyDir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatalf("failed to create brief dir: %v", err)
	}
	worktreeDir := filepath.Join(envoyDir, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	registerMinimalRole(t, "envoy", worktreeDir)

	mgr := &mockSessionMgr{
		exists:         true,
		captureResults: []string{"stable", "stable", "stable"},
	}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Alice",
		Role:          "envoy",
		WorktreeDir:   worktreeDir,
		StartupSphere: &mockStartupSphere{},
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Partial match (agent matches, world doesn't) should still run briefSave.
	if len(mgr.injected) != 1 {
		t.Fatalf("expected 1 Inject call for partial match, got %d", len(mgr.injected))
	}
}

func TestExecSelfHandoffEmptyEnvVarsNotDetectedAsSelf(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	solHome := setupSolHome(t)

	// Ensure env vars are empty — shouldn't match as self-handoff even though
	// both are technically "equal" (empty == empty).
	t.Setenv("SOL_AGENT", "")
	t.Setenv("SOL_WORLD", "")

	// Set up envoy with brief directory.
	envoyDir := filepath.Join(solHome, "ember", "envoys", "Alice")
	briefDir := filepath.Join(envoyDir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatalf("failed to create brief dir: %v", err)
	}
	worktreeDir := filepath.Join(envoyDir, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	registerMinimalRole(t, "envoy", worktreeDir)

	mgr := &mockSessionMgr{
		exists:         true,
		captureResults: []string{"stable", "stable", "stable"},
	}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         "ember",
		AgentName:     "Alice",
		Role:          "envoy",
		WorktreeDir:   worktreeDir,
		StartupSphere: &mockStartupSphere{},
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Empty env vars should not be detected as self-handoff — briefSave should run.
	if len(mgr.injected) != 1 {
		t.Fatalf("expected 1 Inject call when env vars are empty, got %d", len(mgr.injected))
	}
}

// --- Mock startup sphere store ---

type mockStartupSphere struct{}

func (m *mockStartupSphere) GetAgent(id string) (*store.Agent, error) {
	return &store.Agent{ID: id}, nil
}
func (m *mockStartupSphere) CreateAgent(name, world, role string) (string, error) {
	return world + "/" + name, nil
}
func (m *mockStartupSphere) UpdateAgentState(id, state, activeWrit string) error {
	return nil
}
func (m *mockStartupSphere) Close() error {
	return nil
}

// --- Startup path tests ---

func TestExecStartupPathForRegisteredRole(t *testing.T) {
	solHome := setupSolHome(t)

	world := "ember"
	agentName := "StartupBot"
	roleName := "testrole-startup"

	// Create worktree directory.
	worktreeDir := filepath.Join(solHome, world, "outposts", agentName, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	// Register role with persona, system prompt, and prime builder.
	startup.Register(roleName, startup.RoleConfig{
		WorktreeDir: func(w, a string) string { return worktreeDir },
		Persona: func(w, a string) ([]byte, error) {
			return []byte("# Test Persona\nYou are a test agent."), nil
		},
		SystemPromptContent: "You are the test system prompt.",
		ReplacePrompt:       true,
		PrimeBuilder: func(w, a string) string {
			return fmt.Sprintf("Test prime for %s in %s", a, w)
		},
	})

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         world,
		AgentName:     agentName,
		Role:          roleName,
		WorktreeDir:   worktreeDir,
		StartupSphere: &mockStartupSphere{},
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Verify session was cycled.
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
	cmd := mgr.cycled[0].Cmd

	// Command should contain system prompt flag.
	if !strings.Contains(cmd, "--system-prompt-file") {
		t.Errorf("expected --system-prompt-file in command, got %q", cmd)
	}

	// Command should contain role-specific prime.
	if !strings.Contains(cmd, "Test prime for StartupBot in ember") {
		t.Errorf("expected role-specific prime in command, got %q", cmd)
	}

	// Command should NOT contain --continue (non-compact handoff).
	if strings.Contains(cmd, "--continue") {
		t.Errorf("expected no --continue for non-compact handoff, got %q", cmd)
	}

	// Persona should be installed (CLAUDE.local.md).
	personaPath := filepath.Join(worktreeDir, "CLAUDE.local.md")
	data, err := os.ReadFile(personaPath)
	if err != nil {
		t.Fatalf("persona file not installed: %v", err)
	}
	if !strings.Contains(string(data), "Test Persona") {
		t.Errorf("persona content mismatch: %q", string(data))
	}

	// System prompt should be written.
	spPath := filepath.Join(worktreeDir, ".claude", "system-prompt.md")
	data, err = os.ReadFile(spPath)
	if err != nil {
		t.Fatalf("system prompt file not written: %v", err)
	}
	if !strings.Contains(string(data), "test system prompt") {
		t.Errorf("system prompt content mismatch: %q", string(data))
	}

	// Env should contain CLAUDE_CONFIG_DIR.
	if mgr.cycled[0].Env["CLAUDE_CONFIG_DIR"] == "" {
		t.Error("expected CLAUDE_CONFIG_DIR in env")
	}
}

func TestExecStartupCompactUsesResume(t *testing.T) {
	solHome := setupSolHome(t)

	world := "ember"
	agentName := "CompactBot"
	roleName := "testrole-compact"

	worktreeDir := filepath.Join(solHome, world, "outposts", agentName, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	startup.Register(roleName, startup.RoleConfig{
		WorktreeDir: func(w, a string) string { return worktreeDir },
		PrimeBuilder: func(w, a string) string {
			return "Role-specific prime"
		},
	})

	// Set up tether so resume state captures the writ.
	if err := tether.Write(world, agentName, "sol-compact12345", roleName); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         world,
		AgentName:     agentName,
		Role:          roleName,
		WorktreeDir:   worktreeDir,
		Reason:        "compact",
		StartupSphere: &mockStartupSphere{},
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
	cmd := mgr.cycled[0].Cmd

	// Compact should NOT use --continue — reloading the bloated conversation
	// that triggered compaction causes an immediate re-compaction loop.
	if strings.Contains(cmd, "--continue") {
		t.Errorf("compact handoff must NOT use --continue (causes compaction loop), got %q", cmd)
	}

	// Resume context should still be prepended to prime (via BuildResumePrime).
	if !strings.Contains(cmd, "[RESUME]") {
		t.Errorf("expected [RESUME] prefix in prime for compact handoff, got %q", cmd)
	}

	// Should also contain the role-specific prime.
	if !strings.Contains(cmd, "Role-specific prime") {
		t.Errorf("expected role-specific prime in command, got %q", cmd)
	}

	// Should contain the claimed resource from tether.
	if !strings.Contains(cmd, "sol-compact12345") {
		t.Errorf("expected claimed resource in resume prime, got %q", cmd)
	}
}

func TestExecErrorsForUnregisteredRole(t *testing.T) {
	setupSolHome(t)

	world := "ember"
	agentName := "LegacyBot"
	roleName := "unregistered-role-xyz"

	mgr := &mockSessionMgr{}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:     world,
		AgentName: agentName,
		Role:      roleName,
	}, mgr, ts, nil)

	if err == nil {
		t.Fatal("expected error for unregistered role, got nil")
	}
	if !strings.Contains(err.Error(), "no startup config registered") {
		t.Errorf("expected 'no startup config registered' error, got %q", err.Error())
	}

	// No session operations should have been attempted.
	if len(mgr.cycled) != 0 {
		t.Errorf("expected 0 Cycle calls, got %d", len(mgr.cycled))
	}
	if len(mgr.started) != 0 {
		t.Errorf("expected 0 Start calls, got %d", len(mgr.started))
	}
}

func TestExecStartupCycleFallback(t *testing.T) {
	solHome := setupSolHome(t)

	world := "ember"
	agentName := "FallbackBot"
	roleName := "testrole-fallback"

	worktreeDir := filepath.Join(solHome, world, "outposts", agentName, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	startup.Register(roleName, startup.RoleConfig{
		WorktreeDir: func(w, a string) string { return worktreeDir },
		PrimeBuilder: func(w, a string) string {
			return "Fallback prime"
		},
	})

	// Cycle will fail, triggering Stop+Start fallback.
	mgr := &mockSessionMgr{cycleErr: fmt.Errorf("respawn-pane failed")}
	ts := &mockSphereStore{}

	err := Exec(ExecOpts{
		World:         world,
		AgentName:     agentName,
		Role:          roleName,
		WorktreeDir:   worktreeDir,
		StartupSphere: &mockStartupSphere{},
	}, mgr, ts, nil)

	if err != nil {
		t.Fatalf("Exec should succeed with fallback: %v", err)
	}

	// Cycle was attempted but failed — no successful cycles.
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

	// Even with fallback, startup path should produce role-specific command.
	cmd := mgr.started[0].Cmd
	if !strings.Contains(cmd, "Fallback prime") {
		t.Errorf("expected role-specific prime in fallback command, got %q", cmd)
	}
}
