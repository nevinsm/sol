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

// setupTest creates an isolated tmux environment for a single test.
// It sets TMUX_TMPDIR to a test-specific temp directory so the tmux socket
// doesn't collide with any running tmux server. It also sets SOL_HOME so
// session metadata is written to a test-specific directory.
func setupTest(t *testing.T) *Manager {
	t.Helper()

	tmpDir := t.TempDir()

	// Isolate tmux socket
	t.Setenv("TMUX_TMPDIR", tmpDir)
	// Unset TMUX to avoid "sessions should be nested" errors
	t.Setenv("TMUX", "")

	// Isolate SOL_HOME for session metadata
	solHome := filepath.Join(tmpDir, "sol")
	t.Setenv("SOL_HOME", solHome)

	t.Cleanup(func() {
		// Kill all sessions in this isolated tmux server
		kill, killCancel := tmuxCmd("kill-server")
		_ = kill.Run()
		killCancel()
	})

	return New()
}

func TestStartStop(t *testing.T) {
	mgr := setupTest(t)

	err := mgr.Start("test-ss", "/tmp", "sleep 300", nil, "agent", "haven")
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
	mgr := setupTest(t)

	names := []string{"list-a", "list-b", "list-c"}
	for _, name := range names {
		err := mgr.Start(name, "/tmp", "sleep 300", nil, "agent", "haven")
		if err != nil {
			t.Fatalf("Start %s failed: %v", name, err)
		}
	}

	// Let tmux stabilize after creating all sessions
	time.Sleep(500 * time.Millisecond)

	sessions, err := mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}

	found := make(map[string]bool)
	for _, s := range sessions {
		found[s.Name] = true
		if !s.Alive {
			t.Errorf("session %s should be alive", s.Name)
		}
	}
	for _, name := range names {
		if !found[name] {
			t.Errorf("session %s not found in list", name)
		}
	}
}

