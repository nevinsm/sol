// Package adapter defines the RuntimeAdapter interface that abstracts
// agent-runtime-specific operations from sol's session lifecycle.
//
// V1 ships with a single Claude Code adapter. The interface forces clean
// seams so a second runtime can be added without touching startup logic.
package adapter

// RuntimeAdapter abstracts the nine runtime-specific operations that
// startup.Launch performs. Each method maps to a distinct startup step.
type RuntimeAdapter interface {
	// InjectPersona writes the agent persona to a location the runtime
	// discovers via its directory walk (e.g., CLAUDE.local.md for Claude).
	InjectPersona(worktree string, persona []byte) error

	// InstallSkills writes skill definitions to the runtime's skill directory.
	InstallSkills(worktree string, skills []Skill) error

	// InjectSystemPrompt injects the system prompt for this session.
	// replace=true replaces the agent's entire operating context (outpost, forge).
	// replace=false appends to the base persona (envoy, governor, chancellor).
	InjectSystemPrompt(worktree string, content string, replace bool) error

	// InstallHooks writes runtime-agnostic hook descriptions into whatever
	// format the runtime requires (e.g., Claude's settings.local.json).
	InstallHooks(worktree string, hooks HookSet) error

	// EnsureConfigDir creates and returns the per-agent config directory path
	// and the environment variable name used to point the runtime at it.
	EnsureConfigDir(world, role, agent, worktree string) (ConfigDirResult, error)

	// BuildCommand constructs the shell command string used to launch the
	// runtime process inside the tmux session.
	BuildCommand(ctx CommandContext) (string, error)

	// CredentialEnv returns the environment variables required to authenticate
	// the runtime with its backing API.
	CredentialEnv(cred Credential) map[string]string

	// TelemetryEnv returns the environment variables required to enable and
	// direct the runtime's telemetry output.
	TelemetryEnv(port int, agent, world, activeWrit string) map[string]string

	// Name returns the canonical adapter identifier (e.g., "claude").
	// Used for logging and adapter selection.
	Name() string
}
