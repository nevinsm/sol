// Package adapter defines the RuntimeAdapter interface and supporting types
// for abstracting AI agent runtimes.
package adapter

// TelemetryRecord holds extracted telemetry data from a single log event.
// Returned by ExtractTelemetry; nil means the event is not relevant.
type TelemetryRecord struct {
	Model               string
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	CostUSD             *float64
	DurationMS          *int64
}

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
	// Returns an error if the credential type is unrecognized, so that Launch can
	// abort before creating a tmux session that would immediately fail authentication.
	CredentialEnv(cred Credential) (map[string]string, error)

	// TelemetryEnv returns env vars for OTel telemetry export to the sol ledger.
	// Returns empty map when port <= 0 (telemetry disabled).
	//
	// Telemetry contract for OTEL_RESOURCE_ATTRIBUTES:
	//   - MUST set agent.name, world, and service.name.
	//   - SHOULD set writ_id when the agent is tethered to a writ.
	//   - SHOULD set account when the session uses a specific account.
	//   - service.name MUST match the key used to register the adapter's
	//     ExtractTelemetry implementation in the ledger. The ledger uses
	//     service.name as the routing key to select the correct extractor.
	TelemetryEnv(port int, agent, world, activeWrit, account string) map[string]string

	// ExtractTelemetry extracts token usage data from a log event.
	// Returns nil if the event is not relevant (wrong event name, no token data).
	// The adapter owns both event filtering and attribute extraction.
	ExtractTelemetry(eventName string, attrs map[string]string) *TelemetryRecord

	// Name returns the adapter's registered name (e.g. "claude").
	Name() string
}
