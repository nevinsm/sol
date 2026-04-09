package cmd

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/consul"
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

// NOTE: The stopConsulForRestart / findRunningConsuls tests previously
// lived here. Those code paths moved into internal/daemon.Restart and are
// covered by internal/daemon/lifecycle_test.go (TestRestartRecoversFrom
// EmptyPidfile, TestRestartRefusesWhenMultipleOrphans). See
// sol-06e76378be1408bf.
