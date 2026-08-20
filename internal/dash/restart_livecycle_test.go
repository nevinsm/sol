package dash

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/processutil"
)

// TestMain lets this test binary double as a fake sphere daemon. When
// invoked as "<test-binary> <cliName> run" for one of the five sphere-daemon
// CLI names (see sphereProcessMap), it behaves like that daemon's real `run`
// subcommand as far as daemon.Start/daemon.Stop (which restartSphereProcess
// now delegates to) can tell: it self-registers its PID via the same
// flock-authoritative processutil.WritePID that daemon.RunBootstrap uses
// (cmd/chronicle.go, cmd/prefect.go, ...), then blocks until SIGTERM.
//
// This lets TestRestartSphereProcessLiveCycle drive restartSphereProcess
// against a real, live OS process — proving the stop-then-start sequence
// actually works end to end — without needing the compiled sol binary or
// touching the host's real sphere daemons. daemon.Start's default
// resolveSolBinary is os.Executable(), which under `go test` already
// resolves to this test binary, so no override is needed.
func TestMain(m *testing.M) {
	if len(os.Args) >= 3 && os.Args[2] == "run" {
		switch os.Args[1] {
		case "prefect", "consul", "chronicle", "ledger", "broker":
			runFakeSphereDaemon(os.Args[1])
			return
		}
	}
	os.Exit(m.Run())
}

// runFakeSphereDaemon stands in for `sol <name> run`. It never returns; the
// process exits via SIGTERM (sent by daemon.Stop) or the safety timeout.
func runFakeSphereDaemon(name string) {
	pidPath := filepath.Join(config.RuntimeDir(), name+".pid")
	if err := processutil.WritePID(pidPath, os.Getpid()); err != nil {
		os.Exit(2)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-sig:
	case <-time.After(30 * time.Second):
	}
	os.Exit(0)
}

// killPID is a best-effort cleanup kill for a spawned fake daemon.
func killPID(pid int) {
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// TestRestartSphereProcessLiveCycle is the live smoke test for writ
// sol-87ec3e7e5c14c3f5: it drives restartSphereProcess directly against a
// real (fake-daemon) process rather than mocking daemon.Stop/daemon.Start,
// proving the dedup — dash calling the same daemon.Stop/daemon.Start pair
// cmd/up.go uses — actually works for a live process.
//
// Chronicle is used as the target daemon per the writ's suggestion. Only an
// isolated $SOL_HOME is touched; the systemd guard is stubbed off so the
// test's outcome doesn't depend on whether this machine happens to run a
// real sol sphere under systemd (see systemctlIsActive and
// TestSphereProcessRestartMsgTriggersConfirmation for the same concern).
// No tmux session is created (chronicle is PID-based, not tmuxManaged), so
// the SOL_SESSION_COMMAND session-isolation rule in
// test/integration/helpers_test.go does not apply here.
func TestRestartSphereProcessLiveCycle(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc not available")
	}

	solHome := t.TempDir()
	prevHome, hadHome := os.LookupEnv("SOL_HOME")
	os.Setenv("SOL_HOME", solHome)
	t.Cleanup(func() {
		if hadHome {
			os.Setenv("SOL_HOME", prevHome)
		} else {
			os.Unsetenv("SOL_HOME")
		}
	})

	prevSystemd := systemctlIsActive
	systemctlIsActive = func(string) bool { return false }
	t.Cleanup(func() { systemctlIsActive = prevSystemd })

	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	pidPath := filepath.Join(config.RuntimeDir(), "chronicle.pid")

	// Cold start: nothing running yet under the pidfile, so this exercises
	// the "stop finds nothing to stop, then start spawns" path.
	if err := restartSphereProcess("", "Chronicle"); err != nil {
		t.Fatalf("cold-start restart: %v", err)
	}
	firstPID, _ := processutil.ReadPID(pidPath)
	if firstPID <= 0 || !processutil.IsRunning(firstPID) {
		t.Fatalf("expected a live chronicle pid after cold-start restart, got %d", firstPID)
	}
	t.Cleanup(func() { killPID(firstPID) })

	// Warm restart: a real live process now owns the pidfile.
	// restartSphereProcess must stop it (SIGTERM, per daemon.Stop) and start
	// a fresh instance.
	if err := restartSphereProcess("", "Chronicle"); err != nil {
		t.Fatalf("warm restart: %v", err)
	}
	secondPID, _ := processutil.ReadPID(pidPath)
	if secondPID <= 0 || !processutil.IsRunning(secondPID) {
		t.Fatalf("expected a live chronicle pid after warm restart, got %d", secondPID)
	}
	t.Cleanup(func() { killPID(secondPID) })

	if secondPID == firstPID {
		t.Fatalf("expected a new pid after restart, got the same pid %d both times", firstPID)
	}
	if processutil.IsRunning(firstPID) {
		t.Errorf("old chronicle pid %d should have been stopped by the restart", firstPID)
	}
}
