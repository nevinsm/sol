package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nevinsm/sol/internal/config/defaults"
	"github.com/nevinsm/sol/internal/fileutil"
)

// SessionName returns the tmux session name for an agent.
// For sphere-scoped agents (world == ""), returns "sol-{agentName}".
// For world-scoped agents, returns "sol-{world}-{agentName}".
func SessionName(world, agentName string) string {
	if world == "" {
		return fmt.Sprintf("sol-%s", agentName)
	}
	return fmt.Sprintf("sol-%s-%s", world, agentName)
}

// AgentDir returns the base directory for an agent based on its role.
// - "outpost" (default) → $SOL_HOME/{world}/outposts/{agentName}
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

// AccountsDir returns the path to $SOL_HOME/.accounts/.
func AccountsDir() string {
	return filepath.Join(Home(), ".accounts")
}

// AccountDir returns the path to $SOL_HOME/.accounts/{handle}/.
func AccountDir(handle string) string {
	return filepath.Join(AccountsDir(), handle)
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

// WritOutputDir returns the persistent output directory for a writ.
// Path: $SOL_HOME/{world}/writ-outputs/{writID}/
func WritOutputDir(world, writID string) string {
	return filepath.Join(Home(), world, "writ-outputs", writID)
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
	"workflows": true,
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

// ResolveAgent returns the agent name from the flag value, falling back to
// SOL_AGENT env var. Returns an error if neither is set.
func ResolveAgent(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if env := os.Getenv("SOL_AGENT"); env != "" {
		return env, nil
	}
	return "", fmt.Errorf("--agent is required (or set SOL_AGENT env var)")
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
	// Skip any name that is not a valid world name (includes reserved names and internal directories).
	if ValidateWorldName(candidate) != nil {
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

// BuildSessionCommandContinue constructs a claude startup command with the
// --continue flag, which resumes the previous conversation with compressed
// context. Used for compact-triggered session cycles where the predecessor
// session's history provides valuable continuity.
//
// Findings on --continue behavior:
// - Claude Code saves conversation turns to ~/.claude/projects/. The --continue
//   flag loads the most recent conversation and appends the new prompt as the
//   next user message. This gives the agent compressed context from its
//   predecessor session in addition to our injected prime.
// - Quality is sufficient for continuity — the agent retains awareness of what
//   it was working on, recent decisions, and partial progress.
// - Does NOT conflict with our prime injection: the prime appears as a
//   system-reminder in the resumed conversation, providing fresh durable state
//   while --continue provides compressed conversational context.
// - Only used for compact recovery, not fresh starts — fresh casts should
//   start with a clean conversation to avoid stale context from unrelated work.
func BuildSessionCommandContinue(settingsPath, prompt string) string {
	if cmd := os.Getenv("SOL_SESSION_COMMAND"); cmd != "" {
		return cmd
	}
	return fmt.Sprintf("claude --dangerously-skip-permissions --continue --settings %s %s",
		ShellQuote(settingsPath), ShellQuote(prompt))
}

// ClaudeConfigDir returns the CLAUDE_CONFIG_DIR path for an agent.
// World-scoped agents: <worldDir>/.claude-config/<roleDir>/<name>/
func ClaudeConfigDir(worldDir, role, name string) string {
	var roleDir string
	switch role {
	case "envoy":
		roleDir = "envoys"
	case "outpost":
		roleDir = "outposts"
	default:
		roleDir = role // forge, governor
	}
	return filepath.Join(worldDir, ".claude-config", roleDir, name)
}

// EnsureClaudeConfigDir computes and creates the CLAUDE_CONFIG_DIR for an agent.
// Returns the absolute path. Creates the directory (and parents) if needed.
//
// Creates the config dir, seeds settings.json, plugins, and onboarding state.
// Credentials are injected via environment variables at session start — no
// credential files are written here.
func EnsureClaudeConfigDir(worldDir, role, name, account string) (string, error) {
	dir := ClaudeConfigDir(worldDir, role, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create claude config dir %q: %w", dir, err)
	}

	// Ensure .claude-defaults/ exists before seeding. This makes agent
	// startup self-healing — if defaults were never created (e.g. SOL_HOME
	// predates the init step that seeds them), create them now.
	if err := EnsureClaudeDefaults(); err != nil {
		return "", fmt.Errorf("failed to ensure claude defaults: %w", err)
	}

	// Copy settings.json from .claude-defaults/ (always-overwrite).
	// Ensures config changes propagate to all agents on next session start.
	if err := seedClaudeSettings(dir); err != nil {
		return "", fmt.Errorf("failed to seed claude settings for %s: %w", name, err)
	}

	// Copy plugin metadata from .claude-defaults/plugins/ (always-overwrite).
	// Ensures sphere-level plugins are available to all agents.
	if err := seedClaudePlugins(dir); err != nil {
		return "", fmt.Errorf("failed to seed claude plugins for %s: %w", name, err)
	}

	// Pre-seed onboarding state so Claude Code doesn't show interactive
	// onboarding when using the agent-specific config dir.
	if err := SeedOnboardingState(dir); err != nil {
		return "", fmt.Errorf("failed to seed onboarding state for %s: %w", name, err)
	}

	return dir, nil
}

// SeedOnboardingState seeds critical Claude Code state fields from the
// operator's ~/.claude/.claude.json into the agent's config dir .claude.json.
// Only sets fields that are missing — does not overwrite anything Claude Code
// has already written.
//
// Fields seeded:
//   - hasCompletedOnboarding: prevents onboarding flow
//   - lastOnboardingVersion: prevents version-triggered re-onboarding
//   - firstStartTime: Claude Code checks this exists
//
// Does NOT copy personal preferences (theme, status line, etc.) — agents
// should have clean defaults.
func SeedOnboardingState(configDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		// Can't find home — set minimal onboarding state directly.
		return seedMinimalOnboardingState(configDir)
	}

	// Read source state from ~/.claude/.claude.json (Claude Code's state
	// file inside the default config dir).
	sourceJSON := filepath.Join(home, ".claude", ".claude.json")
	sourceData, err := os.ReadFile(sourceJSON)
	if err != nil {
		// No source file — set minimal onboarding state.
		return seedMinimalOnboardingState(configDir)
	}

	var sourceState map[string]any
	if err := json.Unmarshal(sourceData, &sourceState); err != nil {
		return seedMinimalOnboardingState(configDir)
	}

	// Read or create destination state.
	destJSON := filepath.Join(configDir, ".claude.json")
	var destState map[string]any
	destData, err := os.ReadFile(destJSON)
	if err != nil {
		destState = make(map[string]any)
	} else {
		if err := json.Unmarshal(destData, &destState); err != nil {
			destState = make(map[string]any)
		}
	}

	// Seed only missing fields from source.
	fieldsToSeed := []string{"hasCompletedOnboarding", "lastOnboardingVersion", "firstStartTime"}
	changed := false
	for _, field := range fieldsToSeed {
		if _, exists := destState[field]; !exists {
			if val, ok := sourceState[field]; ok {
				destState[field] = val
				changed = true
			}
		}
	}

	// Ensure hasCompletedOnboarding is always set, even if source lacks it.
	if _, exists := destState["hasCompletedOnboarding"]; !exists {
		destState["hasCompletedOnboarding"] = true
		changed = true
	}

	if !changed {
		return nil
	}

	out, err := json.MarshalIndent(destState, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agent .claude.json: %w", err)
	}
	return fileutil.AtomicWrite(destJSON, out, 0o600)
}

// seedMinimalOnboardingState writes the minimum required onboarding state
// when no source ~/.claude/.claude.json is available.
func seedMinimalOnboardingState(configDir string) error {
	destJSON := filepath.Join(configDir, ".claude.json")
	var destState map[string]any
	destData, err := os.ReadFile(destJSON)
	if err != nil {
		destState = make(map[string]any)
	} else {
		if err := json.Unmarshal(destData, &destState); err != nil {
			destState = make(map[string]any)
		}
	}

	if _, exists := destState["hasCompletedOnboarding"]; exists {
		return nil // Already set.
	}

	destState["hasCompletedOnboarding"] = true

	out, err := json.MarshalIndent(destState, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agent .claude.json: %w", err)
	}
	return fileutil.AtomicWrite(destJSON, out, 0o600)
}

// NudgeQueueDir returns the nudge queue directory for a session.
// Path: $SOL_HOME/.runtime/nudge_queue/{session}/
func NudgeQueueDir(session string) string {
	return filepath.Join(RuntimeDir(), "nudge_queue", session)
}

// ClaudeDefaultsDir returns the path to $SOL_HOME/.claude-defaults/.
// This directory serves as the template for all agent config dirs.
func ClaudeDefaultsDir() string {
	return filepath.Join(Home(), ".claude-defaults")
}

// EnsureClaudeDefaults seeds the embedded default settings.json and
// helper scripts into $SOL_HOME/.claude-defaults/. Sol owns settings.json
// and always overwrites it from the embedded template so new keys propagate
// to existing installations. User overrides go in settings.local.json in
// the same .claude-defaults/ directory — sol never writes or modifies that file.
func EnsureClaudeDefaults() error {
	dir := ClaudeDefaultsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create claude defaults dir %q: %w", dir, err)
	}

	// Write statusline.sh (always overwrite — it's a sol-managed script,
	// not user-customizable content).
	statuslinePath := filepath.Join(dir, "statusline.sh")
	if err := fileutil.AtomicWrite(statuslinePath, defaults.StatuslineSh, 0o755); err != nil {
		return fmt.Errorf("failed to write statusline.sh: %w", err)
	}

	// Write settings.json (always overwrite — sol owns this file).
	// User overrides go in settings.local.json in the same directory.
	settingsPath := filepath.Join(dir, "settings.json")
	settingsContent := strings.ReplaceAll(
		string(defaults.SettingsJSON),
		"{{STATUSLINE_PATH}}",
		statuslinePath,
	)
	if err := fileutil.AtomicWrite(settingsPath, []byte(settingsContent), 0o644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// seedClaudePlugins copies plugin metadata from .claude-defaults/plugins/
// into the agent's plugins/ directory. Always overwrites — sol owns these
// files; the operator manages plugins via `sol config claude`.
//
// Files copied:
//   - installed_plugins.json — plugin installation records
//   - known_marketplaces.json — marketplace on-disk locations
//   - blocklist.json — blocked plugins
//
// Paths inside these files already point at .claude-defaults/plugins/
// (cache/, marketplaces/) since that's where the config session installed them.
// All agents share the same underlying plugin data.
func seedClaudePlugins(agentConfigDir string) error {
	srcPluginsDir := filepath.Join(ClaudeDefaultsDir(), "plugins")
	dstPluginsDir := filepath.Join(agentConfigDir, "plugins")

	files := []string{"installed_plugins.json", "known_marketplaces.json", "blocklist.json"}
	for _, f := range files {
		src := filepath.Join(srcPluginsDir, f)
		data, err := os.ReadFile(src)
		if err != nil {
			// Source doesn't exist — no plugins configured, skip silently.
			continue
		}
		if err := os.MkdirAll(dstPluginsDir, 0o755); err != nil {
			return fmt.Errorf("failed to create plugins dir %q: %w", dstPluginsDir, err)
		}
		dst := filepath.Join(dstPluginsDir, f)
		if err := fileutil.AtomicWrite(dst, data, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", f, err)
		}
	}
	return nil
}

// seedClaudeSettings copies settings.json from .claude-defaults/ into the
// given agent config dir. Skips silently if .claude-defaults/settings.json
// doesn't exist (no defaults configured yet — not an error).
//
// Also merges enabledPlugins from installed_plugins.json into settings.json
// so that Claude Code discovers enabled plugins from the base settings file
// rather than relying on settings.local.json (which may not trigger plugin
// initialization in all Claude Code versions).
//
// Also copies settings.local.json from .claude-defaults/ if present — this
// is the user customization surface; sol never writes it, only distributes it.
func seedClaudeSettings(agentConfigDir string) error {
	src := filepath.Join(ClaudeDefaultsDir(), "settings.json")
	data, err := os.ReadFile(src)
	if err != nil {
		// No defaults template — skip silently.
		return nil
	}

	// Merge enabledPlugins from installed_plugins.json into settings.json.
	data = mergeEnabledPlugins(data)

	dst := filepath.Join(agentConfigDir, "settings.json")
	if err := fileutil.AtomicWrite(dst, data, 0o644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	// Copy settings.local.json if present — user customizations layered on top.
	localSrc := filepath.Join(ClaudeDefaultsDir(), "settings.local.json")
	localData, err := os.ReadFile(localSrc)
	if err != nil {
		// No user overrides file — skip silently.
		return nil
	}
	localDst := filepath.Join(agentConfigDir, "settings.local.json")
	if err := fileutil.AtomicWrite(localDst, localData, 0o644); err != nil {
		return fmt.Errorf("failed to write settings.local.json: %w", err)
	}
	return nil
}

// mergeEnabledPlugins reads installed_plugins.json from .claude-defaults/plugins/
// and merges an enabledPlugins map into the given settings.json content.
// Returns the original content unchanged if no plugins are installed or on
// any error (best-effort — plugin enablement should not break settings seeding).
func mergeEnabledPlugins(settingsData []byte) []byte {
	installedPath := filepath.Join(ClaudeDefaultsDir(), "plugins", "installed_plugins.json")
	installedData, err := os.ReadFile(installedPath)
	if err != nil {
		return settingsData
	}

	// Parse installed_plugins.json — format:
	// {"version": 2, "plugins": {"name@marketplace": [...]}}
	var installed struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(installedData, &installed); err != nil || len(installed.Plugins) == 0 {
		return settingsData
	}

	// Build enabledPlugins map: every installed plugin is enabled.
	enabled := make(map[string]bool, len(installed.Plugins))
	for key := range installed.Plugins {
		enabled[key] = true
	}

	// Parse settings, merge, re-serialize.
	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		return settingsData
	}
	settings["enabledPlugins"] = enabled

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return settingsData
	}
	return out
}

// EnsureDirs creates .store/ and .runtime/ if they don't exist.
func EnsureDirs() error {
	for _, dir := range []string{StoreDir(), RuntimeDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create directory %q: %w", dir, err)
		}
	}
	return nil
}
