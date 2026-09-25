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
	"unicode/utf8"
)

// waitFor polls cond every 25ms until it returns true or timeout elapses.
// Fails the test with msg if timeout is hit. Returns roughly the same
// failure semantics as a hardcoded sleep, but exits as soon as the
// condition is met.
func waitFor(t testing.TB, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("waitFor timeout (%s): %s", timeout, msg)
}

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
	// Isolate HOME so TrustDirectory writes to a fresh ~/.claude/claude.json
	// rather than the user's real one. This prevents unbounded growth of the
	// trust file across test counts, which would otherwise cause flock contention.
	os.Setenv("HOME", filepath.Join(tmpDir, "home"))

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-ss") })

	if !mgr.Exists("test-ss") {
		t.Fatal("session should exist after Start")
	}

	err = mgr.Stop("test-ss", true)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	waitFor(t, 5*time.Second, "session to stop", func() bool { return !mgr.Exists("test-ss") })

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
		waitFor(t, 5*time.Second, "session to exist", func() bool { return mgr.Exists(name) })
	}

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

	waitFor(t, 5*time.Second, "echo output to appear", func() bool {
		out, _ := mgr.Capture("test-cap", 50)
		return strings.Contains(out, "hello world")
	})

	output, err := mgr.Capture("test-cap", 50)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if !strings.Contains(output, "hello world") {
		t.Errorf("capture output should contain 'hello world', got: %q", output)
	}
}

func TestCaptureEscapes(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// printf with an ANSI color escape so we can confirm -e passthrough
	// preserves it, unlike plain Capture.
	err := mgr.Start("test-cap-esc", t.TempDir(), `printf '\033[31mhello red\033[0m\n' && sleep 300`, nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-cap-esc", true) })

	waitFor(t, 5*time.Second, "colored output to appear", func() bool {
		out, _ := mgr.CaptureEscapes("test-cap-esc", 50)
		return strings.Contains(out, "hello red")
	})

	output, err := mgr.CaptureEscapes("test-cap-esc", 50)
	if err != nil {
		t.Fatalf("CaptureEscapes failed: %v", err)
	}

	if !strings.Contains(output, "hello red") {
		t.Errorf("capture output should contain 'hello red', got: %q", output)
	}
	if !strings.Contains(output, "\x1b[") {
		t.Errorf("CaptureEscapes output should retain ANSI escape sequences, got: %q", output)
	}

	// Plain Capture on the same session should NOT contain escape sequences.
	plain, err := mgr.Capture("test-cap-esc", 50)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("Capture output should not contain ANSI escape sequences, got: %q", plain)
	}
}

func TestCaptureEscapesNonexistent(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	_, err := mgr.CaptureEscapes("nonexistent", 50)
	if err == nil {
		t.Fatal("CaptureEscapes should fail for nonexistent session")
	}
}

