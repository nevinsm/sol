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
	SessionStart []HookCommand
	PreCompact   []HookCommand
	Guards       []Guard
	TurnBoundary []HookCommand
}

// HookCommand describes a command to run at a hook point.
// Matcher is an optional regex; when non-empty, the hook fires only on
// matching tool/event names (runtime-dependent semantics).
type HookCommand struct {
	Command string
	Matcher string // optional regex
}

// Guard describes a pattern-based block that prevents certain operations.
// The adapter translates this into the runtime's guard/stop mechanism.
type Guard struct {
	Pattern string
	Message string
}

// CommandContext carries the parameters needed to construct the runtime's
// launch command.
type CommandContext struct {
	WorktreeDir   string
	Model         string
	Prime         string
	Resume        bool
	ReplacePrompt bool
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
