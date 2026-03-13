package adapter

// HookCommand is a runtime-agnostic hook command spec.
type HookCommand struct {
	Command string // shell command to execute
	Matcher string // optional event-specific matcher (e.g. "startup|resume")
}

// Guard is a pre-tool-use guard that blocks a matched tool call.
type Guard struct {
	Pattern string // PreToolUse matcher (e.g. "EnterPlanMode", "Bash(git push --force*)")
	Command string // command to execute; should exit 2 to block
}

// HookSet is a runtime-agnostic hook configuration for a role session.
type HookSet struct {
	SessionStart []HookCommand // commands run on session start
	PreCompact   []HookCommand // commands run before context compaction
	TurnBoundary []HookCommand // commands run at turn boundaries (UserPromptSubmit)
	Guards       []Guard       // pre-tool-use blockers (PreToolUse)
}

// Skill is a name + content pair for an agent skill file.
type Skill struct {
	Name    string // subdirectory name under .claude/skills/
	Content string // SKILL.md content
}

// Credential holds authentication material for a session.
type Credential struct {
	Type  string // "oauth_token" or "api_key"
	Token string
}

// CommandContext holds all arguments needed to build a session launch command.
type CommandContext struct {
	WorktreeDir      string
	Prompt           string
	Continue         bool
	Model            string
	SystemPromptFile string // relative path returned by InjectSystemPrompt (or "" if none)
	ReplacePrompt    bool   // true = --system-prompt-file, false = --append-system-prompt-file
}

// ConfigResult holds the output of EnsureConfigDir.
type ConfigResult struct {
	Dir    string            // absolute path to the runtime config directory
	EnvVar map[string]string // env vars to inject (e.g. {"CLAUDE_CONFIG_DIR": "..."})
}
