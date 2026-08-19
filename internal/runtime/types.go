// Package runtime defines the runtime abstraction layer for AI agent runtimes.
// See ADR-0041 for the design rationale.
package runtime

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
	Name    string // subdirectory name under the runtime's skills dir
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

	// ChannelsEnabled mirrors the world's agents.channels_enabled config
	// flag (config.WorldConfig.Agents.ChannelsEnabled). Claude-runtime-only:
	// ClaudeRuntime.BuildCommand appends --channels when true; codex ignores
	// it entirely (ADR-0044, CC-9 runtime symmetry). False by default, so
	// leaving this unset reproduces today's BuildCommand output byte-for-byte.
	ChannelsEnabled bool
}

// ConfigResult holds the output of EnsureConfigDir.
type ConfigResult struct {
	Dir    string            // absolute path to the runtime config directory
	EnvVar map[string]string // env vars to inject (e.g. {"CLAUDE_CONFIG_DIR": "..."})
}

// SpawnContext carries the per-session context that interface methods need to
// place files in the right per-agent locations. Populated by the startup
// machinery; runtime methods read from it rather than the call sites passing
// four-to-five positional arguments.
//
// ConfigDir is populated only at Seed call time (after EnsureConfigDir runs);
// it is empty for WritePersona and InstallHooks calls.
type SpawnContext struct {
	WorktreeDir string // worktree where the agent will execute
	WorldDir    string // <SOL_HOME>/<world>/
	Role        string // "envoy" | "outpost" | "forge-merge"
	Agent       string // agent name
	ConfigDir   string // per-agent runtime config dir (set at Seed time only)

	// ChannelsEnabled mirrors the world's agents.channels_enabled config
	// flag — see CommandContext.ChannelsEnabled for the full contract.
	// ClaudeRuntime.Seed uses this to decide whether to merge sol's channel
	// plugin's per-agent installation record into ctx.ConfigDir.
	ChannelsEnabled bool
}

// TelemetryRecord holds extracted telemetry data from a single log event.
// Returned by Runtime.ExtractTelemetry; nil means the event is not relevant.
type TelemetryRecord struct {
	Model               string
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	CostUSD             *float64
	DurationMS          *int64
}
