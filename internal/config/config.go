package config

import (
	"os"
	"path/filepath"
)

// Home returns the GT_HOME directory. Defaults to ~/gt.
func Home() string {
	if v := os.Getenv("GT_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "gt")
	}
	return filepath.Join(home, "gt")
}

// StoreDir returns the path to $GT_HOME/.store/.
func StoreDir() string {
	return filepath.Join(Home(), ".store")
}

// RuntimeDir returns the path to $GT_HOME/.runtime/.
func RuntimeDir() string {
	return filepath.Join(Home(), ".runtime")
}

// RigDir returns the path to $GT_HOME/{rig}/.
func RigDir(rig string) string {
	return filepath.Join(Home(), rig)
}

// EnsureDirs creates .store/ and .runtime/ if they don't exist.
func EnsureDirs() error {
	for _, dir := range []string{StoreDir(), RuntimeDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}
