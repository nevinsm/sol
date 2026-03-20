// Package claude implements the RuntimeAdapter interface for the Claude Code runtime.
package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
	"github.com/nevinsm/sol/internal/protocol"
)

func init() {
	adapter.Register("claude", New())
}

// Adapter implements adapter.RuntimeAdapter for the Claude Code runtime.
type Adapter struct{}

// Compile-time interface satisfaction check.
var _ adapter.RuntimeAdapter = (*Adapter)(nil)

// New returns a new claude Adapter.
func New() *Adapter {
	return &Adapter{}
}

// Name returns "claude".
func (a *Adapter) Name() string {
	return "claude"
}

// InjectPersona writes persona content to CLAUDE.local.md at the worktree root.
// The file is written at root level so Claude Code's upward directory walk discovers it.
func (a *Adapter) InjectPersona(worktreeDir string, content []byte) error {
	path := filepath.Join(worktreeDir, "CLAUDE.local.md")
	if err := fileutil.AtomicWrite(path, content, 0o644); err != nil {
		return fmt.Errorf("claude adapter: failed to write CLAUDE.local.md: %w", err)
	}
	return nil
}

// InstallSkills writes skill files to {worktreeDir}/.claude/skills/{name}/SKILL.md.
// Stale skill directories (present on disk but not in the skills list) are removed.
func (a *Adapter) InstallSkills(worktreeDir string, skills []adapter.Skill) error {
	skillsDir := filepath.Join(worktreeDir, ".claude", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf("claude adapter: failed to create skills directory: %w", err)
	}

	// Build set of incoming skill names.
	current := make(map[string]bool, len(skills))
	for _, s := range skills {
		current[s.Name] = true
	}

	// Remove stale skill directories.
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return fmt.Errorf("claude adapter: failed to read skills directory: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && !current[e.Name()] {
			stale := filepath.Join(skillsDir, e.Name())
			if err := os.RemoveAll(stale); err != nil {
				return fmt.Errorf("claude adapter: failed to remove stale skill %q: %w", e.Name(), err)
			}
		}
	}

	// Write each skill.
	for _, s := range skills {
		skillDir := filepath.Join(skillsDir, s.Name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return fmt.Errorf("claude adapter: failed to create skill dir %q: %w", s.Name, err)
		}
		skillPath := filepath.Join(skillDir, "SKILL.md")
		if err := os.WriteFile(skillPath, []byte(s.Content), 0o644); err != nil {
			return fmt.Errorf("claude adapter: failed to write skill %q: %w", s.Name, err)
		}
	}

	return nil
}

// InjectSystemPrompt writes content to {worktreeDir}/.claude/system-prompt.md.
// Returns the relative path to the written file, for use in BuildCommand.
// The replace parameter is passed through to BuildCommand via CommandContext.ReplacePrompt.
func (a *Adapter) InjectSystemPrompt(worktreeDir, content string, _ bool) (string, error) {
	claudeDir := filepath.Join(worktreeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return "", fmt.Errorf("claude adapter: failed to create .claude directory: %w", err)
	}
	promptPath := filepath.Join(claudeDir, "system-prompt.md")
	if err := fileutil.AtomicWrite(promptPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("claude adapter: failed to write system prompt: %w", err)
	}
	return ".claude/system-prompt.md", nil
}

// hookEntry is a single hook handler in Claude Code's settings.local.json.
type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// hookMatcherGroup is a matcher + its hook handlers.
type hookMatcherGroup struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []hookEntry `json:"hooks"`
}

// hookSettings is the top-level structure for Claude Code's settings.local.json hooks.
type hookSettings struct {
	Hooks map[string][]hookMatcherGroup `json:"hooks"`
}

