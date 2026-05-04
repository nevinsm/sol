//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"text/template"
)

// UnitName returns the systemd unit name for a component.
func UnitName(component string) string {
	return fmt.Sprintf("sol-%s.service", component)
}

const unitTemplate = `[Unit]
Description=Sol {{.Component}} daemon
After=network.target
{{- if .AfterUnits}}
After={{.AfterUnits}}
Wants={{.AfterUnits}}
{{- end}}

[Service]
Type=simple
ExecStart={{.ExecStart}}
Restart=on-failure
RestartSec=5
Environment=SOL_HOME={{.SOLHome}}

[Install]
WantedBy=default.target
`

var unitTmpl = template.Must(template.New("unit").Parse(unitTemplate))

type unitData struct {
	Component  string
	ExecStart  string
	SOLHome    string
	AfterUnits string
}

// prefectDeps lists the components that the prefect unit should start after.
// The prefect supervises these daemons, so it must come up last.
func prefectDeps() string {
	var deps []string
	for _, c := range Components {
		if c != "prefect" {
			deps = append(deps, UnitName(c))
		}
	}
	return strings.Join(deps, " ")
}

// GenerateUnit returns the systemd unit file content for a component.
func GenerateUnit(component, solBin, solHome string) (string, error) {
	var afterUnits string
	if component == "prefect" {
		afterUnits = prefectDeps()
	}

	var buf strings.Builder
	err := unitTmpl.Execute(&buf, unitData{
		Component:  component,
		ExecStart:  solBin + " " + component + " run",
		SOLHome:    solHome,
		AfterUnits: afterUnits,
	})
	if err != nil {
		return "", fmt.Errorf("failed to render unit template for %s: %w", component, err)
	}
	return buf.String(), nil
}

// unitDir returns ~/.config/systemd/user/.
func unitDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// LingerEnabled checks if loginctl enable-linger is set for the current user.
func LingerEnabled() bool {
	uid := fmt.Sprintf("%d", os.Getuid())
	// Prefer loginctl output as the authoritative source — it works correctly
	// in containers and sudo environments where $USER may not match the login name.
	out, err := exec.Command("loginctl", "show-user", uid, "--property=Linger").CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(out)) == "Linger=yes"
	}
	// Fallback: check linger file by username from os/user (more reliable than $USER).
	u, err := user.Current()
	if err != nil {
		return false
	}
	path := filepath.Join("/var/lib/systemd/linger", u.Username)
	_, statErr := os.Stat(path)
	return statErr == nil
}

// Install generates unit files, writes them to ~/.config/systemd/user/,
// runs daemon-reload, and enables (but does not start) each unit.
func Install(solBin, solHome string) error {
	dir, err := unitDir()
	if err != nil {
		return fmt.Errorf("failed to determine unit directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create unit directory %s: %w", dir, err)
	}

	// Track written unit files so we can clean up on error.
	var writtenPaths []string

	for _, comp := range Components {
		content, err := GenerateUnit(comp, solBin, solHome)
		if err != nil {
			return fmt.Errorf("failed to generate unit for %s: %w", comp, err)
		}
		path := filepath.Join(dir, UnitName(comp))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("failed to write unit file %s: %w", path, err)
		}
		writtenPaths = append(writtenPaths, path)
		fmt.Fprintf(os.Stderr, "Installed %s\n", path)
	}

	removeWritten := func() {
		for _, p := range writtenPaths {
			_ = os.Remove(p)
		}
	}

	if err := systemctl("daemon-reload"); err != nil {
		removeWritten()
		return fmt.Errorf("failed to reload systemd daemon: %w", err)
	}

	var enabledUnits []string
	for _, comp := range Components {
		unit := UnitName(comp)
		if err := systemctl("enable", unit); err != nil {
			// Disable previously-enabled units before removing files
			// to avoid leaving systemd in a dirty state.
			for _, u := range enabledUnits {
				_ = systemctl("disable", u)
			}
			removeWritten()
			return fmt.Errorf("failed to enable %s: %w", unit, err)
		}
		enabledUnits = append(enabledUnits, unit)
		fmt.Fprintf(os.Stderr, "Enabled %s\n", unit)
	}
	return nil
}

// Uninstall stops, disables, and removes unit files, then runs daemon-reload.
func Uninstall() error {
	dir, err := unitDir()
	if err != nil {
		return fmt.Errorf("failed to determine unit directory: %w", err)
	}

	for _, comp := range Components {
		unit := UnitName(comp)
		// Best-effort stop and disable; unit may not be running/enabled.
		_ = systemctl("stop", unit)
		_ = systemctl("disable", unit)

		path := filepath.Join(dir, unit)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
		fmt.Fprintf(os.Stderr, "Removed %s\n", path)
	}

	if err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("failed to reload systemd daemon: %w", err)
	}
	return nil
}

// Start starts all sol units.
func Start() error {
	return systemctlAll("start")
}

// stopAll stops all sol units in order. Returns the list of components that
// were successfully stopped before any failure, plus the failure (if any).
// On success, all Components are returned and err is nil.
func stopAll() (stopped []string, err error) {
	for _, comp := range Components {
		unit := UnitName(comp)
		if cerr := systemctl("stop", unit); cerr != nil {
			return stopped, fmt.Errorf("failed to stop %s: %w", unit, cerr)
		}
		stopped = append(stopped, comp)
	}
	return stopped, nil
}

// Stop stops all sol units.
func Stop() error {
	_, err := stopAll()
	return err
}

// Restart stops then starts all sol units.
//
// Restart is best-effort: it tries to leave the sphere with as many components
// running as possible. If Stop fails partway through, Restart attempts to
// restart any components that were already stopped (rollback toward "running")
// and surfaces both the original Stop failure and the rollback outcome.
//
// Mirrors the Darwin implementation so operators see the same recovery
// behavior on both platforms: a partial-stop failure does not leave units
// silently stopped without an attempt to bring them back.
func Restart() error {
	stopped, stopErr := stopAll()
	if stopErr != nil {
		// Best-effort rollback: restart any units we did stop, so we
		// never end up with fewer running daemons than we started with.
		var rollbackFailures []string
		for _, comp := range stopped {
			unit := UnitName(comp)
			if rerr := systemctl("start", unit); rerr != nil {
				rollbackFailures = append(rollbackFailures,
					fmt.Sprintf("%s: %v", unit, rerr))
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

// Status prints systemctl --user status for all sol units.
// Returns nil for running (exit 0), ErrServiceDegraded for inactive (exit 3),
// and an error for failed (exit 1) or not-found (exit 4) units.
func Status() error {
	args := []string{"--user", "status", "--no-pager"}
	for _, comp := range Components {
		args = append(args, UnitName(comp))
	}
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			switch exitErr.ExitCode() {
			case 3:
				// inactive — services installed but not running
				return ErrServiceDegraded
			case 1:
				return fmt.Errorf("one or more systemd units are in a failed state")
			case 4:
				return fmt.Errorf("one or more systemd units were not found")
			default:
				return fmt.Errorf("systemctl status exited with code %d", exitErr.ExitCode())
			}
		}
		return fmt.Errorf("failed to run systemctl status: %w", err)
	}
	return nil
}

// systemctl executes a `systemctl --user` command. It is a variable so tests
// can substitute a fake implementation.
var systemctl = func(args ...string) error {
	fullArgs := append([]string{"--user"}, args...)
	cmd := exec.Command("systemctl", fullArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func systemctlAll(action string) error {
	for _, comp := range Components {
		if err := systemctl(action, UnitName(comp)); err != nil {
			return err
		}
	}
	return nil
}
