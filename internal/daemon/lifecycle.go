// Package daemon provides a shared pidfile lifecycle for sol-managed Go
// daemons (prefect, consul, chronicle, ledger, broker, forge, sentinel).
//
// The Lifecycle struct describes a single daemon and the Start/Stop/Restart/
// RunBootstrap functions implement the flock-authoritative pidfile protocol
// that every daemon should share. This package exists as phase 0 of the
// daemon-lifecycle caravan — phase 1 migrates the individual daemon cmd
// files to use it.
//
// See writ sol-a0d18aac092e8ab4 and docs/decisions/ for the background on
// the pidfile-empty-but-daemon-running class of bugs this package defends
// against.
package daemon

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/processutil"
)

// daemonStartProbeDelay is how long Start waits after spawning the child
// before reading the pidfile to decide whether the child took the flock,
// exited cleanly via the already-running path, or crashed.
const daemonStartProbeDelay = 1 * time.Second

// defaultStopTimeout is used when Lifecycle.StopTimeout is zero.
const defaultStopTimeout = 10 * time.Second

// resolveSolBinary resolves the path to the running sol binary. It is a
// package-private indirection so tests can substitute a symlinked path whose
// basename is "sol", which is what FindSolSubcommandPIDs matches on.
var resolveSolBinary = os.Executable

// Lifecycle describes a single sol-managed daemon. Callers construct one of
// these per daemon and hand it to Start/Stop/Restart/RunBootstrap.
type Lifecycle struct {
	// Name is the display name used in log messages and errors. For
	// per-world daemons include the world: "forge[sol-dev]".
	Name string

	// PIDPath returns the pidfile path. Late-bound so per-world daemons
	// can close over the world variable without leaking world plumbing
	// into this package.
	PIDPath func() string

	// RunArgs is the argv (without the sol binary path) that starts the
	// daemon in foreground mode. Used both for spawning and for the proc
	// scan recovery path. Examples:
	//
	//   sphere daemon: []string{"consul", "run"}
	//   per-world:     []string{"forge", "run", "--world=sol-dev"}
	RunArgs []string

	// LogPath returns the path to the daemon's log file. The parent opens
	// this for stdout/stderr redirection when spawning.
	LogPath func() string

	// Env is the environment passed to the spawned child. When nil,
	// os.Environ() is used. Callers typically append SOL_HOME=<path> or
	// similar so the child inherits the correct sphere location.
	Env []string

	// PostSpawn, if non-nil, runs after the three-state classification
	// reports "started". Returning a non-nil error marks the daemon as
	// failed. Optional; used by daemons that need extra verification
	// beyond "process is alive" (e.g., heartbeat presence).
	PostSpawn func(pid int) error

	// PreStop, if non-nil, runs inside Stop before SIGTERM is sent.
	// Used for daemon-specific cleanup that must happen before the
	// process goes away (e.g., forge stops its active merge session).
	// Errors from PreStop are logged but do not prevent the SIGTERM.
	PreStop func() error

	// StopTimeout is the maximum time Stop will wait for SIGTERM to take
	// effect before escalating to SIGKILL. Defaults to 10 seconds when
	// zero.
	StopTimeout time.Duration
}

// StartResult reports what Start observed after spawning the daemon.
type StartResult struct {
	// Status is one of:
	//   "running" — another live instance already owned the pidfile; our
	//               spawned child exited cleanly via its flock conflict.
	//   "started" — our child acquired the pidfile and is alive.
	//   "failed"  — our child crashed or the pidfile is still empty.
	Status string

	// PID is the owning pid when Status is "running" or "started", and 0
	// when Status is "failed".
	PID int
}

