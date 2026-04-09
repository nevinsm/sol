package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/store"
)

// setupUpTestEnv creates a temporary SOL_HOME with a sphere store and
// optional worlds. Returns a cleanup function.
func setupUpTestEnv(t *testing.T, worlds []string, sleeping map[string]bool) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("SOL_HOME", home)

	// Create sphere store and register worlds.
	if err := os.MkdirAll(config.StoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}

	for _, w := range worlds {
		if err := sphereStore.RegisterWorld(w, "https://example.com/"+w); err != nil {
			t.Fatal(err)
		}
		// Create world.toml.
		worldDir := filepath.Join(home, w)
		if err := os.MkdirAll(worldDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "[world]\nsource_repo = \"https://example.com/" + w + "\"\n"
		if sleeping[w] {
			content += "sleeping = true\n"
		}
		if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sphereStore.Close()
}

func TestActiveWorldsAllNonSleeping(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha", "beta", "gamma"}, map[string]bool{
		"beta": true,
	})

	worlds, err := activeWorlds("")
	if err != nil {
		t.Fatal(err)
	}

	if len(worlds) != 2 {
		t.Fatalf("expected 2 active worlds, got %d: %v", len(worlds), worlds)
	}

	// ListWorlds returns alphabetical order.
	if worlds[0] != "alpha" || worlds[1] != "gamma" {
		t.Errorf("expected [alpha, gamma], got %v", worlds)
	}
}

func TestActiveWorldsSpecificWorld(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha", "beta"}, nil)

	worlds, err := activeWorlds("alpha")
	if err != nil {
		t.Fatal(err)
	}

	if len(worlds) != 1 || worlds[0] != "alpha" {
		t.Fatalf("expected [alpha], got %v", worlds)
	}
}

func TestActiveWorldsSpecificSleepingWorld(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha"}, map[string]bool{"alpha": true})

	_, err := activeWorlds("alpha")
	if err == nil {
		t.Fatal("expected error for sleeping world, got nil")
	}
}

func TestActiveWorldsSpecificNonexistent(t *testing.T) {
	setupUpTestEnv(t, nil, nil)

	_, err := activeWorlds("nope")
	if err == nil {
		t.Fatal("expected error for nonexistent world, got nil")
	}
}

func TestActiveWorldsNoWorlds(t *testing.T) {
	setupUpTestEnv(t, nil, nil)

	worlds, err := activeWorlds("")
	if err != nil {
		t.Fatal(err)
	}

	if len(worlds) != 0 {
		t.Fatalf("expected 0 worlds, got %d", len(worlds))
	}
}

func TestListAllWorlds(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha", "beta", "gamma"}, map[string]bool{
		"beta": true,
	})

	worlds, err := listAllWorlds()
	if err != nil {
		t.Fatal(err)
	}

	// listAllWorlds returns all worlds, including sleeping.
	if len(worlds) != 3 {
		t.Fatalf("expected 3 worlds, got %d: %v", len(worlds), worlds)
	}
}

func TestResolveWorldsForDownAll(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha", "beta"}, map[string]bool{
		"beta": true,
	})

	// Should return all worlds regardless of sleeping state.
	worlds, err := resolveWorldsForDown("")
	if err != nil {
		t.Fatal(err)
	}

	if len(worlds) != 2 {
		t.Fatalf("expected 2 worlds, got %d: %v", len(worlds), worlds)
	}
}

func TestResolveWorldsForDownSpecific(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha"}, nil)

	worlds, err := resolveWorldsForDown("alpha")
	if err != nil {
		t.Fatal(err)
	}

	if len(worlds) != 1 || worlds[0] != "alpha" {
		t.Fatalf("expected [alpha], got %v", worlds)
	}
}

func TestResolveWorldsForDownNonexistent(t *testing.T) {
	setupUpTestEnv(t, nil, nil)

	_, err := resolveWorldsForDown("nope")
	if err == nil {
		t.Fatal("expected error for nonexistent world, got nil")
	}
}

func TestWorldServicesContents(t *testing.T) {
	found := map[string]bool{}
	for _, svc := range worldServices {
		found[svc] = true
	}

	if !found["sentinel"] {
		t.Error("worldServices missing sentinel")
	}
	if !found["forge"] {
		t.Error("worldServices missing forge")
	}
}

func TestUpCmdHasWorldFlag(t *testing.T) {
	f := upCmd.Flags().Lookup("world")
	if f == nil {
		t.Fatal("up command missing --world flag")
	}
	if f.NoOptDefVal != "" {
		t.Errorf("--world NoOptDefVal should be empty string, got %q", f.NoOptDefVal)
	}
}

// ----- classifyDaemonStartup -----

// TestClassifyDaemonStartupStarted exercises the normal successful-start
// path: the child wrote its own pid and is still alive.
func TestClassifyDaemonStartupStarted(t *testing.T) {
	// Use a live pid — our own is convenient and guaranteed alive.
	self := os.Getpid()
	status, owner := classifyDaemonStartup(self, self)
	if status != "started" {
		t.Fatalf("status = %q, want started", status)
	}
	if owner != self {
		t.Fatalf("owner = %d, want %d", owner, self)
	}
}

