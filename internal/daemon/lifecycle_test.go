package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/processutil"
)

// TestMain lets this test binary double as a fake daemon. When invoked with
// argv ["<exe>", "fake", "daemon"] (either directly or via a symlink named
// "sol"), it runs the fake-daemon logic controlled by environment variables
// rather than invoking the Go test runner.
//
// Env vars consumed by the fake daemon:
//
//	FAKE_DAEMON_EXIT         — if set, exit with this code immediately
//	FAKE_DAEMON_PID_PATH     — if set, WritePID(self) before blocking
//	FAKE_DAEMON_IGNORE_TERM  — if set, ignore SIGTERM (forces SIGKILL escalation)
//	FAKE_DAEMON_POSTSPAWN    — no-op marker; present only so tests can inspect env
//
// Without FAKE_DAEMON_EXIT or IGNORE_TERM the daemon blocks until SIGTERM
// or a 30-second safety timeout.
func TestMain(m *testing.M) {
	if len(os.Args) >= 3 && os.Args[1] == "fake" && os.Args[2] == "daemon" {
		runFakeDaemon()
		return
	}
	os.Exit(m.Run())
}

func runFakeDaemon() {
	if code := os.Getenv("FAKE_DAEMON_EXIT"); code != "" {
		n, _ := strconv.Atoi(code)
		os.Exit(n)
	}
	if pidPath := os.Getenv("FAKE_DAEMON_PID_PATH"); pidPath != "" {
		if err := processutil.WritePID(pidPath, os.Getpid()); err != nil {
			fmt.Fprintf(os.Stderr, "fake daemon: WritePID: %v\n", err)
			os.Exit(2)
		}
	}
	if os.Getenv("FAKE_DAEMON_IGNORE_TERM") != "" {
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-sig:
	case <-time.After(30 * time.Second):
	}
	os.Exit(0)
}

// ----- helpers -----

// setupSolSymlink creates a symlink named "sol" that points at the test
// binary and wires resolveSolBinary so Start spawns the child via the
// symlink. The symlink basename lets FindSolSubcommandPIDs match the
// process in /proc.
func setupSolSymlink(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc not available")
	}
	dir := t.TempDir()
	solLink := filepath.Join(dir, "sol")
	if err := os.Symlink(os.Args[0], solLink); err != nil {
		t.Fatalf("symlink sol: %v", err)
	}
	prev := resolveSolBinary
	resolveSolBinary = func() (string, error) { return solLink, nil }
	t.Cleanup(func() { resolveSolBinary = prev })
	return solLink
}

// killPID best-effort kills pid; used for test cleanup.
func killPID(pid int) {
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// spawnFakeDaemonDirect spawns a fake daemon via the given sol symlink path
// (bypassing Lifecycle.Start). Used to pre-populate /proc for orphan-scan
// tests.
func spawnFakeDaemonDirect(t *testing.T, solLink string, env []string) *exec.Cmd {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "fake.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	t.Cleanup(func() { logFile.Close() })

	cmd := &exec.Cmd{
		Path:   solLink,
		Args:   []string{solLink, "fake", "daemon"},
		Env:    env,
		Stdout: logFile,
		Stderr: logFile,
		SysProcAttr: &syscall.SysProcAttr{
			Setpgid: true,
		},
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake daemon: %v", err)
	}
	t.Cleanup(func() {
		killPID(cmd.Process.Pid)
		_ = cmd.Wait()
	})
	return cmd
}

func makeLifecycle(t *testing.T, name, dir string, env []string) Lifecycle {
	t.Helper()
	return Lifecycle{
		Name:    name,
		PIDPath: func() string { return filepath.Join(dir, name+".pid") },
		RunArgs: []string{"fake", "daemon"},
		LogPath: func() string { return filepath.Join(dir, name+".log") },
		Env:     env,
	}
}

// ----- Start -----

func TestStartReportsRunningWhenPidfileMatchesLiveProcess(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")

	// Pre-populate pidfile with a real live pid (a sleep subprocess).
	sleepCmd := exec.Command("sleep", "30")
	if err := sleepCmd.Start(); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	t.Cleanup(func() { _ = sleepCmd.Process.Kill(); _ = sleepCmd.Wait() })
	livePID := sleepCmd.Process.Pid
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(livePID)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lc := Lifecycle{
		Name:    "running-test",
		PIDPath: func() string { return pidPath },
		// RunArgs intentionally points at a bogus command so that if Start
		// incorrectly tries to spawn, the test explodes.
		RunArgs: []string{"definitely", "not-a-subcommand"},
		LogPath: func() string { return filepath.Join(dir, "d.log") },
	}

	res, err := Start(lc)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if res.Status != "running" || res.PID != livePID {
		t.Fatalf("got %+v, want Status=running PID=%d", res, livePID)
	}

	// Pidfile must still record the original live pid — never clobbered.
	got, _ := processutil.ReadPID(pidPath)
	if got != livePID {
		t.Fatalf("pidfile clobbered: got %d, want %d", got, livePID)
	}
}

func TestStartReportsStartedWhenChildWritesOwnPID(t *testing.T) {
	setupSolSymlink(t)
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")
	env := append(os.Environ(), "FAKE_DAEMON_PID_PATH="+pidPath)
	lc := makeLifecycle(t, "started-test", dir, env)
	lc.PIDPath = func() string { return pidPath }

	res, err := Start(lc)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { killPID(res.PID) })

	if res.Status != "started" || res.PID <= 0 {
		t.Fatalf("got %+v, want Status=started with pid > 0", res)
	}

	filePID, _ := processutil.ReadPID(pidPath)
	if filePID != res.PID {
		t.Fatalf("pidfile has %d, want %d", filePID, res.PID)
	}
	if !processutil.IsRunning(res.PID) {
		t.Fatalf("child pid %d is not running", res.PID)
	}
}