func TestStageText(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	// Start a session running cat which echoes stdin back
	err := mgr.Start("test-stage", t.TempDir(), "cat", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-stage", true) })

	waitFor(t, 5*time.Second, "cat session to start", func() bool { return mgr.Exists("test-stage") })

	err = mgr.StageText("test-stage", "test message")
	if err != nil {
		t.Fatalf("StageText failed: %v", err)
	}

	waitFor(t, 5*time.Second, "staged text to appear in output", func() bool {
		out, _ := mgr.Capture("test-stage", 50)
		return strings.Contains(out, "test message")
	})

	output, err := mgr.Capture("test-stage", 50)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if !strings.Contains(output, "test message") {
		t.Errorf("capture output should contain 'test message', got: %q", output)
	}
}

func TestStageTextNonexistent(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	err := mgr.StageText("nonexistent", "hello")
	if err == nil {
		t.Fatal("StageText should fail for nonexistent session")
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

	waitFor(t, 5*time.Second, "tick output to appear", func() bool {
		out, _ := mgr.Capture("test-hh", 50)
		return strings.Contains(out, "tick")
	})

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-hd") })

	// Kill the tmux session directly to simulate dead session
	kill, killCancel := tmuxCmd("kill-session", "-t", "test-hd")
	_ = kill.Run()
	killCancel()

	waitFor(t, 5*time.Second, "session to disappear after kill", func() bool { return !mgr.Exists("test-hd") })

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-ex") })

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-meta") })

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-ds") })

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
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error should wrap ErrNotFound, got: %v", err)
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention session name, got: %v", err)
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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-env") })

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

	waitFor(t, 5*time.Second, "env var to appear in output", func() bool {
		out, _ := mgr.Capture("test-env-prepend", 50)
		return strings.Contains(out, "VAR_IS_from_prepend")
	})

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

	waitFor(t, 5*time.Second, "initial session to start", func() bool { return mgr.Exists("test-cycle-prepend") })

	env := map[string]string{
		"CYCLE_VAR": "from_cycle",
	}

	// Cycle with env — the new command should see the env var.
	err = mgr.Cycle("test-cycle-prepend", t.TempDir(),
		"echo CYCLE_IS_$CYCLE_VAR && sleep 300", env, "outpost", "haven")
	if err != nil {
		t.Fatalf("Cycle failed: %v", err)
	}

	waitFor(t, 5*time.Second, "cycle env var to appear in output", func() bool {
		out, _ := mgr.Capture("test-cycle-prepend", 50)
		return strings.Contains(out, "CYCLE_IS_from_cycle")
	})

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-gs") })

	// Graceful stop: sends C-c first, then kills after timeout
	err = mgr.Stop("test-gs", false)
	if err != nil {
		t.Fatalf("graceful Stop failed: %v", err)
	}

	waitFor(t, 5*time.Second, "session to stop", func() bool { return !mgr.Exists("test-gs") })

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-ls") })

	// Kill tmux session directly without going through Stop (simulates crash)
	kill, killCancel := tmuxCmd("kill-session", "-t", "test-ls")
	_ = kill.Run()
	killCancel()

	waitFor(t, 5*time.Second, "session to disappear after kill", func() bool { return !mgr.Exists("test-ls") })

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
	// Cannot poll: AgentDead leaves the session open so !Exists() would
	// time out falsely, and Health() has side effects that corrupt the check.
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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-cycle-dead-startup") })

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-get-meta") })

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

	waitFor(t, 5*time.Second, "first session to start", func() bool { return mgr.Exists("test-ms") })

	err = mgr.Stop("test-ms", true)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	waitFor(t, 5*time.Second, "first session to stop", func() bool { return !mgr.Exists("test-ms") })

	// Should be able to start again with same name
	err = mgr.Start("test-ms", t.TempDir(), "sleep 300", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("second Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-ms", true) })

	waitFor(t, 5*time.Second, "second session to start", func() bool { return mgr.Exists("test-ms") })

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-hung") })

	// First health check — writes initial hash
	status, err := mgr.Health("test-hung", 1*time.Nanosecond)
	if err != nil {
		t.Fatalf("first Health failed: %v", err)
	}
	if status != Healthy {
		t.Errorf("first check should be Healthy (no previous hash), got %s", status)
	}

	// Advance real time past the 1ns inactivity threshold — polling cannot substitute for elapsed time.
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
	// Use a unique SOL_HOME so the sessions dir is guaranteed to not exist,
	// even in -count=3 runs where a previous count may have created it.
	t.Setenv("SOL_HOME", t.TempDir())
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
	t.Cleanup(func() { _ = mgr.Stop("test-dir", true) })

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

	err = mgr.NudgeSession("my-special-session", "hello")
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
	waitFor(b, 5*time.Second, "bench session to start", func() bool { return mgr.Exists("bench") })

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-meta-clean") })

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

	waitFor(t, 5*time.Second, "session to disappear after kill", func() bool { return !mgr.Exists("test-meta-clean") })

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-cycle") })

	if !mgr.Exists("test-cycle") {
		t.Fatal("session should exist before Cycle")
	}

	// Cycle to a new command.
	err = mgr.Cycle("test-cycle", t.TempDir(), "sleep 600", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Cycle failed: %v", err)
	}

	waitFor(t, 5*time.Second, "session to exist after cycle", func() bool { return mgr.Exists("test-cycle") })

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-cycle-env") })

	env := map[string]string{
		"SOL_HOME":  "/tmp/sol",
		"SOL_WORLD": "haven",
	}
	err = mgr.Cycle("test-cycle-env", t.TempDir(), "sleep 600", env, "outpost", "haven")
	if err != nil {
		t.Fatalf("Cycle with env failed: %v", err)
	}

	waitFor(t, 5*time.Second, "session to exist after cycle with env", func() bool { return mgr.Exists("test-cycle-env") })

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

// --- chunksByRune tests ---

func TestChunksByRuneASCII(t *testing.T) {
	t.Parallel()
	// 10 ASCII bytes with chunkSize=4 → ["abcd", "efgh", "ij"]
	chunks := chunksByRune("abcdefghij", 4)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %v", len(chunks), chunks)
	}
	if chunks[0] != "abcd" || chunks[1] != "efgh" || chunks[2] != "ij" {
		t.Errorf("unexpected chunks: %v", chunks)
	}
}

func TestChunksByRuneEmpty(t *testing.T) {
	t.Parallel()
	chunks := chunksByRune("", 512)
	if len(chunks) != 0 {
		t.Errorf("expected no chunks for empty string, got %v", chunks)
	}
}

func TestChunksByRuneNoSplit(t *testing.T) {
	t.Parallel()
	text := "short"
	chunks := chunksByRune(text, 512)
	if len(chunks) != 1 || chunks[0] != text {
		t.Errorf("expected single chunk %q, got %v", text, chunks)
	}
}

func TestChunksByRuneRuneBoundary(t *testing.T) {
	t.Parallel()
	// Build a string where a 3-byte rune (✓, U+2713) straddles byte offset 4
	// when using naive byte slicing at chunkSize=4.
	// "AAAA✓" = 4 ASCII bytes + 3 UTF-8 bytes = 7 bytes total.
	// With chunkSize=4 the rune-aware chunker must NOT split "✓" across chunks.
	text := "AAAA✓"
	chunks := chunksByRune(text, 4)

	// All chunks must be valid UTF-8.
	for i, c := range chunks {
		if !utf8.ValidString(c) {
			t.Errorf("chunk[%d] %q is not valid UTF-8", i, c)
		}
	}
	// The "✓" rune is 3 bytes; it doesn't fit in the first chunk alongside "AAAA"
	// (4+3=7 > 4), so it must start a new chunk.
	if chunks[0] != "AAAA" {
		t.Errorf("chunk[0] = %q, want %q", chunks[0], "AAAA")
	}
	if chunks[1] != "✓" {
		t.Errorf("chunk[1] = %q, want %q", chunks[1], "✓")
	}
	// Reconstructed text must equal original.
	if got := strings.Join(chunks, ""); got != text {
		t.Errorf("joined chunks %q != original %q", got, text)
	}
}

func TestChunksByRuneEmojiNearBoundary(t *testing.T) {
	t.Parallel()
	// 4-byte emoji (🎉 = U+1F389) near the 512-byte boundary.
	// Fill 510 ASCII bytes, then append 🎉 (4 bytes) = 514 bytes total.
	// Naive byte slicing at 512 would cut 🎉 in half.
	prefix := strings.Repeat("a", 510)
	text := prefix + "🎉"
	chunks := chunksByRune(text, 512)

	for i, c := range chunks {
		if !utf8.ValidString(c) {
			t.Errorf("chunk[%d] is not valid UTF-8: %q", i, c)
		}
	}
	if strings.Join(chunks, "") != text {
		t.Errorf("joined chunks do not equal original text")
	}
	// The emoji must land in its own chunk (not split with the prefix).
	last := chunks[len(chunks)-1]
	if last != "🎉" {
		t.Errorf("last chunk = %q, want emoji %q", last, "🎉")
	}
}

func TestChunksByRuneOversizedSingleRune(t *testing.T) {
	t.Parallel()
	// A single 4-byte emoji with chunkSize=2 (smaller than one rune).
	// Must still make progress (one rune per chunk) rather than looping forever.
	chunks := chunksByRune("🎉", 2)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for oversized rune, got %d: %v", len(chunks), chunks)
	}
	if chunks[0] != "🎉" {
		t.Errorf("chunk = %q, want %q", chunks[0], "🎉")
	}
}

