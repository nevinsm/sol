package runtime

import "slices"

// RuntimeDescriptor holds per-runtime configuration data.
// Pure data — no methods except HasHookSupport for convenience.
// Each runtime implementation embeds this struct and calls Descriptor()
// to return it, so runtime-specific data never needs to be duplicated.
type RuntimeDescriptor struct {
	Name            string            // "claude", "codex"
	PersonaFile     string            // persona filename at worktree root, e.g. "CLAUDE.local.md"
	SkillsDir       string            // relative path under worktree, e.g. ".claude/skills"
	ConfigDirEnv    string            // env var name pointing at per-agent config dir, e.g. "CLAUDE_CONFIG_DIR"
	CredentialFile  string            // filename inside config dir, e.g. ".credentials.json"
	GlobalCredsPath string            // path to operator's global credentials, e.g. "~/.claude/.credentials.json" (expanded at use time)
	DefaultModel    string            // fallback when no model is configured
	CalloutCommand  string            // one-shot invocation prefix, e.g. "claude -p"
	SupportedHooks  []string          // hook types this runtime handles natively, e.g. ["SessionStart", "PreCompact"]
	StaticEnv       map[string]string // misc constants to inject into agent env (e.g. runtime-specific feature flags)
	CredentialEnvKeys map[string]string // credential type → env var name, e.g. {"api_key": "ANTHROPIC_API_KEY"}

	// SupportsAutoMemory indicates whether this runtime supports Claude Code's
	// autoMemoryDirectory mechanism. Claude sets this to true; Codex (and any
	// future non-Claude runtime) leaves it false. Consumers (e.g.
	// internal/migrate/migrations/envoy_memory.go) use this flag instead of
	// comparing runtime names directly, so new runtimes get the right default.
	SupportsAutoMemory bool
}

// HasHookSupport reports whether this runtime natively handles the given hook
// type (as a real runtime hook, not instruction text). Hook types: "SessionStart",
// "PreCompact", "TurnBoundary", "Guard".
func (d RuntimeDescriptor) HasHookSupport(hookType string) bool {
	return slices.Contains(d.SupportedHooks, hookType)
}

// Runtime is the interface for AI agent runtime implementations.
// Implementations embed RuntimeDescriptor and implement the five behavioral methods below.
//
// Embedding pattern:
//
//	type ClaudeRuntime struct {
//	    runtime.RuntimeDescriptor
//	}
//
//	func (r *ClaudeRuntime) Descriptor() runtime.RuntimeDescriptor {
//	    return r.RuntimeDescriptor
//	}
type Runtime interface {
	// Descriptor returns the runtime's metadata (provided by embedded RuntimeDescriptor).
	Descriptor() RuntimeDescriptor

	// BuildCommand constructs the session launch command string.
	// When SOL_SESSION_COMMAND is set, implementations MUST return it as-is.
	BuildCommand(ctx CommandContext) string

	// WritePersona places the agent persona into the runtime's expected
	// location inside the worktree. Some runtimes (claude) write a standalone
	// file; others (codex) write into a section of a multi-section file that
	// other methods also touch — section-aware writes must not clobber peers.
	WritePersona(ctx SpawnContext, content []byte) error

	// InstallHooks writes runtime-specific hook config inside the worktree.
	// SpawnContext carries the role/agent/world needed by hook formats that
	// reference per-agent state (e.g. claude's autoMemoryDirectory).
	InstallHooks(ctx SpawnContext, hooks HookSet) error

	// Seed populates runtime-specific state into the per-agent config dir
	// (ctx.ConfigDir, already created by EnsureConfigDir): default settings,
	// onboarding markers, trust dialog acceptance, memory dirs, runtime
	// config files. Runtimes with no per-agent state return nil. Idempotent.
	Seed(ctx SpawnContext) error

	// ExtractTelemetry parses runtime-specific log events for token usage.
	// Returns nil if the event is not relevant or contains no token data.
	ExtractTelemetry(eventName string, attrs map[string]string) *TelemetryRecord
}