func TestStartReportsFailedWhenChildExitsImmediately(t *testing.T) {
	setupSolSymlink(t)
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")
	env := append(os.Environ(), "FAKE_DAEMON_EXIT=1")
	lc := makeLifecycle(t, "failed-test", dir, env)
	lc.PIDPath = func() string { return pidPath }

	res, err := Start(lc)
	if err == nil {
		t.Fatalf("Start should have errored, got %+v", res)
	}
	if res.Status != "failed" || res.PID != 0 {
		t.Fatalf("got %+v, want Status=failed PID=0", res)
	}
	if !strings.Contains(err.Error(), "exited immediately") {
		t.Fatalf("error should mention 'exited immediately': %v", err)
	}
	if !strings.Contains(err.Error(), lc.LogPath()) {
		t.Fatalf("error should include log path %q: %v", lc.LogPath(), err)
	}

	// Pidfile must be empty after a failed start.
	filePID, _ := processutil.ReadPID(pidPath)
	if filePID != 0 {
		t.Fatalf("pidfile should be empty after failed start, got %d", filePID)
	}
}

func TestStartInvokesPostSpawn(t *testing.T) {
	setupSolSymlink(t)
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")
	env := append(os.Environ(), "FAKE_DAEMON_PID_PATH="+pidPath)
	lc := makeLifecycle(t, "postspawn-test", dir, env)
	lc.PIDPath = func() string { return pidPath }

	postSpawnErr := fmt.Errorf("post-spawn rejected")
	var observedPID int
	lc.PostSpawn = func(pid int) error {
		observedPID = pid
		return postSpawnErr
	}

	res, err := Start(lc)
	// Whatever the result is, make sure we clean up the child.
	t.Cleanup(func() {
		killPID(res.PID)
		if p, _ := processutil.ReadPID(pidPath); p > 0 {
			killPID(p)
		}
	})

	if err == nil || !strings.Contains(err.Error(), "post-spawn") {
		t.Fatalf("expected post-spawn error, got err=%v res=%+v", err, res)
	}
	if observedPID <= 0 {
		t.Fatalf("PostSpawn was not invoked (observedPID=%d)", observedPID)
	}
	if res.Status != "failed" {
		t.Fatalf("Status should be failed after post-spawn error, got %q", res.Status)
	}
}

func TestStartNeverClobbersForeignLivePid(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")

	// A live foreign process whose pid is recorded in the file.
	sleepCmd := exec.Command("sleep", "30")
	if err := sleepCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sleepCmd.Process.Kill(); _ = sleepCmd.Wait() })
	foreignPID := sleepCmd.Process.Pid
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(foreignPID)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Use a lifecycle whose RunArgs point at a never-will-exist command.
	lc := Lifecycle{
		Name:    "foreign-test",
		PIDPath: func() string { return pidPath },
		RunArgs: []string{"fake", "daemon"},
		LogPath: func() string { return filepath.Join(dir, "d.log") },
	}
	res, err := Start(lc)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if res.Status != "running" || res.PID != foreignPID {
		t.Fatalf("got %+v, want Status=running PID=%d", res, foreignPID)
	}

	// Pidfile must be untouched.
	filePID, _ := processutil.ReadPID(pidPath)
	if filePID != foreignPID {
		t.Fatalf("pidfile clobbered: got %d, want %d", filePID, foreignPID)
	}
}

// ----- Stop -----

