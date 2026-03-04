package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SessionName returns the tmux session name for an agent.
func SessionName(world, agentName string) string {
	return fmt.Sprintf("sol-%s-%s", world, agentName)
}

// AgentDir returns the base directory for an agent based on its role.
// - "agent" (default) → $SOL_HOME/{world}/outposts/{agentName}
// - "envoy"           → $SOL_HOME/{world}/envoys/{agentName}
// - "governor"        → $SOL_HOME/{world}/governor
// - "forge"           → $SOL_HOME/{world}/forge
func AgentDir(world, agentName, role string) string {
	switch role {
	case "envoy":
		return filepath.Join(Home(), world, "envoys", agentName)
	case "governor":
		return filepath.Join(Home(), world, "governor")
	case "forge":
		return filepath.Join(Home(), world, "forge")
	default:
		return filepath.Join(Home(), world, "outposts", agentName)
	}
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

// RepoPath returns the path to the managed git clone for a world.
func RepoPath(world string) string {
	return filepath.Join(WorldDir(world), "repo")
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

// ResolveWorld determines the world name from available sources.
// Precedence: explicit flag value > SOL_WORLD env var > detect from cwd.
// After resolution, validates the world exists via RequireWorld.
func ResolveWorld(flagValue string) (string, error) {
	world := flagValue

	if world == "" {
		world = os.Getenv("SOL_WORLD")
	}

	if world == "" {
		world = detectWorldFromCwd()
	}

	if world == "" {
		return "", fmt.Errorf("--world is required (or set SOL_WORLD, or run from inside a world directory)")
	}

	if err := RequireWorld(world); err != nil {
		return "", err
	}

	return world, nil
}

// detectWorldFromCwd attempts to infer the world name from the current
// working directory. If cwd is under $SOL_HOME/{world}/, returns world.
func detectWorldFromCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	home := Home()
	rel, err := filepath.Rel(home, cwd)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	// rel is like "myworld/outposts/Toast/worktree" or "myworld"
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if len(parts) == 0 {
		return ""
	}
	candidate := parts[0]
	// Skip internal directories.
	if candidate == ".store" || candidate == ".runtime" {
		return ""
	}
	return candidate
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

// DefaultSessionCommand is the default command used to start agent sessions.
const DefaultSessionCommand = "claude --dangerously-skip-permissions"

// SessionCommand returns the command used to start agent sessions.
// Checks SOL_SESSION_COMMAND env var first; defaults to DefaultSessionCommand.
func SessionCommand() string {
	if cmd := os.Getenv("SOL_SESSION_COMMAND"); cmd != "" {
		return cmd
	}
	return DefaultSessionCommand
}

// SettingsPath returns the path to .claude/settings.local.json in a workdir.
func SettingsPath(workdir string) string {
	return filepath.Join(workdir, ".claude", "settings.local.json")
}

// ShellQuote wraps a string in double quotes with interior special characters
// escaped for safe embedding in a shell command. Handles: \ " $ ` !
func ShellQuote(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		`$`, `\$`,
		"`", "\\`",
		`!`, `\!`,
	)
	return `"` + r.Replace(s) + `"`
}

// BuildSessionCommand constructs the full claude startup command with
// --settings and an initial prompt. If SOL_SESSION_COMMAND is set (tests),
// it returns the override verbatim.
func BuildSessionCommand(settingsPath, prompt string) string {
	if cmd := os.Getenv("SOL_SESSION_COMMAND"); cmd != "" {
		return cmd
	}
	return fmt.Sprintf("claude --dangerously-skip-permissions --settings %s %s",
		ShellQuote(settingsPath), ShellQuote(prompt))
}

// NudgeQueueDir returns the nudge queue directory for a session.
// Path: $SOL_HOME/.runtime/nudge_queue/{session}/
func NudgeQueueDir(session string) string {
	return filepath.Join(RuntimeDir(), "nudge_queue", session)
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