func TestCapture(t *testing.T) {
	mgr := setupTest(t)

	err := mgr.Start("test-cap", "/tmp", "echo 'hello world' && sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

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
	mgr := setupTest(t)

	// Start a session running cat which echoes stdin back
	err := mgr.Start("test-inj", "/tmp", "cat", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

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
	mgr := setupTest(t)

	// Start a session that outputs text periodically
	err := mgr.Start("test-hh", "/tmp", "while true; do echo tick; sleep 1; done", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

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
	mgr := setupTest(t)

	err := mgr.Start("test-hd", "/tmp", "sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

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
	mgr := setupTest(t)

	if mgr.Exists("nonexistent") {
		t.Fatal("Exists should return false for nonexistent session")
	}

	err := mgr.Start("test-ex", "/tmp", "sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	if !mgr.Exists("test-ex") {
		t.Fatal("Exists should return true for existing session")
	}
}

func TestMetadata(t *testing.T) {
	mgr := setupTest(t)

	err := mgr.Start("test-meta", "/tmp", "sleep 300", nil, "agent", "haven")
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
	if meta.Role != "agent" {
		t.Errorf("expected role 'agent', got %q", meta.Role)
	}
	if meta.World != "haven" {
		t.Errorf("expected world 'haven', got %q", meta.World)
	}
	if meta.WorkDir != "/tmp" {
		t.Errorf("expected workdir '/tmp', got %q", meta.WorkDir)
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
	mgr := setupTest(t)

	err := mgr.Start("test-ds", "/tmp", "sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("first Start failed: %v", err)
	}

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	err = mgr.Start("test-ds", "/tmp", "sleep 300", nil, "agent", "haven")
	if err == nil {
		t.Fatal("second Start should fail for duplicate session name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestStopNonexistent(t *testing.T) {
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
	mgr := setupTest(t)

	_, err := mgr.Capture("nonexistent", 50)
	if err == nil {
		t.Fatal("Capture should fail for nonexistent session")
	}
}

func TestInjectNonexistent(t *testing.T) {
	mgr := setupTest(t)

	err := mgr.Inject("nonexistent", "hello", true)
	if err == nil {
		t.Fatal("Inject should fail for nonexistent session")
	}
}

func TestHealthStatusStrings(t *testing.T) {
	tests := []struct {
		status   HealthStatus
		str      string
		exitCode int
	}{
		{Healthy, "healthy", 0},
		{Dead, "dead", 1},
		{AgentDead, "agent-dead", 2},
		{Hung, "hung", 3},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
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
	mgr := setupTest(t)

	env := map[string]string{
		"MY_VAR": "hello",
		"OTHER":  "world",
	}

	err := mgr.Start("test-env", "/tmp", "sleep 300", env, "agent", "haven")
	if err != nil {
		t.Fatalf("Start with env vars failed: %v", err)
	}

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	// Verify the session was created (env vars are set but we can't easily read them
	// through tmux without executing a command; we verify no error was returned)
	if !mgr.Exists("test-env") {
		t.Fatal("session with env vars should exist")
	}
}

func TestGracefulStop(t *testing.T) {
	mgr := setupTest(t)

	err := mgr.Start("test-gs", "/tmp", "sleep 300", nil, "agent", "haven")
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
	mgr := setupTest(t)

	err := mgr.Start("test-ls", "/tmp", "sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

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

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session (orphaned metadata), got %d", len(sessions))
	}

	if sessions[0].Alive {
		t.Error("session should not be alive after tmux kill")
	}
}

func TestHealthAgentDead(t *testing.T) {
	mgr := setupTest(t)

	// Start a session with a command that survives startup verification
	// (1.5s) but exits shortly after. sleep 2 dies at 2s — past the
	// verification window, so Start() succeeds.
	err := mgr.Start("test-ad", "/tmp", "sleep 2", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

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
	mgr := setupTest(t)

	// A command that exits immediately should be caught by startup verification.
	err := mgr.Start("test-dead-startup", "/tmp", "echo done", nil, "agent", "haven")
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

func TestMultipleStartStop(t *testing.T) {
	mgr := setupTest(t)

	// Start, stop, then start again with same name
	err := mgr.Start("test-ms", "/tmp", "sleep 300", nil, "agent", "haven")
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
	err = mgr.Start("test-ms", "/tmp", "sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("second Start failed: %v", err)
	}

	// Let tmux stabilize
	time.Sleep(300 * time.Millisecond)

	if !mgr.Exists("test-ms") {
		t.Fatal("session should exist after re-start")
	}
}

func TestSessionInfoJSON(t *testing.T) {
	// Test that SessionInfo serializes correctly to JSON
	info := SessionInfo{
		Name:      "test",
		PID:       12345,
		Role:      "agent",
		World:     "haven",
		WorkDir:   "/tmp",
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
	mgr := setupTest(t)

	// Start a session that just sleeps (no output changes)
	err := mgr.Start("test-hung", "/tmp", "sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

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

	err := mgr.Start("test-dir", "/tmp", "sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Sessions dir should now exist
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("sessions dir should exist after Start: %v", err)
	}
}

func TestSessionNameInErrors(t *testing.T) {
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

	_ = mgr.Start("bench", "/tmp", "sleep 300", nil, "agent", "haven")
	time.Sleep(300 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.Exists("bench")
	}
}

func TestStopCleansMetadataOnKillFailure(t *testing.T) {
	mgr := setupTest(t)

	err := mgr.Start("test-meta-clean", "/tmp", "sleep 300", nil, "agent", "haven")
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
	mgr := setupTest(t)

	// Start a session running sleep.
	err := mgr.Start("test-cycle", "/tmp", "sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let tmux stabilize.
	time.Sleep(300 * time.Millisecond)

	if !mgr.Exists("test-cycle") {
		t.Fatal("session should exist before Cycle")
	}

	// Cycle to a new command.
	err = mgr.Cycle("test-cycle", "/tmp", "sleep 600", nil, "agent", "haven")
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
	mgr := setupTest(t)

	err := mgr.Cycle("nonexistent", "/tmp", "sleep 300", nil, "agent", "haven")
	if err == nil {
		t.Fatal("Cycle should fail for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestCycleWithEnv(t *testing.T) {
	mgr := setupTest(t)

	err := mgr.Start("test-cycle-env", "/tmp", "sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	env := map[string]string{
		"SOL_HOME":  "/tmp/sol",
		"SOL_WORLD": "haven",
	}
	err = mgr.Cycle("test-cycle-env", "/tmp", "sleep 600", env, "agent", "haven")
	if err != nil {
		t.Fatalf("Cycle with env failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	if !mgr.Exists("test-cycle-env") {
		t.Fatal("session should exist after Cycle with env")
	}
}

func TestUnknownHealthStatus(t *testing.T) {
	s := HealthStatus(99)
	expected := fmt.Sprintf("unknown(%d)", 99)
	if s.String() != expected {
		t.Errorf("expected %q, got %q", expected, s.String())
	}
}

// --- sanitizeNudgeMessage tests ---

func TestSanitizeNudgeMessage(t *testing.T) {
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
			got := sanitizeNudgeMessage(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeNudgeMessage(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// --- nudge lock tests ---

func TestNudgeLockAcquireRelease(t *testing.T) {
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
			got := matchesPromptPrefix(tt.line, tt.prefix)
			if got != tt.want {
				t.Errorf("matchesPromptPrefix(%q, %q) = %v, want %v",
					tt.line, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestLinesContainPrompt(t *testing.T) {
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
			got := linesContainPrompt(tt.lines)
			if got != tt.want {
				t.Errorf("linesContainPrompt(%v) = %v, want %v", tt.lines, got, tt.want)
			}
		})
	}
}

func TestLinesAreBusy(t *testing.T) {
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
			got := linesAreBusy(tt.lines)
			if got != tt.want {
				t.Errorf("linesAreBusy(%v) = %v, want %v", tt.lines, got, tt.want)
			}
		})
	}
}

func TestDefaultPromptPrefix(t *testing.T) {
	if DefaultPromptPrefix == "" {
		t.Error("DefaultPromptPrefix should not be empty")
	}
	if !strings.Contains(DefaultPromptPrefix, "❯") {
		t.Errorf("DefaultPromptPrefix = %q, want to contain ❯", DefaultPromptPrefix)
	}
}

func TestErrIdleTimeout(t *testing.T) {
	if ErrIdleTimeout == nil {
		t.Fatal("ErrIdleTimeout should not be nil")
	}
	if !errors.Is(ErrIdleTimeout, ErrIdleTimeout) {
		t.Error("ErrIdleTimeout should be identifiable with errors.Is")
	}
}

// --- NudgeSession integration tests ---

func TestNudgeSessionDelivers(t *testing.T) {
	mgr := setupTest(t)

	// Start a session running cat which echoes stdin back.
	err := mgr.Start("test-nudge", "/tmp", "cat", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

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
	mgr := setupTest(t)

	err := mgr.NudgeSession("nonexistent", "hello")
	if err == nil {
		t.Fatal("NudgeSession should fail for nonexistent session")
	}
}

func TestNudgeSessionSanitizes(t *testing.T) {
	mgr := setupTest(t)

	err := mgr.Start("test-nudge-san", "/tmp", "cat", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	// Message with control characters — should be sanitized, not crash.
	err = mgr.NudgeSession("test-nudge-san", "hello\x1b[31m\rworld\x08!")
	if err != nil {
		t.Fatalf("NudgeSession with control chars failed: %v", err)
	}
}

// --- WaitForIdle integration tests ---

func TestWaitForIdleDetectsPrompt(t *testing.T) {
	mgr := setupTest(t)

	// Start a session that prints the prompt character then sleeps.
	// The prompt character appears in the pane, simulating an idle Claude Code.
	err := mgr.Start("test-idle", "/tmp",
		"printf '\\n❯ ' && sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for printf to execute
	time.Sleep(500 * time.Millisecond)

	err = mgr.WaitForIdle("test-idle", 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForIdle should detect idle prompt: %v", err)
	}
}

func TestWaitForIdleTimeout(t *testing.T) {
	mgr := setupTest(t)

	// Start a session that never shows the prompt — just sleeps.
	err := mgr.Start("test-idle-to", "/tmp", "sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	err = mgr.WaitForIdle("test-idle-to", 600*time.Millisecond)
	if !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("expected ErrIdleTimeout, got: %v", err)
	}
}

func TestWaitForIdleNonexistentSession(t *testing.T) {
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
	mgr := setupTest(t)

	// Start a session that shows both prompt and "esc to interrupt" —
	// simulating Claude Code actively running a tool while prompt is visible.
	err := mgr.Start("test-idle-busy", "/tmp",
		`printf '\n❯ \n⏵⏵ running tool · esc to interrupt\n' && sleep 300`,
		nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Should timeout because "esc to interrupt" means busy.
	err = mgr.WaitForIdle("test-idle-busy", 600*time.Millisecond)
	if !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("expected ErrIdleTimeout for busy session, got: %v", err)
	}
}

func TestWaitForIdleTransientPrompt(t *testing.T) {
	mgr := setupTest(t)

	// Simulate transient prompt: show prompt briefly, then clear the pane
	// and print non-prompt output. This tests that a prompt appearing only
	// once (one poll) doesn't satisfy the 2-consecutive-poll requirement.
	// We use clear to wipe the tmux pane buffer, then print enough lines
	// to push the prompt out of the 5-line capture window.
	err := mgr.Start("test-idle-transient", "/tmp",
		`printf '\n❯ \n' && sleep 0.1 && clear && echo working1 && echo working2 && echo working3 && echo working4 && echo working5 && sleep 300`,
		nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

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
	mgr := setupTest(t)

	err := mgr.Start("test-prompt-t", "/tmp",
		"printf '\\n❯ ' && sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if !mgr.IsAtPrompt("test-prompt-t") {
		t.Error("IsAtPrompt should return true when prompt is visible")
	}
}

func TestIsAtPromptFalse(t *testing.T) {
	mgr := setupTest(t)

	err := mgr.Start("test-prompt-f", "/tmp", "sleep 300", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	if mgr.IsAtPrompt("test-prompt-f") {
		t.Error("IsAtPrompt should return false when prompt is not visible")
	}
}

func TestIsAtPromptNonexistent(t *testing.T) {
	mgr := setupTest(t)

	if mgr.IsAtPrompt("nonexistent") {
		t.Error("IsAtPrompt should return false for nonexistent session")
	}
}

func TestWaitForIdleResetOnBusy(t *testing.T) {
	mgr := setupTest(t)

	// Start a session that shows prompt with "esc to interrupt" on a separate line.
	// This tests that the consecutive counter resets when busy is detected.
	err := mgr.Start("test-idle-reset", "/tmp",
		`printf '\n❯ \nesc to interrupt\n' && sleep 300`,
		nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Should timeout: "esc to interrupt" in captured lines means busy,
	// which resets the consecutive idle counter every poll.
	err = mgr.WaitForIdle("test-idle-reset", 600*time.Millisecond)
	if !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("expected ErrIdleTimeout when busy indicator present, got: %v", err)
	}
}