func TestChunksByRuneAllChunksValidUTF8(t *testing.T) {
	t.Parallel()
	// Mixed ASCII + multi-byte runes at various positions.
	text := "hello 世界 ✓ step done 🎉 end"
	chunks := chunksByRune(text, 8)
	for i, c := range chunks {
		if !utf8.ValidString(c) {
			t.Errorf("chunk[%d] %q is not valid UTF-8", i, c)
		}
	}
	if strings.Join(chunks, "") != text {
		t.Errorf("joined chunks do not equal original text")
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
		{"NBSP after prompt", "❯ ", "❯ ", true},
		{"NBSP in prefix config", "❯ ", "❯ ", true},
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
			[]string{"❯ "},
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

	waitFor(t, 5*time.Second, "cat session to start", func() bool { return mgr.Exists("test-nudge") })

	err = mgr.NudgeSession("test-nudge", "hello from nudge")
	if err != nil {
		t.Fatalf("NudgeSession failed: %v", err)
	}

	waitFor(t, 5*time.Second, "nudged text to appear in output", func() bool {
		out, _ := mgr.Capture("test-nudge", 50)
		return strings.Contains(out, "hello from nudge")
	})

	output, err := mgr.Capture("test-nudge", 50)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if !strings.Contains(output, "hello from nudge") {
		t.Errorf("capture should contain nudged text, got: %q", output)
	}
}

// TestNudgeSessionDoorbellVerifiesTrivially exercises the doorbell nudges
// writ's "verification-path interplay" case: the fixed nudge.DoorbellMessage
// literal ("[sol] pending messages: run sol nudge drain, then continue your
// current work") is short — well under sendKeysChunkSize, so it never needs
// chunking — which means the pane-capture verification NudgeSession performs
// after Enter should succeed on the first attempt against a simple echoing
// fixture, with no retries needed.
//
// The literal is duplicated here rather than imported from internal/nudge:
// internal/nudge imports internal/session, so an internal (white-box, same
// package name) test file in internal/session importing internal/nudge back
// would create an import cycle. Keep this string in sync with
// nudge.DoorbellMessage if that constant ever changes.
func TestNudgeSessionDoorbellVerifiesTrivially(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	const doorbellMessage = "[sol] pending messages: run sol nudge drain, then continue your current work"

	// Start a session running cat which echoes stdin back.
	err := mgr.Start("test-nudge-doorbell", t.TempDir(), "cat", nil, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("test-nudge-doorbell", true) })

	waitFor(t, 5*time.Second, "cat session to start", func() bool { return mgr.Exists("test-nudge-doorbell") })

	err = mgr.NudgeSession("test-nudge-doorbell", doorbellMessage)
	if err != nil {
		t.Fatalf("NudgeSession failed to deliver the doorbell: %v", err)
	}

	waitFor(t, 5*time.Second, "doorbell text to appear in output", func() bool {
		out, _ := mgr.Capture("test-nudge-doorbell", 50)
		return strings.Contains(out, doorbellMessage)
	})

	output, err := mgr.Capture("test-nudge-doorbell", 50)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}
	if !strings.Contains(output, doorbellMessage) {
		t.Errorf("capture should contain the doorbell text, got: %q", output)
	}
	// Single line, no chunking: the doorbell should never be split across
	// multiple pane lines by chunked delivery.
	if strings.Count(doorbellMessage, "\n") != 0 {
		t.Fatalf("test literal must be single-line to match nudge.DoorbellMessage's invariant")
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

	waitFor(t, 5*time.Second, "cat session to start", func() bool { return mgr.Exists("test-nudge-san") })

	// Message with control characters — should be sanitized, not crash.
	err = mgr.NudgeSession("test-nudge-san", "hello\x1b[31m\rworld\x08!")
	if err != nil {
		t.Fatalf("NudgeSession with control chars failed: %v", err)
	}
}