func TestStopSendsSIGTERMAndClearsPidfile(t *testing.T) {
	setupSolSymlink(t)
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")
	env := append(os.Environ(), "FAKE_DAEMON_PID_PATH="+pidPath)
	lc := makeLifecycle(t, "stop-test", dir, env)
	lc.PIDPath = func() string { return pidPath }
	lc.StopTimeout = 3 * time.Second

	res, err := Start(lc)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { killPID(res.PID) })
	child := res.PID

	if err := Stop(lc); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if processutil.IsRunning(child) {
		t.Fatalf("child pid %d still running after Stop", child)
	}
	if p, _ := processutil.ReadPID(pidPath); p != 0 {
		t.Fatalf("pidfile should be empty after Stop, got %d", p)
	}
}

func TestStopEscalatesToKillAfterTimeout(t *testing.T) {
	setupSolSymlink(t)
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")
	env := append(os.Environ(),
		"FAKE_DAEMON_PID_PATH="+pidPath,
		"FAKE_DAEMON_IGNORE_TERM=1",
	)
	lc := makeLifecycle(t, "sigkill-test", dir, env)
	lc.PIDPath = func() string { return pidPath }
	lc.StopTimeout = 500 * time.Millisecond

	res, err := Start(lc)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { killPID(res.PID) })
	child := res.PID

	if err := Stop(lc); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if processutil.IsRunning(child) {
		t.Fatalf("child pid %d still running after SIGKILL escalation", child)
	}
}

func TestStopInvokesPreStopHook(t *testing.T) {
	setupSolSymlink(t)
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")
	env := append(os.Environ(), "FAKE_DAEMON_PID_PATH="+pidPath)
	lc := makeLifecycle(t, "prestop-test", dir, env)
	lc.PIDPath = func() string { return pidPath }
	lc.StopTimeout = 3 * time.Second

	var preStopCalled bool
	lc.PreStop = func() error {
		preStopCalled = true
		return nil
	}

	res, err := Start(lc)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { killPID(res.PID) })

	if err := Stop(lc); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !preStopCalled {
		t.Fatal("PreStop was not invoked")
	}
}

