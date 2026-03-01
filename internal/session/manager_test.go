package session

import (
	"encoding/json"
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

	err = mgr.Inject("test-inj", "test message")
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

	err := mgr.Inject("nonexistent", "hello")
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

	// Start a session with a command that exits immediately
	err := mgr.Start("test-ad", "/tmp", "echo done", nil, "agent", "haven")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for the command to exit and tmux to mark pane as dead.
	// tmux keeps the pane with remain-on-exit if set, otherwise session
	// may disappear. We use a short-lived command to test AgentDead.
	time.Sleep(500 * time.Millisecond)

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

	err = mgr.Inject("my-special-session", "hello")
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

func TestUnknownHealthStatus(t *testing.T) {
	s := HealthStatus(99)
	expected := fmt.Sprintf("unknown(%d)", 99)
	if s.String() != expected {
		t.Errorf("expected %q, got %q", expected, s.String())
	}
}