// escapeDetectScript is a raw-mode fixture ("stty raw -echo") that logs
// "ESCAPE" to $LOGFILE any time it receives a bare ESC (0x1b) byte — the
// terminal encoding of the "Escape" key tmux send-keys would emit. Every
// other byte is echoed back onto the ❯ input line as it arrives, and on
// Enter the pane is cleared and redrawn back to a bare idle prompt (so
// NudgeSession's post-Enter verification succeeds immediately with no
// retries, keeping the byte stream deterministic and short).
//
// sanitizeNudgeMessage strips all control characters below 0x20 (including
// ESC) from message content before it is typed, so an ESC byte reaching
// this fixture cannot originate from the message text itself — it could
// only come from an explicit "Escape" key send by NudgeSession.
const escapeDetectScript = "#!/usr/bin/env bash\n" +
	"stty raw -echo\n" +
	"printf '\\xe2\\x9d\\xaf '\n" +
	"while IFS= read -r -n1 c; do\n" +
	"  if [ -z \"$c\" ]; then c=$'\\n'; fi\n" +
	"  if [ \"$c\" = $'\\e' ]; then\n" +
	"    echo ESCAPE >> \"$LOGFILE\"\n" +
	"  fi\n" +
	"  if [ \"$c\" = $'\\r' ] || [ \"$c\" = $'\\n' ]; then\n" +
	"    clear\n" +
	"    printf '\\xe2\\x9d\\xaf \\n'\n" +
	"  else\n" +
	"    printf '%s' \"$c\"\n" +
	"  fi\n" +
	"done\n"

