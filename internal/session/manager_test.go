package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain sets up a single shared tmux server for all session tests.
// Using a shared server (via a single TMUX_TMPDIR) avoids per-test server
// startup overhead (~300ms each) and enables t.Parallel() across all tests.
// Session names are unique per test, so there is no cross-test interference.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "sol-session-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create tmpdir: %v\n", err)
		os.Exit(1)
	}

	os.Setenv("TMUX_TMPDIR", tmpDir)
	os.Setenv("TMUX", "")
	os.Setenv("SOL_HOME", filepath.Join(tmpDir, "sol"))

	code := m.Run()

	kill, killCancel := tmuxCmd("kill-server")
	_ = kill.Run()
	killCancel()

	os.RemoveAll(tmpDir)
	os.Exit(code)
}

// setupTest returns a session Manager. The shared tmux environment is
// configured by TestMain; individual tests do not need their own isolation.
func setupTest(t *testing.T) *Manager {
	t.Helper()
	return New()
}

func TestStartStop(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Start("test-ss", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	if !mgr.Exists("test-ss") {
		t.Fatal("session should exist after Start")
	}

	err = mgr.Stop("test-ss", true)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Let tmux process the kill
	time.Sleep(200 * time.Millisecond)

	if mgr.Exists("test-ss") {
		t.Fatal("session should not exist after Stop")
	}
}

func TestList(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	names := []string{"list-a", "list-b", "list-c"}
	for _, name := range names {
		name := name
		err := mgr.Start(name, t.TempDir(), "sleep 300", nil, "outpost", "haven")
		if err != nil {
			t.Fatalf("Start %s failed: %v", name, err)
		}
		t.Cleanup(func() { _ = mgr.Stop(name, true) })
	}

	// Let tmux stabilize after creating all sessions
	time.Sleep(500 * time.Millisecond)

	sessions, err := mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Don't check exact count — other parallel tests may have sessions.
	// Verify all expected sessions are present and alive.
	found := make(map[string]*SessionInfo)
	for i := range sessions {
		found[sessions[i].Name] = &sessions[i]
	}
	for _, name := range names {
		s, ok := found[name]
		if !ok {
			t.Errorf("session %s not found in list", name)
			continue
		}
		if !s.Alive {
			t.Errorf("session %s should be alive", name)
		}
	}
}

func TestCapture(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Start("test-cap", t.TempDir(), "echo 'hello world' && sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-cap", true) })

	// Wait for echo to execute and tmux to capture output
	time.Sleep(500 * time.Millisecond)

	output, err := mgr.Capture("test-cap", 50)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if !strings.Contains(output, "hello world") {
		t.Errorf("capture output should contain 'hello world', got: %q", output)
	}
}

