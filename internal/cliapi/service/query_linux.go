//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	svc "github.com/nevinsm/sol/internal/service"
)

const managerName = "systemd"

// unitDir returns ~/.config/systemd/user/.
func unitDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// QueryStatuses queries systemd for the state of all sol components and
// returns a ServiceStatus per component.
func QueryStatuses() ([]ServiceStatus, error) {
	dir, err := unitDir()
	if err != nil {
		return nil, err
	}

	var result []ServiceStatus
	for _, comp := range svc.Components {
		unit := svc.UnitName(comp)
		path := filepath.Join(dir, unit)

		s := ServiceStatus{
			Name:    comp,
			Manager: managerName,
		}

		if _, err := os.Stat(path); err == nil {
			s.Installed = true
			s.UnitPath = path
		}

		// Query active state.
		if out, err := exec.Command("systemctl", "--user", "is-active", unit).Output(); err == nil {
			s.Active = strings.TrimSpace(string(out)) == "active"
		}

		// Query enabled state.
		if out, err := exec.Command("systemctl", "--user", "is-enabled", unit).Output(); err == nil {
			s.Enabled = strings.TrimSpace(string(out)) == "enabled"
		}

		result = append(result, s)
	}
	return result, nil
}