// TestNudgeSessionNeverSendsEscape is the delivery-path assertion for the
// doorbell-wedge fix (writ: doorbell interrupts working agents, 2026-08-26):
// NudgeSession must never send a literal Escape key, since Escape
// interrupts an in-flight turn on the Claude Code REPL (the root cause of
// the incident this writ closes) rather than doing anything useful — sol
// never nudges a shell pane, so there is no vim-mode case left to guard
// against. This is a live, code-level assertion (not just a grep): it
// exercises the real NudgeSession send-keys path against a fixture that
// can distinguish an actual Escape keypress from message content.
func TestNudgeSessionNeverSendsEscape(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	script := writeFakePaneScript(t, escapeDetectScript)
	logfile := filepath.Join(t.TempDir(), "escape.log")
	name := "test-nudge-no-escape"
	err := mgr.Start(name, t.TempDir(), script, map[string]string{"LOGFILE": logfile}, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(name, true) })
	waitFor(t, 5*time.Second, "fake pane prompt to render", func() bool { return mgr.IsAtPrompt(name) })

	err = mgr.NudgeSession(name, "hello escape check")
	if err != nil {
		t.Fatalf("NudgeSession failed: %v", err)
	}

	// Absence, not presence: no log file at all (fixture never wrote
	// "ESCAPE") is success. os.IsNotExist on read is treated the same as an
	// empty file — either way, no Escape byte was ever observed.
	data, readErr := os.ReadFile(logfile)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("failed to read escape log: %v", readErr)
	}
	if strings.Contains(string(data), "ESCAPE") {
		t.Fatalf("NudgeSession sent a literal Escape key — this is exactly the wedge bug the fix removes")
	}
}

// --- verificationFragment / lastNonBlankLine unit tests ---