func TestInject(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a session running cat which echoes stdin back
	err := mgr.Start("test-inj", t.TempDir(), "cat", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-inj", true) })

	// Wait for cat to start
	time.Sleep(300 * time.Millisecond)

	err = mgr.Inject("test-inj", "test message", false)
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	// Wait for the injected text to appear in the pane
	time.Sleep(300 * time.Millisecond)

	output, err := mgr.Capture("test-inj", 50)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if !strings.Contains(output, "test message") {
		t.Errorf("capture output should contain 'test message', got: %q", output)
	}
}

func TestHealthHealthy(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a session that outputs text periodically
	err := mgr.Start("test-hh", t.TempDir(), "while true; do echo tick; sleep 1; done", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-hh", true) })

	// Wait for output to appear
	time.Sleep(500 * time.Millisecond)

	status, err := mgr.Health("test-hh", 30*time.Minute)
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	if status != Healthy {
		t.Errorf("expected Healthy, got %s", status)
	}
}

func TestHealthDead(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Start("test-hd", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-hd", true) })

	// Let tmux stabilize then force-kill the session
	time.Sleep(300 * time.Millisecond)

	// Kill the tmux session directly to simulate dead session
	kill, killCancel := tmuxCmd("kill-session", "-t", "test-hd")
	_ = kill.Run()
	killCancel()

	// Let tmux process the kill
	time.Sleep(200 * time.Millisecond)

	status, err := mgr.Health("test-hd", 30*time.Minute)
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	if status != Dead {
		t.Errorf("expected Dead, got %s", status)
	}
}

func TestExists(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	if mgr.Exists("nonexistent") {
		t.Fatal("Exists should return false for nonexistent session")
	}

	err := mgr.Start("test-ex", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-ex", true) })

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	if !mgr.Exists("test-ex") {
		t.Fatal("Exists should return true for existing session")
	}
}

func TestMetadata(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	workDir := t.TempDir()
	err := mgr.Start("test-meta", workDir, "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	// Verify metadata file exists with correct content
	metaFile := metadataPath("test-meta")
	data, err := os.ReadFile(metaFile)
	if err != nil {
		t.Fatalf("metadata file should exist: %v", err)
	}

	var meta sessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("failed to parse metadata: %v", err)
	}

	if meta.Name != "test-meta" {
		t.Errorf("expected name 'test-meta', got %q", meta.Name)
	}
	if meta.Role != "outpost" {
		t.Errorf("expected role 'outpost', got %q", meta.Role)
	}
	if meta.World != "haven" {
		t.Errorf("expected world 'haven', got %q", meta.World)
	}
	if meta.WorkDir != workDir {
		t.Errorf("expected workdir %q, got %q", workDir, meta.WorkDir)
	}
	if meta.StartedAt.IsZero() {
		t.Error("started_at should not be zero")
	}

	// Stop session and verify metadata file is removed
	err = mgr.Stop("test-meta", true)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if _, err := os.Stat(metaFile); !os.IsNotExist(err) {
		t.Error("metadata file should be removed after Stop")
	}
}

func TestDoubleStart(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Start("test-ds", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-ds", true) })

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	err = mgr.Start("test-ds", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err == nil {
		t.Fatal("second Start should fail for duplicate session name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestStopNonexistent(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Stop("nonexistent", true)
	if err == nil {
		t.Fatal("Stop should fail for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestCaptureNonexistent(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	_, err := mgr.Capture("nonexistent", 50)
	if err == nil {
		t.Fatal("Capture should fail for nonexistent session")
	}
}

func TestInjectNonexistent(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Inject("nonexistent", "hello", true)
	if err == nil {
		t.Fatal("Inject should fail for nonexistent session")
	}
}

func TestHealthStatusStrings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status   HealthStatus
		str      string
		exitCode int
	}{
		{Healthy, "healthy", 0},
		{Dead, "dead", 1},
		{AgentDead, "agent-dead", 2},
		{Hung, "hung", 2},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.String(); got != tt.str {
				t.Errorf("String() = %q, want %q", got, tt.str)
			}
			if got := tt.status.ExitCode(); got != tt.exitCode {
				t.Errorf("ExitCode() = %d, want %d", got, tt.exitCode)
			}
		})
	}
}

func TestEnvVars(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	env := map[string]string{
		"MY_VAR": "hello",
		"OTHER":  "world",
	}

	err := mgr.Start("test-env", t.TempDir(), "sleep 300", env, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start with env vars failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-env", true) })

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	// Verify the session was created (env vars are set but we can't easily read them
	// through tmux without executing a command; we verify no error was returned)
	if !mgr.Exists("test-env") {
		t.Fatal("session with env vars should exist")
	}
}

func TestPrependEnv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cmd      string
		env      map[string]string
		expected string
	}{
		{
			name:     "nil env",
			cmd:      "sleep 300",
			env:      nil,
			expected: "sleep 300",
		},
		{
			name:     "empty env",
			cmd:      "sleep 300",
			env:      map[string]string{},
			expected: "sleep 300",
		},
		{
			name: "single var",
			cmd:  "sleep 300",
			env:  map[string]string{"MY_VAR": "hello"},
			expected: `export MY_VAR="hello" && sleep 300`,
		},
		{
			name: "multiple vars sorted",
			cmd:  "sleep 300",
			env: map[string]string{
				"ZZZ_VAR": "last",
				"AAA_VAR": "first",
			},
			expected: `export AAA_VAR="first" ZZZ_VAR="last" && sleep 300`,
		},
		{
			name: "value with spaces",
			cmd:  "sleep 300",
			env:  map[string]string{"PATH_VAR": "/some/path with spaces"},
			expected: `export PATH_VAR="/some/path with spaces" && sleep 300`,
		},
		{
			name: "value with special chars",
			cmd:  "sleep 300",
			env:  map[string]string{"SPECIAL": `has "quotes" and $vars`},
			expected: `export SPECIAL="has \"quotes\" and \$vars" && sleep 300`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := prependEnv(tt.cmd, tt.env)
			if got != tt.expected {
				t.Errorf("prependEnv(%q, %v) = %q, want %q", tt.cmd, tt.env, got, tt.expected)
			}
		})
	}
}

func TestStartPrependsEnvToCommand(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	env := map[string]string{
		"TEST_SOL_VAR": "from_prepend",
	}

	// Start a session that prints the env var. If prependEnv works,
	// the export runs before echo, making the var available.
	err := mgr.Start("test-env-prepend", t.TempDir(),
		"echo VAR_IS_$TEST_SOL_VAR && sleep 300", env, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-env-prepend", true) })

	// Wait for echo to execute
	time.Sleep(500 * time.Millisecond)

	output, err := mgr.Capture("test-env-prepend", 50)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if !strings.Contains(output, "VAR_IS_from_prepend") {
		t.Errorf("env var not visible in process, capture: %q", output)
	}
}

func TestCyclePrependsEnvToCommand(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start without env.
	err := mgr.Start("test-cycle-prepend", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-cycle-prepend", true) })

	time.Sleep(300 * time.Millisecond)

	env := map[string]string{
		"CYCLE_VAR": "from_cycle",
	}

	// Cycle with env — the new command should see the env var.
	err = mgr.Cycle("test-cycle-prepend", t.TempDir(),
		"echo CYCLE_IS_$CYCLE_VAR && sleep 300", env, "outpost", "haven")
	if err != nil {
		t.Fatalf("Cycle failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	output, err := mgr.Capture("test-cycle-prepend", 50)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if !strings.Contains(output, "CYCLE_IS_from_cycle") {
		t.Errorf("env var not visible after cycle, capture: %q", output)
	}
}

func TestGracefulStop(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Start("test-gs", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	// Graceful stop: sends C-c first, then kills after timeout
	err = mgr.Stop("test-gs", false)
	if err != nil {
		t.Fatalf("graceful Stop failed: %v", err)
	}

	// Let tmux process the kill
	time.Sleep(200 * time.Millisecond)

	if mgr.Exists("test-gs") {
		t.Fatal("session should not exist after graceful Stop")
	}

	// Verify metadata is cleaned up
	if _, err := os.Stat(metadataPath("test-gs")); !os.IsNotExist(err) {
		t.Error("metadata file should be removed after Stop")
	}
}

func TestListEmpty(t *testing.T) {
	mgr := setupTest(t)

	sessions, err := mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestListWithStoppedSession(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Start("test-ls", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-ls", true) })

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	// Kill tmux session directly without going through Stop (simulates crash)
	kill, killCancel := tmuxCmd("kill-session", "-t", "test-ls")
	_ = kill.Run()
	killCancel()

	// Let tmux process the kill
	time.Sleep(200 * time.Millisecond)

	sessions, err := mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Find "test-ls" specifically — other parallel tests may also have sessions.
	var found *SessionInfo
	for i := range sessions {
		if sessions[i].Name == "test-ls" {
			found = &sessions[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected orphaned metadata for 'test-ls' in List()")
	}
	if found.Alive {
		t.Error("session should not be alive after tmux kill")
	}
}

func TestHealthAgentDead(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a session with a command that survives startup verification
	// (1.5s) but exits shortly after. sleep 2 dies at 2s — past the
	// verification window, so Start() succeeds.
	err := mgr.Start("test-ad", t.TempDir(), "sleep 2", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-ad", true) })

	// Wait for the process to exit after startup verification.
	// Start() already consumed 1.5s; sleep 2 exits at 2s from creation.
	time.Sleep(1 * time.Second)

	// The session might still exist (tmux default is to close window when
	// process exits). If it does, check for AgentDead; if not, Dead is ok too.
	status, err := mgr.Health("test-ad", 30*time.Minute)
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	// Either Dead (session gone) or AgentDead (pane dead) is acceptable
	if status != Dead && status != AgentDead {
		t.Errorf("expected Dead or AgentDead, got %s", status)
	}
}

func TestStartDeadOnStartup(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// A command that exits immediately should be caught by startup verification.
	err := mgr.Start("test-dead-startup", t.TempDir(), "echo done", nil, "outpost", "haven")
	if err == nil {
		t.Fatal("Start should fail for a command that dies immediately")
	}
	if !strings.Contains(err.Error(), "process died during startup") {
		t.Errorf("error should mention process died during startup, got: %v", err)
	}

	// Session and metadata should be cleaned up.
	if mgr.Exists("test-dead-startup") {
		t.Error("session should not exist after dead-on-startup cleanup")
	}
	if _, err := os.Stat(metadataPath("test-dead-startup")); !os.IsNotExist(err) {
		t.Error("metadata should be removed after dead-on-startup cleanup")
	}
}

func TestCycleDeadOnStartup(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a long-lived session.
	err := mgr.Start("test-cycle-dead-startup", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-cycle-dead-startup", true) })

	// Let tmux stabilize.
	time.Sleep(300 * time.Millisecond)

	// Cycle to a command that exits immediately — should fail startup verification.
	err = mgr.Cycle("test-cycle-dead-startup", t.TempDir(), "echo done", nil, "outpost", "haven")
	if err == nil {
		t.Fatal("Cycle should fail for a command that dies immediately")
	}
	if !strings.Contains(err.Error(), "process died during startup") {
		t.Errorf("error should mention process died during startup, got: %v", err)
	}
}

func TestGetMeta(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Session not found: GetMeta should return nil, nil.
	meta, err := mgr.GetMeta("nonexistent-get-meta")
	if err != nil {
		t.Fatalf("GetMeta for nonexistent session should not error: %v", err)
	}
	if meta != nil {
		t.Errorf("GetMeta for nonexistent session should return nil, got: %+v", meta)
	}

	// Start a session and verify GetMeta returns correct metadata.
	workDir := t.TempDir()
	err = mgr.Start("test-get-meta", workDir, "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-get-meta", true) })

	// Let tmux stabilize.
	time.Sleep(300 * time.Millisecond)

	meta, err = mgr.GetMeta("test-get-meta")
	if err != nil {
		t.Fatalf("GetMeta failed: %v", err)
	}
	if meta == nil {
		t.Fatal("GetMeta should return non-nil for existing session")
	}
	if meta.Name != "test-get-meta" {
		t.Errorf("expected name 'test-get-meta', got %q", meta.Name)
	}
	if meta.Role != "outpost" {
		t.Errorf("expected role 'outpost', got %q", meta.Role)
	}
	if meta.World != "haven" {
		t.Errorf("expected world 'haven', got %q", meta.World)
	}
	if meta.WorkDir != workDir {
		t.Errorf("expected workdir %q, got %q", workDir, meta.WorkDir)
	}
	if !meta.Alive {
		t.Error("GetMeta for live session should have Alive=true")
	}
	if meta.StartedAt.IsZero() {
		t.Error("GetMeta StartedAt should not be zero")
	}
}

func TestMultipleStartStop(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start, stop, then start again with same name
	err := mgr.Start("test-ms", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("first Start failed: %v", err)
	}

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	err = mgr.Stop("test-ms", true)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Let tmux process the kill
	time.Sleep(200 * time.Millisecond)

	// Should be able to start again with same name
	err = mgr.Start("test-ms", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("second Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-ms", true) })

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	if !mgr.Exists("test-ms") {
		t.Fatal("session should exist after re-start")
	}
}

func TestSessionInfoJSON(t *testing.T) {
	t.Parallel()
	// Test that SessionInfo serializes correctly to JSON
	info := SessionInfo{
		Name:      "test",
		PID:       12345,
		Role:      "outpost",
		World:     "haven",
		WorkDir:   os.TempDir(),
		StartedAt: time.Date(2026, 2, 25, 10, 30, 0, 0, time.UTC),
		Alive:     true,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal SessionInfo: %v", err)
	}

	var decoded SessionInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal SessionInfo: %v", err)
	}

	if decoded.Name != info.Name || decoded.PID != info.PID || decoded.Role != info.Role {
		t.Errorf("SessionInfo roundtrip mismatch: got %+v", decoded)
	}
}

func TestHealthHung(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a session that just sleeps (no output changes)
	err := mgr.Start("test-hung", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-hung", true) })

	// Wait for session to stabilize
	time.Sleep(500 * time.Millisecond)

	// First health check — writes initial hash
	status, err := mgr.Health("test-hung", 1*time.Nanosecond)
	if err != nil {
		t.Fatalf("first Health failed: %v", err)
	}
	if status != Healthy {
		t.Errorf("first check should be Healthy (no previous hash), got %s", status)
	}

	// Wait a tiny bit to ensure timestamp moves forward
	time.Sleep(10 * time.Millisecond)

	// Second health check — same content, maxInactivity is 1ns so it should be Hung
	status, err = mgr.Health("test-hung", 1*time.Nanosecond)
	if err != nil {
		t.Fatalf("second Health failed: %v", err)
	}
	if status != Hung {
		// Log hash file content for debugging
		data, _ := os.ReadFile(captureHashPath("test-hung"))
		t.Errorf("expected Hung, got %s (hash file: %s)", status, string(data))
	}
}

func TestStartCreatesSessionsDir(t *testing.T) {
	mgr := setupTest(t)

	// sessions dir shouldn't exist yet
	dir := sessionsDir()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("sessions dir should not exist before Start")
	}

	err := mgr.Start("test-dir", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Sessions dir should now exist
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("sessions dir should exist after Start: %v", err)
	}
}

func TestSessionNameInErrors(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Test that error messages include session name
	_, err := mgr.Capture("my-special-session", 50)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "my-special-session") {
		t.Errorf("error should mention session name, got: %v", err)
	}

	err = mgr.Inject("my-special-session", "hello", true)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "my-special-session") {
		t.Errorf("error should mention session name, got: %v", err)
	}

	err = mgr.Stop("my-special-session", true)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "my-special-session") {
		t.Errorf("error should mention session name, got: %v", err)
	}
}

func BenchmarkExists(b *testing.B) {
	tmpDir := b.TempDir()
	b.Setenv("TMUX_TMPDIR", tmpDir)
	b.Setenv("TMUX", "")
	b.Setenv("SOL_HOME", filepath.Join(tmpDir, "sol"))

	mgr := New()

	b.Cleanup(func() {
		kill, killCancel := tmuxCmd("kill-server")
		_ = kill.Run()
		killCancel()
	})

	_ = mgr.Start("bench", b.TempDir(), "sleep 300", nil, "outpost", "haven")
	time.Sleep(300 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.Exists("bench")
	}
}

func TestStopCleansMetadataOnKillFailure(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Start("test-meta-clean", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	// Verify metadata file exists
	metaFile := metadataPath("test-meta-clean")
	if _, err := os.Stat(metaFile); err != nil {
		t.Fatalf("metadata file should exist after Start: %v", err)
	}

	// Kill the session directly via raw tmux command, bypassing the manager.
	// This means the subsequent mgr.Stop will find the session already dead.
	kill, killCancel := tmuxCmd("kill-session", "-t", "test-meta-clean")
	_ = kill.Run()
	killCancel()

	// Let tmux process the kill
	time.Sleep(200 * time.Millisecond)

	// Call Stop — the kill-session will fail (session already dead),
	// but metadata should still be cleaned up.
	err = mgr.Stop("test-meta-clean", true)
	if err == nil {
		t.Fatal("Stop should return error for already-dead session")
	}

	// Verify metadata file is removed despite kill failure.
	if _, err := os.Stat(metaFile); !os.IsNotExist(err) {
		t.Error("metadata file should be removed even when session is already dead")
	}

	// Verify List does not include the session.
	sessions, err := mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for _, s := range sessions {
		if s.Name == "test-meta-clean" {
			t.Error("session should not appear in List after Stop cleans metadata")
		}
	}
}

func TestCycle(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a session running sleep.
	err := mgr.Start("test-cycle", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-cycle", true) })

	// Let tmux stabilize.
	time.Sleep(300 * time.Millisecond)

	if !mgr.Exists("test-cycle") {
		t.Fatal("session should exist before Cycle")
	}

	// Cycle to a new command.
	err = mgr.Cycle("test-cycle", t.TempDir(), "sleep 600", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Cycle failed: %v", err)
	}

	// Let tmux process the respawn.
	time.Sleep(300 * time.Millisecond)

	// Session should still exist with the new process.
	if !mgr.Exists("test-cycle") {
		t.Fatal("session should still exist after Cycle")
	}

	// Verify metadata was updated.
	data, err := os.ReadFile(metadataPath("test-cycle"))
	if err != nil {
		t.Fatalf("metadata should exist after Cycle: %v", err)
	}
	var meta sessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("failed to parse metadata: %v", err)
	}
	if meta.Name != "test-cycle" {
		t.Errorf("expected name test-cycle, got %q", meta.Name)
	}
}

func TestCycleNonexistent(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Cycle("nonexistent", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err == nil {
		t.Fatal("Cycle should fail for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestCycleWithEnv(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Start("test-cycle-env", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-cycle-env", true) })

	time.Sleep(300 * time.Millisecond)

	env := map[string]string{
		"SOL_HOME":  "/tmp/sol",
		"SOL_WORLD": "haven",
	}
	err = mgr.Cycle("test-cycle-env", t.TempDir(), "sleep 600", env, "outpost", "haven")
	if err != nil {
		t.Fatalf("Cycle with env failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	if !mgr.Exists("test-cycle-env") {
		t.Fatal("session should exist after Cycle with env")
	}
}

func TestUnknownHealthStatus(t *testing.T) {
	t.Parallel()
	s := HealthStatus(99)
	expected := fmt.Sprintf("unknown(%d)", 99)
	if s.String() != expected {
		t.Errorf("expected %q, got %q", expected, s.String())
	}
}

// --- sanitizeNudgeMessage tests ---

func TestSanitizeNudgeMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain text", "hello world", "hello world"},
		{"preserves newlines", "line1\nline2", "line1\nline2"},
		{"strips ESC", "before\x1b[31mred\x1b[0m after", "before[31mred[0m after"},
		{"strips CR", "hello\r\nworld", "hello\nworld"},
		{"strips BS", "abc\x08def", "abcdef"},
		{"tab to space", "col1\tcol2", "col1 col2"},
		{"strips DEL", "abc\x7fdef", "abcdef"},
		{"preserves unicode", "hello 世界 ❯", "hello 世界 ❯"},
		{"preserves quotes", `he said "hello"`, `he said "hello"`},
		{"empty string", "", ""},
		{"strips NUL", "a\x00b", "ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeNudgeMessage(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeNudgeMessage(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// --- nudge lock tests ---

func TestNudgeLockAcquireRelease(t *testing.T) {
	t.Parallel()
	session := "test-lock-session"

	// Acquire should succeed.
	if !acquireNudgeLock(session, 1*time.Second) {
		t.Fatal("first acquire should succeed")
	}

	// Second acquire should fail (timeout quickly).
	if acquireNudgeLock(session, 50*time.Millisecond) {
		t.Fatal("second acquire should fail while lock is held")
	}

	// Release.
	releaseNudgeLock(session)

	// Now acquire should succeed again.
	if !acquireNudgeLock(session, 1*time.Second) {
		t.Fatal("acquire after release should succeed")
	}
	releaseNudgeLock(session)
}

func TestNudgeLockDifferentSessions(t *testing.T) {
	t.Parallel()
	sess1 := "lock-test-a"
	sess2 := "lock-test-b"

	if !acquireNudgeLock(sess1, 1*time.Second) {
		t.Fatal("acquire sess1 should succeed")
	}

	// Different session should not be blocked.
	if !acquireNudgeLock(sess2, 1*time.Second) {
		t.Fatal("acquire sess2 should succeed while sess1 is locked")
	}

	releaseNudgeLock(sess1)
	releaseNudgeLock(sess2)
}

// --- Idle detection unit tests ---

func TestMatchesPromptPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		line   string
		prefix string
		want   bool
	}{
		{"exact match", "❯ ", "❯ ", true},
		{"prompt with trailing text", "❯ hello", "❯ ", true},
		{"prompt only char", "❯", "❯ ", true},
		{"leading whitespace", "  ❯ ", "❯ ", true},
		{"NBSP after prompt", "❯\u00a0", "❯ ", true},
		{"NBSP in prefix config", "❯ ", "❯\u00a0", true},
		{"no match", "some other text", "❯ ", false},
		{"empty line", "", "❯ ", false},
		{"empty prefix", "❯ ", "", false},
		{"partial match", "❯", "❯ hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := matchesPromptPrefix(tt.line, tt.prefix)
			if got != tt.want {
				t.Errorf("matchesPromptPrefix(%q, %q) = %v, want %v",
					tt.line, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestLinesContainPrompt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		lines []string
		want  bool
	}{
		{
			"prompt on last line",
			[]string{"output line 1", "output line 2", "❯ "},
			true,
		},
		{
			"prompt in middle of lines",
			[]string{"output line 1", "❯ ", "status bar info"},
			true,
		},
		{
			"no prompt",
			[]string{"output line 1", "output line 2", "still working..."},
			false,
		},
		{
			"empty lines only",
			[]string{"", "  ", ""},
			false,
		},
		{
			"prompt with leading whitespace",
			[]string{"", "   ❯ ", ""},
			true,
		},
		{
			"prompt with NBSP",
			[]string{"❯\u00a0"},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := linesContainPrompt(tt.lines)
			if got != tt.want {
				t.Errorf("linesContainPrompt(%v) = %v, want %v", tt.lines, got, tt.want)
			}
		})
	}
}

func TestLinesAreBusy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		lines []string
		want  bool
	}{
		{
			"busy with esc to interrupt",
			[]string{"❯ ", "⏵⏵ running tool · esc to interrupt"},
			true,
		},
		{
			"not busy - normal status bar",
			[]string{"❯ ", "⏵⏵ bypass permissions on (shift+tab) · 1 file"},
			false,
		},
		{
			"not busy - no status bar",
			[]string{"some output", "❯ "},
			false,
		},
		{
			"busy text in middle of lines",
			[]string{"line 1", "esc to interrupt", "line 3"},
			true,
		},
		{
			"empty lines",
			[]string{"", "", ""},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := linesAreBusy(tt.lines)
			if got != tt.want {
				t.Errorf("linesAreBusy(%v) = %v, want %v", tt.lines, got, tt.want)
			}
		})
	}
}

func TestDefaultPromptPrefix(t *testing.T) {
	t.Parallel()
	if DefaultPromptPrefix == "" {
		t.Error("DefaultPromptPrefix should not be empty")
	}
	if !strings.Contains(DefaultPromptPrefix, "❯") {
		t.Errorf("DefaultPromptPrefix = %q, want to contain ❯", DefaultPromptPrefix)
	}
}

func TestErrIdleTimeout(t *testing.T) {
	t.Parallel()
	if ErrIdleTimeout == nil {
		t.Fatal("ErrIdleTimeout should not be nil")
	}
	if !errors.Is(ErrIdleTimeout, ErrIdleTimeout) {
		t.Error("ErrIdleTimeout should be identifiable with errors.Is")
	}
}

// --- NudgeSession integration tests ---

func TestNudgeSessionDelivers(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a session running cat which echoes stdin back.
	err := mgr.Start("test-nudge", t.TempDir(), "cat", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-nudge", true) })

	time.Sleep(300 * time.Millisecond)

	err = mgr.NudgeSession("test-nudge", "hello from nudge")
	if err != nil {
		t.Fatalf("NudgeSession failed: %v", err)
	}

	// Give time for the text + enter to be processed.
	time.Sleep(500 * time.Millisecond)

	output, err := mgr.Capture("test-nudge", 50)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if !strings.Contains(output, "hello from nudge") {
		t.Errorf("capture should contain nudged text, got: %q", output)
	}
}

func TestNudgeSessionNonexistent(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.NudgeSession("nonexistent", "hello")
	if err == nil {
		t.Fatal("NudgeSession should fail for nonexistent session")
	}
}

func TestNudgeSessionSanitizes(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Start("test-nudge-san", t.TempDir(), "cat", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-nudge-san", true) })

	time.Sleep(300 * time.Millisecond)

	// Message with control characters — should be sanitized, not crash.
	err = mgr.NudgeSession("test-nudge-san", "hello\x1b[31m\rworld\x08!")
	if err != nil {
		t.Fatalf("NudgeSession with control chars failed: %v", err)
	}
}

// --- WaitForIdle integration tests ---

func TestWaitForIdleDetectsPrompt(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a session that prints the prompt character then sleeps.
	// The prompt character appears in the pane, simulating an idle Claude Code.
	err := mgr.Start("test-idle", t.TempDir(),
		"printf '\\n❯ ' && sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-idle", true) })

	// Wait for printf to execute
	time.Sleep(500 * time.Millisecond)

	err = mgr.WaitForIdle("test-idle", 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForIdle should detect idle prompt: %v", err)
	}
}

func TestWaitForIdleTimeout(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a session that never shows the prompt — just sleeps.
	err := mgr.Start("test-idle-to", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-idle-to", true) })

	time.Sleep(300 * time.Millisecond)

	err = mgr.WaitForIdle("test-idle-to", 600*time.Millisecond)
	if !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("expected ErrIdleTimeout, got: %v", err)
	}
}

func TestWaitForIdleNonexistentSession(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.WaitForIdle("nonexistent", 5*time.Second)
	if err == nil {
		t.Fatal("WaitForIdle should fail for nonexistent session")
	}
	if errors.Is(err, ErrIdleTimeout) {
		t.Error("should return session-not-found error, not ErrIdleTimeout")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestWaitForIdleBusySession(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a session that shows both prompt and "esc to interrupt" —
	// simulating Claude Code actively running a tool while prompt is visible.
	err := mgr.Start("test-idle-busy", t.TempDir(),
		`printf '\n❯ \n⏵⏵ running tool · esc to interrupt\n' && sleep 300`,
		nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-idle-busy", true) })

	time.Sleep(500 * time.Millisecond)

	// Should timeout because "esc to interrupt" means busy.
	err = mgr.WaitForIdle("test-idle-busy", 600*time.Millisecond)
	if !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("expected ErrIdleTimeout for busy session, got: %v", err)
	}
}

func TestWaitForIdleTransientPrompt(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Simulate transient prompt: show prompt briefly, then clear the pane
	// and print non-prompt output. This tests that a prompt appearing only
	// once (one poll) doesn't satisfy the 2-consecutive-poll requirement.
	// We use clear to wipe the tmux pane buffer, then print enough lines
	// to push the prompt out of the 5-line capture window.
	err := mgr.Start("test-idle-transient", t.TempDir(),
		`printf '\n❯ \n' && sleep 0.1 && clear && echo working1 && echo working2 && echo working3 && echo working4 && echo working5 && sleep 300`,
		nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-idle-transient", true) })

	// Wait for the script to show the prompt, clear, and print non-prompt output
	time.Sleep(800 * time.Millisecond)

	// By now the prompt has been cleared and replaced — should timeout.
	err = mgr.WaitForIdle("test-idle-transient", 600*time.Millisecond)
	if !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("expected ErrIdleTimeout for transient prompt, got: %v", err)
	}
}

// --- IsAtPrompt tests ---

func TestIsAtPromptTrue(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Start("test-prompt-t", t.TempDir(),
		"printf '\\n❯ ' && sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-prompt-t", true) })

	time.Sleep(500 * time.Millisecond)

	if !mgr.IsAtPrompt("test-prompt-t") {
		t.Error("IsAtPrompt should return true when prompt is visible")
	}
}

func TestIsAtPromptFalse(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.Start("test-prompt-f", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-prompt-f", true) })

	time.Sleep(300 * time.Millisecond)

	if mgr.IsAtPrompt("test-prompt-f") {
		t.Error("IsAtPrompt should return false when prompt is not visible")
	}
}

func TestIsAtPromptNonexistent(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	if mgr.IsAtPrompt("nonexistent") {
		t.Error("IsAtPrompt should return false for nonexistent session")
	}
}

func TestWaitForIdleResetOnBusy(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a session that shows prompt with "esc to interrupt" on a separate line.
	// This tests that the consecutive counter resets when busy is detected.
	err := mgr.Start("test-idle-reset", t.TempDir(),
		`printf '\n❯ \nesc to interrupt\n' && sleep 300`,
		nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-idle-reset", true) })

	time.Sleep(500 * time.Millisecond)

	// Should timeout: "esc to interrupt" in captured lines means busy,
	// which resets the consecutive idle counter every poll.
	err = mgr.WaitForIdle("test-idle-reset", 600*time.Millisecond)
	if !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("expected ErrIdleTimeout when busy indicator present, got: %v", err)
	}
}

func TestCountSessionsNoSessions(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Use a prefix that won't match any sessions created by other parallel tests.
	count, err := mgr.CountSessions("count-nosess-xyz-")
	if err != nil {
		t.Fatalf("CountSessions returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestCountSessionsSomeMatching(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Create sessions with a unique prefix for this test.
	prefix := "count-some-"
	matching := []string{prefix + "a", prefix + "b", prefix + "c"}
	other := "other-count-x"

	for _, name := range append(matching, other) {
		workdir := t.TempDir()
		err := mgr.Start(name, workdir, "sleep 300", nil, "outpost", "haven")
		if err != nil {
			t.Fatalf("Start %s failed: %v", name, err)
		}
		t.Cleanup(func() { _ = mgr.Stop(name, true) })
		time.Sleep(200 * time.Millisecond) // let each session stabilize
	}

	count, err := mgr.CountSessions(prefix)
	if err != nil {
		t.Fatalf("CountSessions returned error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 matching sessions, got %d", count)
	}
}

func TestCountSessionsNoneMatching(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Create a session that does NOT match the prefix we'll query.
	err := mgr.Start("count-none-other", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("count-none-other", true) })

	time.Sleep(300 * time.Millisecond)

	count, err := mgr.CountSessions("count-none-nomatch-")
	if err != nil {
		t.Fatalf("CountSessions returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestCountSessionsTmuxNotRunning(t *testing.T) {
	// This test uses an isolated TMUX_TMPDIR with no tmux server
	// to verify CountSessions returns 0 when tmux is not running.
	tmpDir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", tmpDir)
	t.Setenv("TMUX", "")

	mgr := New()
	count, err := mgr.CountSessions("sol-")
	if err != nil {
		t.Fatalf("expected no error when tmux not running, got: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 when tmux not running, got %d", count)
	}
}
