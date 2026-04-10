//go:build darwin

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	svc "github.com/nevinsm/sol/internal/service"
)

const managerName = "launchd"

// plistDir returns ~/Library/LaunchAgents/.
func plistDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

// QueryStatuses queries launchctl for the state of all sol components and
// returns a ServiceStatus per component.
func QueryStatuses() ([]ServiceStatus, error) {
	dir, err := plistDir()
	if err != nil {
		return nil, err
	}

	// Parse launchctl list for running state.
	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run launchctl list: %w", err)
	}
	states := parseLaunchctlList(string(out))

	var result []ServiceStatus
	for _, comp := range svc.Components {
		label := svc.ServiceLabel(comp)
		path := filepath.Join(dir, label+".plist")

		s := ServiceStatus{
			Name:    comp,
			Manager: managerName,
		}

		if _, err := os.Stat(path); err == nil {
			s.Installed = true
			s.UnitPath = path
			// On macOS, a loaded plist is effectively enabled.
			s.Enabled = true
		}

		if st, ok := states[label]; ok {
			s.Active = st == "running"
		}

		result = append(result, s)
	}
	return result, nil
}

// parseLaunchctlList parses tab-separated `launchctl list` output and returns
// a map from service label to state string ("running", "stopped", "failed").
func parseLaunchctlList(out string) map[string]string {
	states := make(map[string]string)
	for i, line := range strings.Split(out, "\n") {
		if i == 0 && strings.HasPrefix(line, "PID") {
			continue
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
			states[label] = "running"
		case status != "0":
			states[label] = "failed"
		default:
			states[label] = "stopped"
		}
	}
	return states
}
