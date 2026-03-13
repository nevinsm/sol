// Package claude provides the Claude Code runtime adapter for sol.
// It satisfies adapter.RuntimeAdapter with stub implementations that will be
// filled in incrementally as startup.Launch is migrated to use the adapter.
package claude

import (
	"github.com/nevinsm/sol/internal/adapter"
)

func init() {
	adapter.Register("claude", New())
}

// Adapter is the Claude Code runtime adapter.
type Adapter struct{}

// Compile-time interface satisfaction check.
var _ adapter.RuntimeAdapter = (*Adapter)(nil)

// New returns a new Claude Code adapter.
func New() *Adapter { return &Adapter{} }

// Name returns the canonical adapter identifier.
func (a *Adapter) Name() string { return "claude" }

// InjectPersona writes the agent persona to CLAUDE.local.md in the worktree.
func (a *Adapter) InjectPersona(worktree string, persona []byte) error {
	panic("not yet implemented")
}

// InstallSkills writes skill definitions to .claude/skills/ in the worktree.
func (a *Adapter) InstallSkills(worktree string, skills []adapter.Skill) error {
	panic("not yet implemented")
}

// InjectSystemPrompt injects a system prompt for the session.
// replace=true replaces the agent's context; replace=false appends to it.
func (a *Adapter) InjectSystemPrompt(worktree string, content string, replace bool) error {
	panic("not yet implemented")
}

// InstallHooks writes hook definitions to .claude/settings.local.json.
func (a *Adapter) InstallHooks(worktree string, hooks adapter.HookSet) error {
	panic("not yet implemented")
}

// EnsureConfigDir creates the per-agent CLAUDE_CONFIG_DIR and returns its path.
func (a *Adapter) EnsureConfigDir(world, role, agent, worktree string) (adapter.ConfigDirResult, error) {
	panic("not yet implemented")
}

// BuildCommand constructs the claude CLI invocation for the tmux session.
func (a *Adapter) BuildCommand(ctx adapter.CommandContext) (string, error) {
	panic("not yet implemented")
}

// CredentialEnv returns the Anthropic credential environment variables.
func (a *Adapter) CredentialEnv(cred adapter.Credential) map[string]string {
	panic("not yet implemented")
}

// TelemetryEnv returns the Claude Code telemetry environment variables.
func (a *Adapter) TelemetryEnv(port int, agent, world, activeWrit string) map[string]string {
	panic("not yet implemented")
}
