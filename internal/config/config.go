package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// SessionName returns the tmux session name for an agent.
func SessionName(world, agentName string) string {
	return fmt.Sprintf("sol-%s-%s", world, agentName)
}

// WorktreePath returns the worktree directory for an agent.
func WorktreePath(world, agentName string) string {
	return filepath.Join(Home(), world, "outposts", agentName, "worktree")
}

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

var validAgentName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]*$`)

const maxAgentNameLen = 64

// ValidateAgentName checks that an agent name contains only safe characters.
// Names must start with a letter, contain only [a-zA-Z0-9._-], and be at most 64 chars.
func ValidateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name must not be empty")
	}
	if len(name) > maxAgentNameLen {
		return fmt.Errorf("agent name %q is too long (%d chars, max %d)", name, len(name), maxAgentNameLen)
	}
	if !validAgentName.MatchString(name) {
		return fmt.Errorf("invalid agent name %q: must start with a letter and contain only [a-zA-Z0-9._-]", name)
	}
	return nil
}

var validWorldName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

var reservedWorldNames = map[string]bool{
	"store":    true,
	"runtime":  true,
	"sol":      true,
	"formulas": true,
}

const maxWorldNameLen = 64

// ValidateWorldName checks that a world name contains only safe characters.
func ValidateWorldName(name string) error {
	if name == "" {
		return fmt.Errorf("world name must not be empty")
	}
	if len(name) > maxWorldNameLen {
		return fmt.Errorf("world name %q is too long (%d chars, max %d)", name, len(name), maxWorldNameLen)
	}
	if !validWorldName.MatchString(name) {
		return fmt.Errorf("invalid world name %q: must match [a-zA-Z0-9][a-zA-Z0-9_-]*", name)
	}
	if reservedWorldNames[name] {
		return fmt.Errorf("world name %q is reserved", name)
	}
	return nil
}

// RequireWorld checks that a world has been initialized.
// Returns nil if world.toml exists at $SOL_HOME/{world}/world.toml.
//
// Distinguishes two error cases:
// - Pre-Arc1 world (DB exists but no world.toml): tells user to run
//   "sol world init <world>" to adopt the existing world.
// - Nonexistent world: tells user to run "sol world init <world>".
func RequireWorld(world string) error {
	if err := ValidateWorldName(world); err != nil {
		return err
	}
	path := WorldConfigPath(world)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Check if this is a pre-Arc1 world (DB exists but no config).
		dbPath := filepath.Join(StoreDir(), world+".db")
		if _, err := os.Stat(dbPath); err == nil {
			return fmt.Errorf("world %q was created before world lifecycle management; "+
				"run: sol world init %s", world, world)
		}
		return fmt.Errorf("world %q does not exist; run: sol world init %s", world, world)
	} else if err != nil {
		return fmt.Errorf("failed to check world %q: %w", world, err)
	}
	return nil
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