func TestVerificationFragment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"short message returned whole", "hello", "hello"},
		{"long message trims to trailing runes", strings.Repeat("a", 30), strings.Repeat("a", nudgeVerifyFragmentLen)},
		{"trailing newline ignored", "hello\n", "hello"},
		{"trailing whitespace ignored", "hello   ", "hello"},
		{"multiline uses last non-blank line", "line one\nline two", "line two"},
		{"multiline with trailing blank line", "line one\n\n\n", "line one"},
		{"whitespace-only message", "   \n\t\n", ""},
		{"empty message", "", ""},
		{"long last line of multiline message trims", "short\n" + strings.Repeat("b", 30), strings.Repeat("b", nudgeVerifyFragmentLen)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := verificationFragment(tt.input)
			if got != tt.want {
				t.Errorf("verificationFragment(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLastNonBlankLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{"all blank", []string{"", "  ", "\t"}, ""},
		{"single line", []string{"hello"}, "hello"},
		{"trailing blanks ignored", []string{"hello", "", "", ""}, "hello"},
		{"picks last non-blank, not first", []string{"first", "second", ""}, "second"},
		{"trims whitespace", []string{"  hello  "}, "hello"},
		{"empty slice", []string{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := lastNonBlankLine(tt.lines)
			if got != tt.want {
				t.Errorf("lastNonBlankLine(%v) = %q, want %q", tt.lines, got, tt.want)
			}
		})
	}
}

// --- Enter-verification integration tests (fake pane fixtures) ---
//
// These fixtures are small raw-mode ("stty raw -echo") shell scripts that
// stand in for a TUI's input box: they echo typed characters as they
// arrive (simulating live display of staged text) and decide for
// themselves, on each Enter, whether to treat it as a submit or a swallow —
// something a plain cooked-mode shell (e.g. `cat`) cannot simulate, because
// the tty's own line discipline would echo the Enter as a real newline
// regardless of what the "app" wants.

// swallowScript redraws the buffered text on the ❯ input line unchanged (as
// if Enter never happened) for the first $SWALLOW_N Enters, then "delivers"
// — clears the input line back to a bare idle prompt. Each Enter processed
// is also appended to $LOGFILE, one line per Enter, giving the test a
// rendering-independent way to confirm how many Enters the fixture actually
// saw (pane content is deliberately NOT used for that — see busyQueueScript
// for why relying on accumulated pane text is fragile).
//
// clear runs before every redraw so each capture reflects a single
// self-consistent frame rather than an appended-to scrollback the next
// frame might partially overwrite.
const swallowScript = "#!/usr/bin/env bash\n" +
	"stty raw -echo\n" +
	"SWALLOW_N=\"${SWALLOW_N:-2}\"\n" +
	"buf=\"\"\n" +
	"count=0\n" +
	"while IFS= read -r -n1 c; do\n" +
	"  if [ -z \"$c\" ]; then c=$'\\n'; fi\n" +
	"  if [ \"$c\" = $'\\r' ] || [ \"$c\" = $'\\n' ]; then\n" +
	"    count=$((count+1))\n" +
	"    echo \"$count\" >> \"$LOGFILE\"\n" +
	"    clear\n" +
	"    if [ \"$count\" -le \"$SWALLOW_N\" ]; then\n" +
	"      printf '\\xe2\\x9d\\xaf %s' \"$buf\"\n" +
	"    else\n" +
	"      printf '\\xe2\\x9d\\xaf \\n'\n" +
	"      buf=\"\"\n" +
	"    fi\n" +
	"  else\n" +
	"    buf+=\"$c\"\n" +
	"    printf '%s' \"$c\"\n" +
	"  fi\n" +
	"done\n"

// busyQueueScript never delivers — every Enter redraws the buffered text on
// the ❯ input line, tagged "(queued)", with an "esc to interrupt" busy
// marker on the line above. This simulates Claude Code mid-turn with the
// message queued (visible, not yet consumed) rather than swallowed. Enters
// are logged to $LOGFILE the same way as swallowScript.
const busyQueueScript = "#!/usr/bin/env bash\n" +
	"stty raw -echo\n" +
	"buf=\"\"\n" +
	"count=0\n" +
	"while IFS= read -r -n1 c; do\n" +
	"  if [ -z \"$c\" ]; then c=$'\\n'; fi\n" +
	"  if [ \"$c\" = $'\\r' ] || [ \"$c\" = $'\\n' ]; then\n" +
	"    count=$((count+1))\n" +
	"    echo \"$count\" >> \"$LOGFILE\"\n" +
	"    clear\n" +
	"    printf 'esc to interrupt\\r\\n\\xe2\\x9d\\xaf %s (queued)\\r\\n' \"$buf\"\n" +
	"  else\n" +
	"    buf+=\"$c\"\n" +
	"    printf '%s' \"$c\"\n" +
	"  fi\n" +
	"done\n"

// writeFakePaneScript writes a fake-pane fixture script to a fresh temp
// file and returns its path. The file is executable.
func writeFakePaneScript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake_pane.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("failed to write fake pane script: %v", err)
	}
	return path
}

