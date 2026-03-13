// Package adapter defines the RuntimeAdapter interface and supporting types
// for abstracting AI agent runtimes.
package adapter

// RuntimeAdapter abstracts the underlying AI agent runtime.
// Each method corresponds to one concern in the startup.Launch sequence.
type RuntimeAdapter interface {
	// InjectPersona writes persona content (CLAUDE.local.md or equivalent) to the worktree.
	InjectPersona(worktreeDir string, content []byte) error

	// InstallSkills writes skill files to the worktree's skill directory.
	// Stale skills from previous sessions are removed.
	InstallSkills(worktreeDir string, skills []Skill) error

	// InjectSystemPrompt writes the system prompt to the worktree.
	// Returns the relative path to the written file, for use in BuildCommand.
	InjectSystemPrompt(worktreeDir, content string, replace bool) (string, error)

	// InstallHooks converts the runtime-agnostic HookSet to runtime-specific
	// hook configuration and writes it to the worktree.
	InstallHooks(worktreeDir string, hooks HookSet) error

	// EnsureConfigDir ensures the runtime-specific config directory exists and
	// is configured for the agent. Returns env vars to inject (e.g. CLAUDE_CONFIG_DIR).
	EnsureConfigDir(worldDir, role, agent, worktreeDir string) (ConfigResult, error)

	// BuildCommand constructs the session launch command string.
	// When SOL_SESSION_COMMAND is set, implementations must return it as-is.
	BuildCommand(ctx CommandContext) string

	// CredentialEnv returns env vars for the given credential (e.g. ANTHROPIC_API_KEY).
	CredentialEnv(cred Credential) map[string]string

	// TelemetryEnv returns env vars for OTel telemetry.
	// Returns empty map when port <= 0 (telemetry disabled).
	TelemetryEnv(port int, agent, world, activeWrit string) map[string]string
}
