package adapter

// Skill represents a named skill definition to be installed into the runtime's
// skill directory.
type Skill struct {
	Name    string
	Content string
}

// HookSet describes agent hooks in runtime-agnostic terms.
// The adapter translates this into whatever format the runtime requires.
type HookSet struct {
	// SessionStart commands run when the agent session starts.
	// Each entry is a shell command string.
	SessionStart []string

	// PreCompact commands run before context compaction.
	PreCompact []string

	// Guards are PreToolUse guards that block matching tool invocations.
	Guards []Guard

	// TurnBoundary commands run at each user-prompt-submit boundary.
	TurnBoundary []string
}

// Guard is a runtime-agnostic pre-tool guard that blocks dangerous patterns.
type Guard struct {
	Matcher string // pattern matched against tool invocations
	Command string // shell command to run (non-zero exit blocks the tool)
}

// CommandContext carries the parameters needed to construct the runtime's
// launch command.
type CommandContext struct {
	Worktree      string // path to the agent's worktree
	Prime         string // initial prompt text
	Model         string // model override (empty = use default)
	Resume        bool   // true = add --continue flag
	ReplacePrompt bool   // true = --system-prompt-file, false = --append-system-prompt-file
}

// Credential carries an authentication credential for the runtime's API.
type Credential struct {
	// Type is the credential kind: "oauth_token" or "api_key".
	Type  string
	Value string
}

// ConfigDirResult is returned by EnsureConfigDir.
type ConfigDirResult struct {
	// Path is the absolute path to the per-agent config directory.
	Path string
	// EnvVar is the environment variable name that points the runtime at Path
	// (e.g., "CLAUDE_CONFIG_DIR").
	EnvVar string
}