// readEnterLog reads the fixture's $LOGFILE and returns the number of
// Enters it recorded. Missing file (fixture never got an Enter) counts as 0.
func readEnterLog(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("failed to read enter log: %v", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func TestNudgeSessionRetriesOnSwallowedEnterThenSucceeds(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	script := writeFakePaneScript(t, swallowScript)
	logfile := filepath.Join(t.TempDir(), "enter.log")
	name := "test-nudge-swallow-retry"
	err := mgr.Start(name, t.TempDir(), script,
		map[string]string{"SWALLOW_N": "2", "LOGFILE": logfile}, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(name, true) })
	waitFor(t, 5*time.Second, "fake pane to start", func() bool { return mgr.Exists(name) })

	err = mgr.NudgeSession(name, "hello swallow test")
	if err != nil {
		t.Fatalf("NudgeSession should succeed after retries, got error: %v", err)
	}

	// SWALLOW_N=2 means the fixture swallows Enters 1 and 2 and delivers on
	// the 3rd — NudgeSession should have retried exactly that many times.
	if got := readEnterLog(t, logfile); got != 3 {
		t.Errorf("expected 3 Enters (2 swallowed + 1 delivered), fixture recorded %d", got)
	}
}

func TestNudgeSessionErrorsWhenAlwaysSwallowed(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	script := writeFakePaneScript(t, swallowScript)
	logfile := filepath.Join(t.TempDir(), "enter.log")
	name := "test-nudge-swallow-fail"
	err := mgr.Start(name, t.TempDir(), script,
		map[string]string{"SWALLOW_N": "99", "LOGFILE": logfile}, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(name, true) })
	waitFor(t, 5*time.Second, "fake pane to start", func() bool { return mgr.Exists(name) })

	err = mgr.NudgeSession(name, "hello swallow test")
	if err == nil {
		t.Fatal("expected NudgeSession to fail when Enter is always swallowed")
	}
	if !strings.Contains(err.Error(), "not submitted") {
		t.Errorf("expected an explicit staged-but-not-submitted error, got: %v", err)
	}

	// All 3 attempts should have been swallowed (fixture never reaches
	// SWALLOW_N=99), and NudgeSession gives up after exactly 3.
	if got := readEnterLog(t, logfile); got != 3 {
		t.Errorf("expected exactly 3 verified attempts before giving up, fixture recorded %d", got)
	}
}

func TestNudgeSessionQueuedWhileBusyCountsAsDelivered(t *testing.T) {
	t.Parallel()
	mgr := setupTest(t)

	script := writeFakePaneScript(t, busyQueueScript)
	logfile := filepath.Join(t.TempDir(), "enter.log")
	name := "test-nudge-busy-queue"
	err := mgr.Start(name, t.TempDir(), script, map[string]string{"LOGFILE": logfile}, "outpost", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(name, true) })
	waitFor(t, 5*time.Second, "fake pane to start", func() bool { return mgr.Exists(name) })

	err = mgr.NudgeSession(name, "hello swallow test")
	if err != nil {
		t.Fatalf("expected queued-while-busy to count as delivered, got error: %v", err)
	}

	// The fixture never actually delivers — it always shows the message as
	// queued alongside the busy marker. A queued-while-busy message is
	// treated as delivered on the FIRST post-Enter check, so NudgeSession
	// should not have retried at all.
	if got := readEnterLog(t, logfile); got != 1 {
		t.Errorf("expected exactly 1 Enter (queued-while-busy is delivered on first check), fixture recorded %d", got)
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

	waitFor(t, 5*time.Second, "prompt to appear in output", func() bool {
		out, _ := mgr.Capture("test-idle", 50)
		return strings.Contains(out, "❯")
	})

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-idle-to") })

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

	waitFor(t, 5*time.Second, "busy indicator to appear in output", func() bool {
		out, _ := mgr.Capture("test-idle-busy", 50)
		return strings.Contains(out, "esc to interrupt")
	})

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

	// Wait for the script to show the prompt, clear, and print non-prompt output.
	waitFor(t, 5*time.Second, "transient prompt phase to complete", func() bool {
		out, _ := mgr.Capture("test-idle-transient", 50)
		return strings.Contains(out, "working5")
	})

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

	waitFor(t, 5*time.Second, "prompt to appear in output", func() bool {
		out, _ := mgr.Capture("test-prompt-t", 50)
		return strings.Contains(out, "❯")
	})

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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("test-prompt-f") })

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

	waitFor(t, 5*time.Second, "busy indicator to appear in output", func() bool {
		out, _ := mgr.Capture("test-idle-reset", 50)
		return strings.Contains(out, "esc to interrupt")
	})

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
		waitFor(t, 5*time.Second, "session to stabilize", func() bool { return mgr.Exists(name) })
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

	waitFor(t, 5*time.Second, "session to start", func() bool { return mgr.Exists("count-none-other") })

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
