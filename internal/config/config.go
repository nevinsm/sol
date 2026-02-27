package config

import (
	"os"
	"path/filepath"
)

// Home returns the SOL_HOME directory. Defaults to ~/sol.
func Home() string {
	if v := os.Getenv("SOL_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "sol")
	}
	return filepath.Join(home, "sol")
}

// StoreDir returns the path to $SOL_HOME/.store/.
func StoreDir() string {
	return filepath.Join(Home(), ".store")
}

// RuntimeDir returns the path to $SOL_HOME/.runtime/.
func RuntimeDir() string {
	return filepath.Join(Home(), ".runtime")
}

// WorldDir returns the path to $SOL_HOME/{world}/.
func WorldDir(world string) string {
	return filepath.Join(Home(), world)
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