// Start attempts to start the daemon described by lc.
//
// The protocol is:
//  1. If the pidfile already records a live pid, return {Status: "running"}
//     without spawning anything — this is the success-already-running path.
//  2. Clear a stale pidfile defensively (ClearPIDIfMatches with expected=0
//     which leaves foreign live pids alone).
//  3. Spawn <solBin> <lc.RunArgs...> via processutil.StartDaemon.
//  4. Wait daemonStartProbeDelay for the child to either take its own flock
//     via WritePID or exit.
//  5. Read the pidfile and classify the outcome.
//
// See classifyStart and writ sol-a0d18aac092e8ab4 for the race this is
// defending against.
func Start(lc Lifecycle) (StartResult, error) {
	pidPath := lc.PIDPath()

	// Step 1: already running?
	if p, _ := processutil.ReadPID(pidPath); p > 0 && processutil.IsRunning(p) {
		return StartResult{Status: "running", PID: p}, nil
	}

	// Step 2: clear stale pidfile without clobbering foreign live pids.
	// expected=0 means "only clear if the file is empty, dead, or unreadable".
	_ = processutil.ClearPIDIfMatches(pidPath, 0)

	// Step 3: resolve sol binary and spawn child.
	solBin, err := resolveSolBinary()
	if err != nil {
		return StartResult{Status: "failed"}, fmt.Errorf("%s: find sol binary: %w", lc.Name, err)
	}

	env := lc.Env
	if env == nil {
		env = os.Environ()
	}

	childPid, err := processutil.StartDaemon(lc.LogPath(), env, solBin, lc.RunArgs...)
	if err != nil {
		return StartResult{Status: "failed"}, fmt.Errorf("%s: spawn: %w", lc.Name, err)
	}

	// Step 4: wait briefly for the child to take the flock or exit.
	time.Sleep(daemonStartProbeDelay)

	// Step 5: classify.
	filePID, _ := processutil.ReadPID(pidPath)
	status, owner := classifyStart(filePID, childPid)
	switch status {
	case "running":
		// Another instance already owns the file. Our child exited cleanly
		// via its flock-fail path. Do NOT clobber the pidfile.
		return StartResult{Status: "running", PID: owner}, nil
	case "started":
		if lc.PostSpawn != nil {
			if err := lc.PostSpawn(childPid); err != nil {
				return StartResult{Status: "failed"}, fmt.Errorf("%s: post-spawn verification failed: %w", lc.Name, err)
			}
		}
		return StartResult{Status: "started", PID: childPid}, nil
	default:
		// Defensive clear: only truncate if the file still records our
		// child's pid (or is empty/stale). Never clobber a foreign live pid.
		_ = processutil.ClearPIDIfMatches(pidPath, childPid)
		return StartResult{Status: "failed"}, fmt.Errorf("%s: exited immediately (check %s)", lc.Name, lc.LogPath())
	}
}

