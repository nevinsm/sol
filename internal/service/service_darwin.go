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
func launchAgentsDir() (string, error) {
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
func Install(solBin, solHome string) error {
	dir, err := launchAgentsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory %s: %w", dir, err)
	}

	for _, comp := range Components {
		content, err := GeneratePlist(comp, solBin, solHome)
		if err != nil {
			return fmt.Errorf("failed to generate plist for %s: %w", comp, err)
		}
		path := plistPath(dir, comp)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("failed to write plist file %s: %w", path, err)
		}
		fmt.Fprintf(os.Stderr, "Installed %s\n", path)

		if err := launchctl("load", path); err != nil {
			return fmt.Errorf("failed to load %s: %w", path, err)
		}
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

// Stop stops all sol LaunchAgents.
func Stop() error {
	for _, comp := range Components {
		label := ServiceLabel(comp)
		if err := launchctl("stop", label); err != nil {
			return fmt.Errorf("failed to stop %s: %w", label, err)
		}
	}
	return nil
}

// Restart stops then starts all sol LaunchAgents.
func Restart() error {
	if err := Stop(); err != nil {
		return err
	}
	return Start()
}

// Status lists loaded sol LaunchAgents by filtering launchctl list output.
func Status() error {
	cmd := exec.Command("launchctl", "list")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run launchctl list: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "com.sol.") {
			if !found {
				// Print header from first line of launchctl list output.
				if len(lines) > 0 && strings.HasPrefix(lines[0], "PID") {
					fmt.Println(lines[0])
				}
				found = true
			}
			fmt.Println(line)
		}
	}
	if !found {
		fmt.Println("No sol LaunchAgents are currently loaded.")
	}
	return nil
}

func launchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launchctl %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}