func TestStopReturnsNilWhenNothingToStop(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")
	// Pidfile does not exist.
	lc := Lifecycle{
		Name:    "empty-stop",
		PIDPath: func() string { return pidPath },
		RunArgs: []string{"fake", "daemon"},
		LogPath: func() string { return filepath.Join(dir, "d.log") },
	}
	if err := Stop(lc); err != nil {
		t.Fatalf("Stop on empty pidfile: %v", err)
	}

	// Empty pidfile.
	if err := os.WriteFile(pidPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Stop(lc); err != nil {
		t.Fatalf("Stop on empty pidfile: %v", err)
	}
}

// ----- Restart -----

func TestRestartSimpleStopStart(t *testing.T) {
	setupSolSymlink(t)
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")
	env := append(os.Environ(), "FAKE_DAEMON_PID_PATH="+pidPath)
	lc := makeLifecycle(t, "restart-test", dir, env)
	lc.PIDPath = func() string { return pidPath }
	lc.StopTimeout = 3 * time.Second

	res, err := Start(lc)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	original := res.PID
	t.Cleanup(func() { killPID(original) })

	if err := Restart(lc); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	newPID, _ := processutil.ReadPID(pidPath)
	t.Cleanup(func() { killPID(newPID) })

	if newPID == 0 {
		t.Fatal("pidfile empty after Restart")
	}
	if newPID == original {
		t.Fatalf("Restart did not spawn a new process (pid %d)", newPID)
	}
	if !processutil.IsRunning(newPID) {
		t.Fatalf("new pid %d not running", newPID)
	}
	if processutil.IsRunning(original) {
		t.Fatalf("original pid %d still running", original)
	}
}

func TestRestartRecoversFromEmptyPidfile(t *testing.T) {
	setupSolSymlink(t)
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")
	env := append(os.Environ(), "FAKE_DAEMON_PID_PATH="+pidPath)
	lc := makeLifecycle(t, "recover-test", dir, env)
	lc.PIDPath = func() string { return pidPath }
	lc.StopTimeout = 3 * time.Second

	res, err := Start(lc)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	orphan := res.PID
	t.Cleanup(func() { killPID(orphan) })
	if !processutil.IsRunning(orphan) {
		t.Fatalf("orphan %d not running after Start", orphan)
	}

	// Simulate the bug: pidfile is truncated while the daemon is still alive.
	if err := processutil.ClearPID(pidPath); err != nil {
		t.Fatal(err)
	}
	if p, _ := processutil.ReadPID(pidPath); p != 0 {
		t.Fatalf("pidfile should be empty, got %d", p)
	}

	// Give the kernel a moment to populate /proc/{orphan}/cmdline with the
	// argv that FindSolSubcommandPIDs will match on.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pids, _ := processutil.FindSolSubcommandPIDs("fake", "daemon")
		found := false
		for _, p := range pids {
			if p == orphan {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := Restart(lc); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	newPID, _ := processutil.ReadPID(pidPath)
	t.Cleanup(func() { killPID(newPID) })

	if newPID == 0 {
		t.Fatal("pidfile still empty after Restart")
	}
	if newPID == orphan {
		t.Fatalf("Restart reused orphan pid %d — should have killed and respawned", orphan)
	}
	if processutil.IsRunning(orphan) {
		t.Fatalf("orphan %d still running after Restart", orphan)
	}
	if !processutil.IsRunning(newPID) {
		t.Fatalf("new pid %d not running after Restart", newPID)
	}
}

func TestRestartRefusesWhenMultipleOrphans(t *testing.T) {
	solLink := setupSolSymlink(t)
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")

	// Spawn two fake daemons directly (bypassing Start) so both remain alive
	// with matching argv and neither is tracked by the pidfile.
	env := os.Environ()
	cmd1 := spawnFakeDaemonDirect(t, solLink, env)
	cmd2 := spawnFakeDaemonDirect(t, solLink, env)

	// Wait for both to show up in /proc scan.
	want := map[int]bool{cmd1.Process.Pid: false, cmd2.Process.Pid: false}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pids, _ := processutil.FindSolSubcommandPIDs("fake", "daemon")
		found := 0
		for k := range want {
			want[k] = false
		}
		for _, p := range pids {
			if _, ok := want[p]; ok {
				want[p] = true
				found++
			}
		}
		if found == 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	for pid, seen := range want {
		if !seen {
			t.Fatalf("proc scan did not find fake daemon pid %d", pid)
		}
	}

	lc := Lifecycle{
		Name:    "multi-orphan-test",
		PIDPath: func() string { return pidPath },
		RunArgs: []string{"fake", "daemon"},
		LogPath: func() string { return filepath.Join(dir, "d.log") },
	}

	err := Restart(lc)
	if err == nil {
		t.Fatal("Restart should fail with multiple orphans")
	}
	if !strings.Contains(err.Error(), "multiple") || !strings.Contains(err.Error(), "refusing to guess") {
		t.Fatalf("expected multiple-orphans error, got: %v", err)
	}
}

// ----- RunBootstrap -----

func TestRunBootstrapAcquiresFlock(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")

	lc := Lifecycle{
		Name:    "bootstrap-test",
		PIDPath: func() string { return pidPath },
	}
	release, err := RunBootstrap(lc)
	if err != nil {
		t.Fatalf("RunBootstrap: %v", err)
	}
	t.Cleanup(release)

	p, _ := processutil.ReadPID(pidPath)
	if p != os.Getpid() {
		t.Fatalf("pidfile has %d, want self pid %d", p, os.Getpid())
	}

	// A second RunBootstrap from the same process should re-enter cleanly
	// (WritePID reuses the held handle).
	release2, err := RunBootstrap(lc)
	if err != nil {
		t.Fatalf("second RunBootstrap (re-entry): %v", err)
	}
	release2()
}

func TestRunBootstrapReleaseCleansUp(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")

	lc := Lifecycle{
		Name:    "release-test",
		PIDPath: func() string { return pidPath },
	}
	release, err := RunBootstrap(lc)
	if err != nil {
		t.Fatalf("RunBootstrap: %v", err)
	}
	release()

	info, err := os.Stat(pidPath)
	if err != nil {
		t.Fatalf("pidfile should still exist (inode preserved): %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("pidfile should be truncated after release, size=%d", info.Size())
	}
}

func TestRunBootstrapRejectsSecondInstance(t *testing.T) {
	setupSolSymlink(t)
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "d.pid")

	// Spawn a fake daemon that calls WritePID(self) via its FAKE_DAEMON_PID_PATH
	// and holds the flock. This is the equivalent of a running sibling.
	env := append(os.Environ(), "FAKE_DAEMON_PID_PATH="+pidPath)
	lc := makeLifecycle(t, "bootstrap-conflict", dir, env)
	lc.PIDPath = func() string { return pidPath }

	res, err := Start(lc)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { killPID(res.PID) })

	// Now try to RunBootstrap from the parent — the child process holds the
	// flock on the same inode, so WritePID should fail.
	_, err = RunBootstrap(lc)
	if err == nil {
		t.Fatal("RunBootstrap should fail while another instance holds the flock")
	}
	if !strings.Contains(err.Error(), "acquire pidfile lock") {
		t.Fatalf("error should mention flock acquisition: %v", err)
	}
}
