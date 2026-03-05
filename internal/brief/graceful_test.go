package brief

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// --- Mock ---

type mockManager struct {
	existsFn  func(string) bool
	injectFn  func(string, string, bool) error
	captureFn func(string, int) (string, error)
	stopFn    func(string, bool) error

	injectCalls []string
	stopCalls   []bool // force flag for each stop call
}

func (m *mockManager) Exists(name string) bool {
	if m.existsFn != nil {
		return m.existsFn(name)
	}
	return true
}

func (m *mockManager) Inject(name, text string, submit bool) error {
	m.injectCalls = append(m.injectCalls, text)
	if m.injectFn != nil {
		return m.injectFn(name, text, submit)
	}
	return nil
}

func (m *mockManager) Capture(name string, lines int) (string, error) {
	if m.captureFn != nil {
		return m.captureFn(name, lines)
	}
	return "", nil
}

func (m *mockManager) Stop(name string, force bool) error {
	m.stopCalls = append(m.stopCalls, force)
	if m.stopFn != nil {
		return m.stopFn(name, force)
	}
	return nil
}

// --- Tests ---

func TestGracefulStop_NoBriefDir(t *testing.T) {
	mgr := &mockManager{}
	briefDir := filepath.Join(t.TempDir(), "nonexistent", ".brief")

	err := gracefulStop("sol-test-gov", briefDir, mgr, time.Millisecond, 4, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have called Stop immediately, no Inject.
	if len(mgr.injectCalls) != 0 {
		t.Errorf("expected no inject calls, got %d", len(mgr.injectCalls))
	}
	if len(mgr.stopCalls) != 1 {
		t.Fatalf("expected 1 stop call, got %d", len(mgr.stopCalls))
	}
	if !mgr.stopCalls[0] {
		t.Error("expected force=true on stop")
	}
}

func TestGracefulStop_SessionStabilizes(t *testing.T) {
	tmp := t.TempDir()
	briefDir := filepath.Join(tmp, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Captures: 2 changing, then stabilize.
	var idx int32
	captures := []string{"output-1", "output-2", "stable", "stable", "stable", "stable", "stable"}

	mgr := &mockManager{
		captureFn: func(name string, lines int) (string, error) {
			i := int(atomic.AddInt32(&idx, 1)) - 1
			if i >= len(captures) {
				return captures[len(captures)-1], nil
			}
			return captures[i], nil
		},
	}

	err := gracefulStop("sol-test-gov", briefDir, mgr, time.Millisecond, 4, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have injected the stop prompt.
	if len(mgr.injectCalls) != 1 {
		t.Fatalf("expected 1 inject call, got %d", len(mgr.injectCalls))
	}
	if mgr.injectCalls[0] != StopPrompt {
		t.Errorf("inject text = %q, want StopPrompt", mgr.injectCalls[0])
	}

	// Should have stopped after stabilization.
	if len(mgr.stopCalls) != 1 {
		t.Fatalf("expected 1 stop call, got %d", len(mgr.stopCalls))
	}
	if !mgr.stopCalls[0] {
		t.Error("expected force=true on stop")
	}
}

func TestGracefulStop_SessionExitsDuringPolling(t *testing.T) {
	tmp := t.TempDir()
	briefDir := filepath.Join(tmp, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Session dies after 2 captures.
	var captureCount int32
	mgr := &mockManager{
		existsFn: func(name string) bool {
			return atomic.LoadInt32(&captureCount) < 3
		},
		captureFn: func(name string, lines int) (string, error) {
			atomic.AddInt32(&captureCount, 1)
			return fmt.Sprintf("output-%d", captureCount), nil
		},
	}

	err := gracefulStop("sol-test-gov", briefDir, mgr, time.Millisecond, 4, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have injected.
	if len(mgr.injectCalls) != 1 {
		t.Fatalf("expected 1 inject call, got %d", len(mgr.injectCalls))
	}

	// Should NOT have called Stop (session exited on its own).
	if len(mgr.stopCalls) != 0 {
		t.Errorf("expected 0 stop calls (session exited), got %d", len(mgr.stopCalls))
	}
}

func TestGracefulStop_MaxTimeout(t *testing.T) {
	tmp := t.TempDir()
	briefDir := filepath.Join(tmp, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Captures always change — never stabilizes.
	var idx int32
	mgr := &mockManager{
		captureFn: func(name string, lines int) (string, error) {
			i := atomic.AddInt32(&idx, 1)
			return fmt.Sprintf("changing-output-%d", i), nil
		},
	}

	start := time.Now()
	err := gracefulStop("sol-test-gov", briefDir, mgr, time.Millisecond, 4, 20*time.Millisecond)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have timed out and force-killed.
	if len(mgr.stopCalls) != 1 {
		t.Fatalf("expected 1 stop call, got %d", len(mgr.stopCalls))
	}
	if !mgr.stopCalls[0] {
		t.Error("expected force=true on stop")
	}

	// Should have injected.
	if len(mgr.injectCalls) != 1 {
		t.Fatalf("expected 1 inject call, got %d", len(mgr.injectCalls))
	}

	// Verify timeout was respected (with generous margin for CI).
	if elapsed > 5*time.Second {
		t.Errorf("took too long: %v (expected ~20ms)", elapsed)
	}
}

func TestGracefulStop_InjectFails(t *testing.T) {
	tmp := t.TempDir()
	briefDir := filepath.Join(tmp, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}

	mgr := &mockManager{
		injectFn: func(name, text string, submit bool) error {
			return fmt.Errorf("inject failed")
		},
	}

	err := gracefulStop("sol-test-gov", briefDir, mgr, time.Millisecond, 4, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have fallen back to force kill.
	if len(mgr.stopCalls) != 1 {
		t.Fatalf("expected 1 stop call, got %d", len(mgr.stopCalls))
	}
	if !mgr.stopCalls[0] {
		t.Error("expected force=true on stop")
	}
}

func TestGracefulStop_InjectFailsSessionDead(t *testing.T) {
	tmp := t.TempDir()
	briefDir := filepath.Join(tmp, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}

	injectCalled := false
	mgr := &mockManager{
		existsFn: func(name string) bool {
			// Exists returns true before inject, false after.
			return !injectCalled
		},
		injectFn: func(name, text string, submit bool) error {
			injectCalled = true
			return fmt.Errorf("session died")
		},
	}

	err := gracefulStop("sol-test-gov", briefDir, mgr, time.Millisecond, 4, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT have called Stop (session already dead).
	if len(mgr.stopCalls) != 0 {
		t.Errorf("expected 0 stop calls, got %d", len(mgr.stopCalls))
	}
}