// InstallHooks translates the runtime-agnostic HookSet to Claude Code hook JSON
// and writes it to {worktreeDir}/.claude/settings.local.json.
//
// Mapping:
//   - HookSet.SessionStart  → Claude Code "SessionStart" hook entries
//   - HookSet.PreCompact    → Claude Code "PreCompact" hook entries
//   - HookSet.Guards        → Claude Code "PreToolUse" hook entries
//   - HookSet.TurnBoundary  → Claude Code "UserPromptSubmit" hook entries
func (a *Adapter) InstallHooks(worktreeDir string, hooks adapter.HookSet) error {
	hooksMap := map[string][]hookMatcherGroup{}

	// SessionStart
	if len(hooks.SessionStart) > 0 {
		groups := make([]hookMatcherGroup, 0, len(hooks.SessionStart))
		for _, hc := range hooks.SessionStart {
			groups = append(groups, hookMatcherGroup{
				Matcher: hc.Matcher,
				Hooks:   []hookEntry{{Type: "command", Command: hc.Command}},
			})
		}
		hooksMap["SessionStart"] = groups
	}

	// PreCompact
	if len(hooks.PreCompact) > 0 {
		groups := make([]hookMatcherGroup, 0, len(hooks.PreCompact))
		for _, hc := range hooks.PreCompact {
			groups = append(groups, hookMatcherGroup{
				Matcher: hc.Matcher,
				Hooks:   []hookEntry{{Type: "command", Command: hc.Command}},
			})
		}
		hooksMap["PreCompact"] = groups
	}

	// PreToolUse (Guards)
	if len(hooks.Guards) > 0 {
		groups := make([]hookMatcherGroup, 0, len(hooks.Guards))
		for _, g := range hooks.Guards {
			groups = append(groups, hookMatcherGroup{
				Matcher: g.Pattern,
				Hooks:   []hookEntry{{Type: "command", Command: g.Command}},
			})
		}
		hooksMap["PreToolUse"] = groups
	}

	// UserPromptSubmit (TurnBoundary)
	if len(hooks.TurnBoundary) > 0 {
		groups := make([]hookMatcherGroup, 0, len(hooks.TurnBoundary))
		for _, hc := range hooks.TurnBoundary {
			groups = append(groups, hookMatcherGroup{
				Matcher: hc.Matcher,
				Hooks:   []hookEntry{{Type: "command", Command: hc.Command}},
			})
		}
		hooksMap["UserPromptSubmit"] = groups
	}

	settings := hookSettings{Hooks: hooksMap}

	claudeDir := filepath.Join(worktreeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("claude adapter: failed to create .claude directory: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("claude adapter: failed to marshal hook settings: %w", err)
	}

	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	if err := fileutil.AtomicWrite(settingsPath, data, 0o644); err != nil {
		return fmt.Errorf("claude adapter: failed to write settings.local.json: %w", err)
	}

	return nil
}

// EnsureConfigDir creates the Claude config directory, seeds defaults, and
// pre-trusts the worktree. Delegates to config.EnsureClaudeConfigDir and
// protocol.TrustDirectoryIn.
func (a *Adapter) EnsureConfigDir(worldDir, role, agent, worktreeDir string) (adapter.ConfigResult, error) {
	dir, err := config.EnsureClaudeConfigDir(worldDir, role, agent, "")
	if err != nil {
		return adapter.ConfigResult{}, fmt.Errorf("claude adapter: failed to ensure config dir: %w", err)
	}

	if err := protocol.TrustDirectoryIn(worktreeDir, dir); err != nil {
		// Non-fatal: log but don't fail startup.
		fmt.Fprintf(os.Stderr, "claude adapter: failed to pre-trust directory %s in config dir %s: %v\n", worktreeDir, dir, err)
	}

	return adapter.ConfigResult{
		Dir: dir,
		EnvVar: map[string]string{
			"CLAUDE_CONFIG_DIR": dir,
		},
	}, nil
}

// BuildCommand constructs the claude startup command string.
//
// Format:
//
//	claude --dangerously-skip-permissions [--continue] --settings <path>
//	    [--model <model>]
//	    [--system-prompt-file|--append-system-prompt-file <path>]
//	    [<prompt>]
//
// If SOL_SESSION_COMMAND is set (for testing), it is returned verbatim.
func (a *Adapter) BuildCommand(ctx adapter.CommandContext) string {
	if cmd := os.Getenv("SOL_SESSION_COMMAND"); cmd != "" {
		return cmd
	}

	settingsPath := config.SettingsPath(ctx.WorktreeDir)

	args := "claude --dangerously-skip-permissions"

	if ctx.Continue {
		args += " --continue"
	}

	args += " --settings " + config.ShellQuote(settingsPath)

	if ctx.Model != "" {
		args += " --model " + ctx.Model
	}

	if ctx.SystemPromptFile != "" {
		if ctx.ReplacePrompt {
			args += " --system-prompt-file " + config.ShellQuote(ctx.SystemPromptFile)
		} else {
			args += " --append-system-prompt-file " + config.ShellQuote(ctx.SystemPromptFile)
		}
	}

	if ctx.Prompt != "" {
		args += " " + config.ShellQuote(ctx.Prompt)
	}

	return args
}

// CredentialEnv returns the environment variable map for the given credential.
//   - "oauth_token" → CLAUDE_CODE_OAUTH_TOKEN
//   - "api_key"     → ANTHROPIC_API_KEY
//
// Returns an error for unrecognized credential types so that the caller can
// abort startup before creating a tmux session that would fail authentication.
func (a *Adapter) CredentialEnv(cred adapter.Credential) (map[string]string, error) {
	switch cred.Type {
	case "oauth_token":
		return map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": cred.Token}, nil
	case "api_key":
		return map[string]string{"ANTHROPIC_API_KEY": cred.Token}, nil
	default:
		return nil, fmt.Errorf("unrecognized credential type %q — no credentials set; session will fail authentication", cred.Type)
	}
}

// TelemetryEnv returns the environment variables for OTLP telemetry reporting
// to the sol ledger service.
func (a *Adapter) TelemetryEnv(port int, agent, world, activeWrit string) map[string]string {
	if port <= 0 {
		return map[string]string{}
	}
	attrs := fmt.Sprintf("agent.name=%s,world=%s", agent, world)
	if activeWrit != "" {
		attrs += ",writ_id=" + activeWrit
	}
	return map[string]string{
		"CLAUDE_CODE_ENABLE_TELEMETRY":     "1",
		"OTEL_LOGS_EXPORTER":               "otlp",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": fmt.Sprintf("http://localhost:%d/v1/logs", port),
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL": "http/json",
		"OTEL_RESOURCE_ATTRIBUTES":         attrs,
	}
}
