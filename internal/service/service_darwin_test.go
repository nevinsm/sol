//go:build darwin

package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSuccess(t *testing.T) {
	dir := t.TempDir()
	origDir := launchAgentsDir
	launchAgentsDir = func() (string, error) { return dir, nil }
	defer func() { launchAgentsDir = origDir }()

	var loadedPaths []string
	origLaunchctl := launchctl
	launchctl = func(args ...string) error {
		if args[0] == "load" {
			loadedPaths = append(loadedPaths, args[1])
		}
		return nil
	}
	defer func() { launchctl = origLaunchctl }()

	if err := Install("/usr/local/bin/sol", "/Users/test/sol"); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// All component plists should exist.
	for _, comp := range Components {
		path := plistPath(dir, comp)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected plist %s to exist: %v", path, err)
		}
	}

	// All components should have been loaded.
	if len(loadedPaths) != len(Components) {
		t.Errorf("expected %d loads, got %d", len(Components), len(loadedPaths))
	}
}

func TestInstallRollbackOnLoadFailure(t *testing.T) {
	dir := t.TempDir()
	origDir := launchAgentsDir
	launchAgentsDir = func() (string, error) { return dir, nil }
	defer func() { launchAgentsDir = origDir }()

	// Fail on the third component's load.
	failAt := 2
	loadCount := 0
	var unloadedPaths []string

	origLaunchctl := launchctl
	launchctl = func(args ...string) error {
		switch args[0] {
		case "load":
			if loadCount == failAt {
				return fmt.Errorf("launchctl load failed: simulated")
			}
			loadCount++
			return nil
		case "unload":
			unloadedPaths = append(unloadedPaths, args[1])
			return nil
		}
		return nil
	}
	defer func() { launchctl = origLaunchctl }()

	err := Install("/usr/local/bin/sol", "/Users/test/sol")
	if err == nil {
		t.Fatal("expected Install to fail")
	}

	// All plist files should have been removed (components 0..failAt).
	for _, comp := range Components {
		path := plistPath(dir, comp)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("expected plist %s to be removed after rollback", path)
		}
	}

	// Previously-loaded components (0..failAt-1) should have been unloaded.
	if len(unloadedPaths) != failAt {
		t.Errorf("expected %d unloads, got %d: %v", failAt, len(unloadedPaths), unloadedPaths)
	}

	// Verify unloaded paths match the first failAt components.
	for i := 0; i < failAt; i++ {
		expected := plistPath(dir, Components[i])
		if i < len(unloadedPaths) && unloadedPaths[i] != expected {
			t.Errorf("unload[%d] = %q, want %q", i, unloadedPaths[i], expected)
		}
	}
}

func TestInstallRollbackOnFirstLoadFailure(t *testing.T) {
	dir := t.TempDir()
	origDir := launchAgentsDir
	launchAgentsDir = func() (string, error) { return dir, nil }
	defer func() { launchAgentsDir = origDir }()

	// Fail on the very first component's load.
	var unloadedPaths []string
	origLaunchctl := launchctl
	launchctl = func(args ...string) error {
		switch args[0] {
		case "load":
			return fmt.Errorf("launchctl load failed: simulated")
		case "unload":
			unloadedPaths = append(unloadedPaths, args[1])
			return nil
		}
		return nil
	}
	defer func() { launchctl = origLaunchctl }()

	err := Install("/usr/local/bin/sol", "/Users/test/sol")
	if err == nil {
		t.Fatal("expected Install to fail")
	}

	// The first component's plist should be removed.
	path := plistPath(dir, Components[0])
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected plist %s to be removed after rollback", path)
	}

	// No components were loaded, so no unloads should occur.
	if len(unloadedPaths) != 0 {
		t.Errorf("expected 0 unloads, got %d", len(unloadedPaths))
	}
}

func TestInstallRollbackOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	origDir := launchAgentsDir
	launchAgentsDir = func() (string, error) { return dir, nil }
	defer func() { launchAgentsDir = origDir }()

	loadCount := 0
	var unloadedPaths []string
	origLaunchctl := launchctl
	launchctl = func(args ...string) error {
		switch args[0] {
		case "load":
			loadCount++
			return nil
		case "unload":
			unloadedPaths = append(unloadedPaths, args[1])
			return nil
		}
		return nil
	}
	defer func() { launchctl = origLaunchctl }()

	// Make the plist path for the second component unwritable by creating
	// a directory in its place.
	blockerPath := plistPath(dir, Components[1])
	if err := os.MkdirAll(blockerPath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := Install("/usr/local/bin/sol", "/Users/test/sol")
	if err == nil {
		t.Fatal("expected Install to fail on write")
	}
	if !strings.Contains(err.Error(), "failed to write plist file") {
		t.Fatalf("unexpected error: %v", err)
	}

	// The first component was loaded, so it should be unloaded during rollback.
	if len(unloadedPaths) != 1 {
		t.Errorf("expected 1 unload, got %d: %v", len(unloadedPaths), unloadedPaths)
	}

	// The first component's plist should be removed.
	firstPath := plistPath(dir, Components[0])
	if _, statErr := os.Stat(firstPath); !os.IsNotExist(statErr) {
		t.Errorf("expected plist %s to be removed after rollback", firstPath)
	}
}

// fakeListOutput builds a `launchctl list`-style table where every component
// in `running` is reported with a numeric PID, every component in `stopped`
// is reported with PID "-" and exit status 0, and every component in `failed`
// is reported with PID "-" and a non-zero exit status. Components not present
// in any list are omitted (so they will be classified as "unknown").
func fakeListOutput(running, stopped, failed []string) string {
	var b strings.Builder
	b.WriteString("PID\tStatus\tLabel\n")
	for i, c := range running {
		fmt.Fprintf(&b, "%d\t0\t%s\n", 1000+i, ServiceLabel(c))
	}
	for _, c := range stopped {
		fmt.Fprintf(&b, "-\t0\t%s\n", ServiceLabel(c))
	}
	for _, c := range failed {
		fmt.Fprintf(&b, "-\t1\t%s\n", ServiceLabel(c))
	}
	// A few non-sol entries to ensure parser ignores them.
	b.WriteString("-\t0\tcom.apple.something\n")
	b.WriteString("42\t0\tcom.example.other\n")
	return b.String()
}

func TestStatusAllRunning(t *testing.T) {
	origList := launchctlList
	launchctlList = func() (string, error) {
		return fakeListOutput(Components, nil, nil), nil
	}
	defer func() { launchctlList = origList }()

	if err := Status(); err != nil {
		t.Fatalf("Status returned error when all components running: %v", err)
	}
}

func TestStatusReportsStoppedDaemon(t *testing.T) {
	// One component stopped, the rest running.
	stoppedComp := "consul"
	var running []string
	for _, c := range Components {
		if c != stoppedComp {
			running = append(running, c)
		}
	}

	origList := launchctlList
	launchctlList = func() (string, error) {
		return fakeListOutput(running, []string{stoppedComp}, nil), nil
	}
	defer func() { launchctlList = origList }()

	err := Status()
	if err == nil {
		t.Fatal("Status should return an error when a component is stopped")
	}
	if !errors.Is(err, ErrServiceDegraded) {
		t.Errorf("Status error = %v, want ErrServiceDegraded", err)
	}
}

func TestStatusReportsFailedDaemon(t *testing.T) {
	origList := launchctlList
	launchctlList = func() (string, error) {
		return fakeListOutput(nil, nil, Components), nil
	}
	defer func() { launchctlList = origList }()

	err := Status()
	if !errors.Is(err, ErrServiceDegraded) {
		t.Errorf("Status error = %v, want ErrServiceDegraded", err)
	}
}

func TestStatusReportsUnknownDaemon(t *testing.T) {
	// Empty list — no sol components reported at all.
	origList := launchctlList
	launchctlList = func() (string, error) {
		return "PID\tStatus\tLabel\n-\t0\tcom.apple.foo\n", nil
	}
	defer func() { launchctlList = origList }()

	err := Status()
	if !errors.Is(err, ErrServiceDegraded) {
		t.Errorf("Status error = %v, want ErrServiceDegraded", err)
	}
}

func TestStatusPropagatesListError(t *testing.T) {
	origList := launchctlList
	launchctlList = func() (string, error) {
		return "", fmt.Errorf("launchctl list: not found")
	}
	defer func() { launchctlList = origList }()

	err := Status()
	if err == nil {
		t.Fatal("Status should propagate launchctl list errors")
	}
	if errors.Is(err, ErrServiceDegraded) {
		t.Errorf("Status should not return ErrServiceDegraded for command failure: %v", err)
	}
}