// TestClassifyDaemonStartupAlreadyRunning is the key regression test for the
// writ sol-a0d18aac092e8ab4 bug: pidfile contains a live pid that does NOT
// belong to our child. The child legitimately exited via the "already
// running" early return. This must be classified as "running", not "failed",
// so the caller does not clobber the pidfile.
func TestClassifyDaemonStartupAlreadyRunning(t *testing.T) {
	// Start a real child process whose pid we can reference.
	holder, err := startHolderProcess()
	if err != nil {
		t.Fatalf("startHolderProcess: %v", err)
	}
	t.Cleanup(func() { stopHolderProcess(holder) })

	// Our "spawned child" pid is something different — use an obviously
	// invalid-but-nonzero value that is not the holder.
	fakeChildPid := holder.Pid + 999999 // unlikely to collide

	status, owner := classifyDaemonStartup(holder.Pid, fakeChildPid)
	if status != "running" {
		t.Fatalf("status = %q, want running (filePID=%d childPid=%d)",
			status, holder.Pid, fakeChildPid)
	}
	if owner != holder.Pid {
		t.Fatalf("owner = %d, want %d", owner, holder.Pid)
	}
}

// TestClassifyDaemonStartupFailedEmpty covers the crash path: child exited
// before writing the pidfile.
func TestClassifyDaemonStartupFailedEmpty(t *testing.T) {
	status, owner := classifyDaemonStartup(0, 99999)
	if status != "failed" {
		t.Fatalf("status = %q, want failed", status)
	}
	if owner != 0 {
		t.Fatalf("owner = %d, want 0", owner)
	}
}

// TestClassifyDaemonStartupFailedChildDead covers the case where the child
// wrote its pid but then died before the parent's sleep ended.
func TestClassifyDaemonStartupFailedChildDead(t *testing.T) {
	// Use a pid that's almost certainly not alive.
	deadPid := 1 // init (or rare contend); we only need filePID == childPid
	// On Linux pid 1 is always alive, so use a short-lived subprocess instead.
	dead, err := runToCompletion()
	if err != nil {
		t.Fatal(err)
	}
	_ = deadPid
	status, _ := classifyDaemonStartup(dead, dead)
	if status != "failed" {
		t.Fatalf("status = %q, want failed for dead pid %d", status, dead)
	}
}

// ----- clearDaemonPIDIfMine -----