// Stop stops the daemon described by lc. If the pidfile is empty or records
// a dead pid, Stop returns nil (nothing to stop is not an error).
//
// When the pidfile contains a live pid, Stop sends SIGTERM and polls every
// 500ms up to lc.StopTimeout (default 10s). If the process is still alive
// after the timeout, SIGKILL is sent and Stop polls briefly for it to die.
//
// Defensive pidfile clear: after the kill, Stop calls ClearPIDIfMatches with
// the pid it just targeted, so if a completely different process has taken
// over the pidfile in the interim (e.g. a fresh start from a separate code
// path), the new process's record is left alone.
func Stop(lc Lifecycle) error {
	if lc.PreStop != nil {
		if err := lc.PreStop(); err != nil {
			// Logged but non-fatal — proceed with SIGTERM regardless.
			fmt.Fprintf(os.Stderr, "%s: pre-stop hook error: %v\n", lc.Name, err)
		}
	}

	pidPath := lc.PIDPath()
	pid, _ := processutil.ReadPID(pidPath)
	if pid <= 0 || !processutil.IsRunning(pid) {
		// Nothing to stop. Defensive clear in case the file records a
		// stale pid; leaves foreign live pids alone.
		_ = processutil.ClearPIDIfMatches(pidPath, 0)
		return nil
	}

	timeout := lc.StopTimeout
	if timeout == 0 {
		timeout = defaultStopTimeout
	}

	// SIGTERM + poll.
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if !processutil.IsRunning(pid) {
			_ = processutil.ClearPIDIfMatches(pidPath, pid)
			return nil
		}
		return fmt.Errorf("%s: SIGTERM pid %d: %w", lc.Name, pid, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processutil.IsRunning(pid) {
			_ = processutil.ClearPIDIfMatches(pidPath, pid)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Escalate to SIGKILL.
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		if !processutil.IsRunning(pid) {
			_ = processutil.ClearPIDIfMatches(pidPath, pid)
			return nil
		}
		return fmt.Errorf("%s: SIGKILL pid %d: %w", lc.Name, pid, err)
	}
	for range 20 {
		if !processutil.IsRunning(pid) {
			_ = processutil.ClearPIDIfMatches(pidPath, pid)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("%s: pid %d did not exit after SIGKILL", lc.Name, pid)
}

// Restart stops then starts the daemon.
//
// Beyond a plain Stop+Start, Restart includes the pidfile-empty recovery
// path: if Stop found nothing to stop because the pidfile is empty but a
// process matching lc.RunArgs is still running (the bug class that motivated
// this package), Restart locates the orphan via FindSolSubcommandPIDs and
// kills it before starting a new instance.
//
// If the proc scan finds multiple matches, Restart refuses to guess and
// returns an error so the operator can resolve manually.
func Restart(lc Lifecycle) error {
	if err := Stop(lc); err != nil {
		return err
	}

	// Orphan recovery: after Stop the pidfile should be empty. If a
	// process matching RunArgs is still alive, clean it up.
	if pid, _ := processutil.ReadPID(lc.PIDPath()); pid == 0 {
		pids, err := processutil.FindSolSubcommandPIDs(lc.RunArgs...)
		if err != nil {
			return fmt.Errorf("%s: scan for orphans: %w", lc.Name, err)
		}
		switch len(pids) {
		case 0:
			// Nothing to recover.
		case 1:
			orphan := pids[0]
			fmt.Fprintf(os.Stderr,
				"%s: pidfile empty but found running process at pid %d via proc scan; killing to proceed with restart\n",
				lc.Name, orphan)
			if err := processutil.GracefulKill(orphan, 5*time.Second); err != nil {
				return fmt.Errorf("%s: kill orphan pid %d: %w", lc.Name, orphan, err)
			}
			_ = processutil.ClearPIDIfMatches(lc.PIDPath(), orphan)
		default:
			return fmt.Errorf(
				"%s: pidfile empty and multiple (%d) processes found via proc scan; "+
					"refusing to guess which to kill — resolve manually and retry",
				lc.Name, len(pids))
		}
	}

	if _, err := Start(lc); err != nil {
		return err
	}
	return nil
}

// RunBootstrap acquires the pidfile flock for the current process. It is
// intended to be called from the top of a daemon's `run` subcommand:
//
//	release, err := daemon.RunBootstrap(ledgerLifecycle)
//	if err != nil {
//	    return fmt.Errorf("ledger run: %w", err)
//	}
//	defer release()
//
// If the flock cannot be acquired because another live instance already
// holds it, RunBootstrap returns a non-nil error; the caller's run command
// MUST treat this as fatal and return the error. There are no more silent
// "warning: failed to write PID file" continues.
//
// On success, the returned release closure truncates the pidfile and
// releases the flock; callers should defer it so the file is cleared on
// normal exit.
func RunBootstrap(lc Lifecycle) (func(), error) {
	pidPath := lc.PIDPath()
	if err := processutil.WritePID(pidPath, os.Getpid()); err != nil {
		return nil, fmt.Errorf("%s: acquire pidfile lock: %w", lc.Name, err)
	}
	return func() { _ = processutil.ClearPID(pidPath) }, nil
}

// classifyStart determines what happened after a parent spawned a daemon
// child and waited briefly for it to take ownership of the pidfile. filePID
// is the pid currently recorded in the file (0 if empty or unreadable);
// childPid is the pid of the process we just spawned.
//
// Returns one of:
//   - "running": the pidfile records a live pid that is NOT our child. Our
//     child correctly detected another instance via its own WritePID flock
//     failing and exited cleanly — this is a success. The caller must NOT
//     clear the pidfile; the other instance owns it. ownerPID is filePID.
//   - "started": the pidfile records our child's pid and our child is alive.
//     The normal successful-start path. ownerPID is childPid.
//   - "failed":  the pidfile is empty, stale, or our child is dead. The
//     caller should use the defensive clearer (ClearPIDIfMatches) to avoid
//     clobbering another instance's file. ownerPID is 0.
//
// See writ sol-a0d18aac092e8ab4 for the race this classification fixes. An
// identical helper lives in cmd/up.go for now; phase 1 of the daemon-
// lifecycle caravan removes that duplicate in favor of this one.
func classifyStart(filePID, childPid int) (status string, ownerPID int) {
	if filePID > 0 && filePID != childPid && processutil.IsRunning(filePID) {
		return "running", filePID
	}
	if filePID == childPid && processutil.IsRunning(childPid) {
		return "started", childPid
	}
	return "failed", 0
}