func TestParseLaunchctlList(t *testing.T) {
	out := "PID\tStatus\tLabel\n" +
		"1234\t0\tcom.sol.prefect\n" +
		"-\t0\tcom.sol.consul\n" +
		"-\t1\tcom.sol.chronicle\n" +
		"42\t0\tcom.apple.unrelated\n"

	states := parseLaunchctlList(out)

	checks := map[string]componentState{
		"com.sol.prefect":     stateRunning,
		"com.sol.consul":      stateStopped,
		"com.sol.chronicle":   stateFailed,
		"com.apple.unrelated": stateRunning,
	}
	for label, want := range checks {
		if got := states[label]; got != want {
			t.Errorf("state[%s] = %v, want %v", label, got, want)
		}
	}
}

func TestRestartHappyPath(t *testing.T) {
	var calls []string
	origLaunchctl := launchctl
	launchctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	}
	defer func() { launchctl = origLaunchctl }()

	if err := Restart(); err != nil {
		t.Fatalf("Restart failed: %v", err)
	}

	// Expect a stop+start for every component.
	wantCalls := 2 * len(Components)
	if len(calls) != wantCalls {
		t.Errorf("expected %d launchctl calls, got %d: %v", wantCalls, len(calls), calls)
	}
}

func TestRestartRollsBackOnPartialStopFailure(t *testing.T) {
	// Fail Stop on the third component. The first two should have been
	// stopped, and Restart should attempt to start them again.
	failAt := 2
	stopCount := 0
	var calls []string

	origLaunchctl := launchctl
	launchctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "stop":
			if stopCount == failAt {
				return fmt.Errorf("simulated stop failure for %s", args[1])
			}
			stopCount++
			return nil
		}
		return nil
	}
	defer func() { launchctl = origLaunchctl }()

	err := Restart()
	if err == nil {
		t.Fatal("Restart should have returned an error after partial stop failure")
	}
	if !strings.Contains(err.Error(), "restart failed") {
		t.Errorf("error should mention restart failure: %v", err)
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("error should mention rollback outcome: %v", err)
	}

	// Expected call sequence:
	//   stop comp[0] (ok), stop comp[1] (ok), stop comp[2] (fail),
	//   start comp[0], start comp[1]
	wantStops := failAt + 1 // includes the failing one
	wantStarts := failAt    // rollback restarts the previously-stopped components
	stops := 0
	starts := 0
	for _, c := range calls {
		switch {
		case strings.HasPrefix(c, "stop "):
			stops++
		case strings.HasPrefix(c, "start "):
			starts++
		}
	}
	if stops != wantStops {
		t.Errorf("got %d stop calls, want %d (calls: %v)", stops, wantStops, calls)
	}
	if starts != wantStarts {
		t.Errorf("got %d start calls (rollback), want %d (calls: %v)", starts, wantStarts, calls)
	}

	// Confirm the rollback restarts targeted exactly the previously-stopped
	// components, in order.
	for i := 0; i < failAt; i++ {
		want := "start " + ServiceLabel(Components[i])
		found := false
		for _, c := range calls {
			if c == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected rollback call %q in %v", want, calls)
		}
	}
}

func TestRestartReportsRollbackFailure(t *testing.T) {
	// Stop fails on second component; rollback start also fails.
	stopCount := 0
	origLaunchctl := launchctl
	launchctl = func(args ...string) error {
		switch args[0] {
		case "stop":
			if stopCount == 1 {
				return fmt.Errorf("simulated stop failure")
			}
			stopCount++
			return nil
		case "start":
			return fmt.Errorf("simulated start failure")
		}
		return nil
	}
	defer func() { launchctl = origLaunchctl }()

	err := Restart()
	if err == nil {
		t.Fatal("Restart should fail when both stop and rollback fail")
	}
	if !strings.Contains(err.Error(), "rollback also failed") {
		t.Errorf("error should mention rollback failure: %v", err)
	}
}

func TestPlistPath(t *testing.T) {
	got := plistPath("/tmp/agents", "consul")
	want := filepath.Join("/tmp/agents", "com.sol.consul.plist")
	if got != want {
		t.Errorf("plistPath = %q, want %q", got, want)
	}
}