// TestClearDaemonPIDIfMineProtectsForeignLivePID verifies that the defensive
// clearer does NOT truncate the pidfile when it records a live pid belonging
// to a different process. This is the "another instance owns the file"
// invariant from writ sol-a0d18aac092e8ab4.
func TestClearDaemonPIDIfMineProtectsForeignLivePID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SOL_HOME", home)
	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	holder, err := startHolderProcess()
	if err != nil {
		t.Fatalf("startHolderProcess: %v", err)
	}
	t.Cleanup(func() { stopHolderProcess(holder) })

	path := daemonPIDPath("test-daemon")
	if err := os.WriteFile(path, []byte(strconv.Itoa(holder.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Our "child pid" is different from the holder.
	clearDaemonPIDIfMine("test-daemon", holder.Pid+999999)

	got := readDaemonPID("test-daemon")
	if got != holder.Pid {
		t.Fatalf("pidfile should still contain holder pid %d, got %d", holder.Pid, got)
	}
}

// TestClearDaemonPIDIfMineClearsOwnPID verifies that the defensive clearer
// DOES truncate when the file contains exactly the expected child pid.
func TestClearDaemonPIDIfMineClearsOwnPID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SOL_HOME", home)
	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	// Use a dead pid so IsRunning(filePID) is false and we hit the "my pid,
	// dead" branch. A process we ran to completion is a reliable dead pid.
	dead, err := runToCompletion()
	if err != nil {
		t.Fatal(err)
	}

	path := daemonPIDPath("test-daemon")
	if err := os.WriteFile(path, []byte(strconv.Itoa(dead)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	clearDaemonPIDIfMine("test-daemon", dead)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("pidfile should be truncated, size = %d", info.Size())
	}
}

// ----- Helpers -----

// startHolderProcess starts a long-lived sleep subprocess suitable as a stand-
// in for "another running daemon". Returns its os.Process.
func startHolderProcess() (*os.Process, error) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Ensure it's scheduled and visible via /proc.
	time.Sleep(10 * time.Millisecond)
	return cmd.Process, nil
}

func stopHolderProcess(p *os.Process) {
	if p == nil {
		return
	}
	_ = p.Signal(syscall.SIGKILL)
	// Best-effort reap.
	_, _ = p.Wait()
}

// writeFakeSphereDaemonScript creates a shell script that impersonates a
// sphere daemon's `sol <daemon> run` binary. When invoked it acquires an
// advisory flock on the pidfile (interoperable with processutil.WritePID's
// syscall.Flock) and writes its own pid. If another process already holds the
// flock it exits cleanly — mirroring the real daemon's "already running"
// early return. The `crash` mode exits immediately without writing a pid to
// simulate an immediate crash.
func writeFakeSphereDaemonScript(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-sol")
	var script string
	switch mode {
	case "normal":
		script = `#!/bin/sh
set -e
DAEMON="$1"
ACTION="$2"
if [ "$ACTION" != "run" ]; then
	exit 2
fi
PIDFILE="$SOL_HOME/.runtime/$DAEMON.pid"
mkdir -p "$(dirname "$PIDFILE")"
[ -f "$PIDFILE" ] || : > "$PIDFILE"
exec 9<>"$PIDFILE"
if ! flock -xn 9; then
	# Another instance holds the flock — exit cleanly without touching pidfile.
	exit 0
fi
: > "$PIDFILE"
printf '%d\n' "$$" > "$PIDFILE"
# Keep fd 9 open (flock held) while we sleep.
sleep 60
`
	case "crash":
		script = `#!/bin/sh
# Exits immediately without writing a pidfile — simulates an immediate crash.
exit 1
`
	default:
		t.Fatalf("unknown fake daemon mode %q", mode)
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStartSphereDaemonsChildOwnsPidfile is the functional regression test for
// scope 1 of writ sol-a0d18aac092e8ab4: verify that startSphereDaemons does
// NOT clobber the pidfile written by the spawned child. The fake daemon
// acquires a real flock on the pidfile and writes its own pid via shell. If
// the parent were still calling writeDaemonPID on the unlocked fast path, it
// would race against the child's write and corrupt the contents.
func TestStartSphereDaemonsChildOwnsPidfile(t *testing.T) {
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock(1) not available")
	}

	home := t.TempDir()
	t.Setenv("SOL_HOME", home)
	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	// Swap sphereDaemons for a minimal single-daemon list so we don't touch
	// any real daemon pidfiles. The name must not match a systemd unit on
	// the host system.
	origDaemons := sphereDaemons
	t.Cleanup(func() { sphereDaemons = origDaemons })
	sphereDaemons = []sphereDaemon{{name: "fakedaemon-" + t.Name()}}

	solBin := writeFakeSphereDaemonScript(t, "normal")

	failed, err := startSphereDaemons(solBin, nil)
	if err != nil {
		t.Fatalf("startSphereDaemons: %v", err)
	}
	if failed {
		t.Fatalf("startSphereDaemons reported failure")
	}

	pid := readDaemonPID(sphereDaemons[0].name)
	if pid <= 0 {
		t.Fatalf("pidfile empty after startSphereDaemons — parent may have clobbered it")
	}
	if !prefect.IsRunning(pid) {
		t.Fatalf("recorded pid %d is not alive", pid)
	}

	// Clean up: kill the fake child.
	_ = syscall.Kill(pid, syscall.SIGKILL)
	// Give the kernel a moment to reap; we don't wait on it because we never
	// held its os.Process handle.
	time.Sleep(50 * time.Millisecond)
}

// TestStartSphereDaemonsDetectsCrash verifies that an immediate-exit child is
// classified as "failed" and that the defensive clearer is used (it is safe
// because the pidfile was empty — the child never wrote it).
func TestStartSphereDaemonsDetectsCrash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SOL_HOME", home)
	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	origDaemons := sphereDaemons
	t.Cleanup(func() { sphereDaemons = origDaemons })
	sphereDaemons = []sphereDaemon{{name: "fakedaemon-crash-" + t.Name()}}

	solBin := writeFakeSphereDaemonScript(t, "crash")

	failed, err := startSphereDaemons(solBin, nil)
	if err != nil {
		t.Fatalf("startSphereDaemons: %v", err)
	}
	if !failed {
		t.Fatalf("expected failed=true for crashing daemon")
	}
	// Pidfile should be empty (child never wrote; defensive clear is a no-op
	// because the file was already empty).
	pid := readDaemonPID(sphereDaemons[0].name)
	if pid != 0 {
		t.Fatalf("pidfile should be empty after crash, got pid %d", pid)
	}
}

// runToCompletion runs a subprocess that exits immediately and returns the
// (now-dead) pid. Useful for synthesizing a "stale pid" value.
func runToCompletion() (int, error) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	// Poll until IsRunning reports dead — /proc/<pid>/stat may linger briefly.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !prefect.IsRunning(pid) {
			return pid, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return pid, nil
}

func TestDownCmdHasWorldFlag(t *testing.T) {
	f := downCmd.Flags().Lookup("world")
	if f == nil {
		t.Fatal("down command missing --world flag")
	}
	if f.NoOptDefVal != "" {
		t.Errorf("--world NoOptDefVal should be empty string, got %q", f.NoOptDefVal)
	}
}
