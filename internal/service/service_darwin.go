//go:build darwin

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// launchAgentsDir returns ~/Library/LaunchAgents/.
// It is a variable so tests can substitute a temp directory.
var launchAgentsDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

// plistPath returns the full path for a component's plist file.
func plistPath(dir, component string) string {
	return filepath.Join(dir, ServiceLabel(component)+".plist")
}

// LingerEnabled returns true unconditionally on macOS.
// LaunchAgents persist for the logged-in user by default; there is no
// equivalent of loginctl enable-linger.
func LingerEnabled() bool {
	return true
}

// Install generates plist files to ~/Library/LaunchAgents/ and loads each.
// If launchctl load fails for any component, all previously-loaded components
// are unloaded and all written plist files are removed.
func Install(solBin, solHome string) error {
	dir, err := launchAgentsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory %s: %w", dir, err)
	}

	// Track written plist paths and loaded plist paths so we can
	// roll back on partial failure (mirrors Linux Install pattern).
	var writtenPaths []string
	var loadedPaths []string

	rollback := func() {
		for _, p := range loadedPaths {
			_ = launchctl("unload", p)
		}
		for _, p := range writtenPaths {
			_ = os.Remove(p)
		}
	}

	for _, comp := range Components {
		content, err := GeneratePlist(comp, solBin, solHome)
		if err != nil {
			rollback()
			return fmt.Errorf("failed to generate plist for %s: %w", comp, err)
		}
		path := plistPath(dir, comp)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			rollback()
			return fmt.Errorf("failed to write plist file %s: %w", path, err)
		}
		writtenPaths = append(writtenPaths, path)
		fmt.Fprintf(os.Stderr, "Installed %s\n", path)

		if err := launchctl("load", path); err != nil {
			rollback()
			return fmt.Errorf("failed to load %s: %w", path, err)
		}
		loadedPaths = append(loadedPaths, path)
		fmt.Fprintf(os.Stderr, "Loaded %s\n", ServiceLabel(comp))
	}
	return nil
}

// Uninstall unloads each agent and removes plist files.
func Uninstall() error {
	dir, err := launchAgentsDir()
	if err != nil {
		return err
	}

	for _, comp := range Components {
		path := plistPath(dir, comp)
		// Best-effort unload; agent may not be loaded.
		_ = launchctl("unload", path)

		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
		fmt.Fprintf(os.Stderr, "Removed %s\n", path)
	}
	return nil
}

// Start starts all sol LaunchAgents.
func Start() error {
	for _, comp := range Components {
		label := ServiceLabel(comp)
		if err := launchctl("start", label); err != nil {
			return fmt.Errorf("failed to start %s: %w", label, err)
		}
	}
	return nil
}

// stopAll stops all sol LaunchAgents in order. Returns the list of components
// that were successfully stopped before any failure, plus the failure (if any).
// On success, all Components are returned and err is nil.
func stopAll() (stopped []string, err error) {
	for _, comp := range Components {
		label := ServiceLabel(comp)
		if cerr := launchctl("stop", label); cerr != nil {
			return stopped, fmt.Errorf("failed to stop %s: %w", label, cerr)
		}
		stopped = append(stopped, comp)
	}
	return stopped, nil
}

// Stop stops all sol LaunchAgents.
func Stop() error {
	_, err := stopAll()
	return err
}

// Restart stops then starts all sol LaunchAgents.
//
// Restart is best-effort: it tries to leave the sphere with as many components
// running as possible. If Stop fails partway through, Restart attempts to
// restart any components that were already stopped (rollback toward "running")
// and surfaces both the original Stop failure and the rollback outcome.
func Restart() error {
	stopped, stopErr := stopAll()
	if stopErr != nil {
		// Best-effort rollback: restart any components we did stop, so we
		// never end up with fewer running daemons than we started with.
		var rollbackFailures []string
		for _, comp := range stopped {
			label := ServiceLabel(comp)
			if rerr := launchctl("start", label); rerr != nil {
				rollbackFailures = append(rollbackFailures,
					fmt.Sprintf("%s: %v", label, rerr))
			}
		}
		if len(rollbackFailures) > 0 {
			return fmt.Errorf("restart failed: %w; rollback also failed for: %s",
				stopErr, strings.Join(rollbackFailures, "; "))
		}
		return fmt.Errorf("restart failed: %w; rolled back %d previously-stopped component(s)",
			stopErr, len(stopped))
	}
	return Start()
}

// componentState describes the runtime state of a single sol LaunchAgent
// as reported by `launchctl list`.
type componentState int

const (
	stateUnknown    componentState = iota // launchctl list didn't include this label
	stateRunning                          // PID column is a positive integer
	stateStopped                          // PID column is "-" and last exit status is 0
	stateFailed                           // PID column is "-" and last exit status is non-zero
)

func (s componentState) String() string {
	switch s {
	case stateRunning:
		return "running"
	case stateStopped:
		return "stopped"
	case stateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// launchctlList returns the raw output of `launchctl list`. It is a variable
// so tests can substitute a fake implementation.
var launchctlList = func() (string, error) {
	cmd := exec.Command("launchctl", "list")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run launchctl list: %w", err)
	}
	return string(out), nil
}

// parseLaunchctlList parses tab-separated `launchctl list` output and returns
// a map from service label to its componentState. The output format is:
//
//	PID	Status	Label
//	-	0	com.apple.foo
//	123	0	com.sol.prefect
//	-	1	com.sol.consul
func parseLaunchctlList(out string) map[string]componentState {
	states := make(map[string]componentState)
	for i, line := range strings.Split(out, "\n") {
		if i == 0 && strings.HasPrefix(line, "PID") {
			continue // header
		}
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, status, label := fields[0], fields[1], fields[2]
		switch {
		case pid != "-":
			states[label] = stateRunning
		case status != "0":
			states[label] = stateFailed
		default:
			states[label] = stateStopped
		}
	}
	return states
}

// Status inspects each sol LaunchAgent via `launchctl list` and prints
// per-component state. It returns ErrServiceDegraded if any component is
// stopped, failed, or unknown to launchctl.
//
// This mirrors the Linux implementation's contract: a non-nil error is
// returned when the daemons are not all healthy, allowing the CLI to surface
// a non-zero exit code to monitoring scripts.
func Status() error {
	out, err := launchctlList()
	if err != nil {
		return err
	}
	states := parseLaunchctlList(out)

	fmt.Println("LABEL\tSTATE\tCOMPONENT")
	degraded := false
	for _, comp := range Components {
		label := ServiceLabel(comp)
		state, ok := states[label]
		if !ok {
			state = stateUnknown
		}
		if state != stateRunning {
			degraded = true
		}
		fmt.Printf("%s\t%s\t%s\n", label, state, comp)
	}
	if degraded {
		return ErrServiceDegraded
	}
	return nil
}

// launchctl executes a launchctl command. It is a variable so tests can
// substitute a fake implementation.
var launchctl = func(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launchctl %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}
