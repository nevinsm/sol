package cmd

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/consul"
	"github.com/nevinsm/sol/internal/prefect"
)

// TestConsulStatusExitCodeStaleHeartbeat verifies that `sol consul status`
// returns exit 2 when the heartbeat is stale (the wedged-but-PID-alive case).
// CF-M22.
func TestConsulStatusExitCodeStaleHeartbeat(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Write a stale heartbeat (older than the staleness threshold).
	if err := consul.WriteHeartbeat(solHome, &consul.Heartbeat{
		Timestamp:   time.Now().Add(-30 * time.Minute),
		Status:      "running",
		PatrolCount: 7,
	}); err != nil {
		t.Fatalf("WriteHeartbeat: %v", err)
	}

	rootCmd.SetArgs([]string{"consul", "status"})
	err := rootCmd.Execute()
	if code := ExitCode(err); code != 2 {
		t.Fatalf("expected exit code 2 for stale heartbeat, got %d (err=%v)", code, err)
	}
}

// TestConsulStatusExitCodeFreshHeartbeat verifies that a fresh heartbeat
// returns exit 0.
func TestConsulStatusExitCodeFreshHeartbeat(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	if err := consul.WriteHeartbeat(solHome, &consul.Heartbeat{
		Timestamp:   time.Now(),
		Status:      "running",
		PatrolCount: 1,
	}); err != nil {
		t.Fatalf("WriteHeartbeat: %v", err)
	}

	rootCmd.SetArgs([]string{"consul", "status"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected nil error on fresh heartbeat, got: %v", err)
	}
}

// TestStopConsulForRestartEmptyPidfileFindsOrphan covers the recovery path
// for writ sol-a0d18aac092e8ab4 scope 4: when the pidfile is empty but a
// `sol consul run` process is still alive (the bug symptom), stopConsulForRestart
// must locate it via the proc-scan stub, SIGTERM it, and clear the pidfile.
func TestStopConsulForRestartEmptyPidfileFindsOrphan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SOL_HOME", home)
	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	// Pidfile is empty — simulates the corrupted state.
	if err := os.WriteFile(consulPIDPath(), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Orphan is a real long-lived subprocess that responds to SIGTERM.
	orphan := exec.Command("sleep", "60")
	if err := orphan.Start(); err != nil {
		t.Fatalf("start orphan: %v", err)
	}
	t.Cleanup(func() {
		_ = orphan.Process.Kill()
		_, _ = orphan.Process.Wait()
	})

	// Stub the proc scan to return our orphan.
	orig := findRunningConsuls
	t.Cleanup(func() { findRunningConsuls = orig })
	findRunningConsuls = func() ([]int, error) {
		return []int{orphan.Process.Pid}, nil
	}

	if err := stopConsulForRestart(); err != nil {
		t.Fatalf("stopConsulForRestart: %v", err)
	}

	// Wait for the orphan to actually exit.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !prefect.IsRunning(orphan.Process.Pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if prefect.IsRunning(orphan.Process.Pid) {
		t.Fatalf("orphan pid %d still running after stopConsulForRestart", orphan.Process.Pid)
	}
}

// TestStopConsulForRestartEmptyPidfileNoOrphan covers the case where the
// pidfile is empty AND no running consul process exists. stopConsulForRestart
// should return nil (nothing to do) so the caller can proceed to start.
func TestStopConsulForRestartEmptyPidfileNoOrphan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SOL_HOME", home)
	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(consulPIDPath(), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	orig := findRunningConsuls
	t.Cleanup(func() { findRunningConsuls = orig })
	findRunningConsuls = func() ([]int, error) { return nil, nil }

	if err := stopConsulForRestart(); err != nil {
		t.Fatalf("stopConsulForRestart: %v", err)
	}
}

// TestStopConsulForRestartEmptyPidfileMultipleOrphans verifies that
// ambiguous state (multiple matches) surfaces as an explicit error rather
// than guessing which process to kill.
func TestStopConsulForRestartEmptyPidfileMultipleOrphans(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SOL_HOME", home)
	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(consulPIDPath(), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	orig := findRunningConsuls
	t.Cleanup(func() { findRunningConsuls = orig })
	findRunningConsuls = func() ([]int, error) { return []int{11111, 22222}, nil }

	err := stopConsulForRestart()
	if err == nil {
		t.Fatal("expected error for multiple orphans, got nil")
	}
}

// TestStopConsulForRestartLivePidInFile covers the normal path where the
// pidfile records a live consul pid.
func TestStopConsulForRestartLivePidInFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SOL_HOME", home)
	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	target := exec.Command("sleep", "60")
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = target.Process.Kill()
		_, _ = target.Process.Wait()
	})

	if err := os.WriteFile(consulPIDPath(),
		[]byte(strconv.Itoa(target.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := stopConsulForRestart(); err != nil {
		t.Fatalf("stopConsulForRestart: %v", err)
	}

	// Target should be dead.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !prefect.IsRunning(target.Process.Pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if prefect.IsRunning(target.Process.Pid) {
		t.Fatalf("target pid %d still alive", target.Process.Pid)
	}
}

// TestConsulStatusExitCodeMissingHeartbeat verifies the no-heartbeat path
// returns exit 1.
func TestConsulStatusExitCodeMissingHeartbeat(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	// No heartbeat file exists.
	rootCmd.SetArgs([]string{"consul", "status"})
	err := rootCmd.Execute()
	if code := ExitCode(err); code != 1 {
		t.Fatalf("expected exit code 1 for missing heartbeat, got %d (err=%v)", code, err)
	}
}
