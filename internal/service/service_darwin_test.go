//go:build darwin

package service

import (
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

func TestPlistPath(t *testing.T) {
	got := plistPath("/tmp/agents", "consul")
	want := filepath.Join("/tmp/agents", "com.sol.consul.plist")
	if got != want {
		t.Errorf("plistPath = %q, want %q", got, want)
	}
}
